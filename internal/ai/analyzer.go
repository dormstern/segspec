package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dormstern/segspec/internal/model"
)

const (
	ollamaBaseURL  = "http://localhost:11434"
	ollamaGenURL   = ollamaBaseURL + "/api/generate"
	ollamaTagsURL  = ollamaBaseURL + "/api/tags"
	ollamaModel    = "nuextract"
	geminiModel    = "gemini-2.0-flash"
	geminiURL      = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiModel + ":generateContent"
	maxFileSize    = 100 * 1024 // 100KB per file
	maxContentSize = 50 * 1024  // 50KB total content sent to API
)

// configExtensions are file extensions we collect for AI analysis.
var configExtensions = map[string]bool{
	".yml":        true,
	".yaml":       true,
	".properties": true,
	".env":        true,
	".xml":        true,
	".json":       true,
	".toml":       true,
	".ini":        true,
	".cfg":        true,
	".conf":       true,
	".gradle":     true,
}

// configFilenames are exact filenames we collect regardless of extension.
var configFilenames = map[string]bool{
	"Dockerfile":         true,
	"docker-compose.yml": true,
	"docker-compose.yaml": true,
	"Makefile":           true,
}

// skippedDirs mirrors walker's skipped dirs.
var skippedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	".git":         true,
	".svn":         true,
	"__pycache__":  true,
}

// fileEntry holds a collected file's path and content.
type fileEntry struct {
	Path    string
	Content string
}

// aiDep is the JSON shape returned by NuExtract.
type aiDep struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	ServiceType string `json:"service_type"`
	Description string `json:"description"`
}

// ollamaGenerateRequest is the Ollama generate API request body.
type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaGenerateResponse is the Ollama generate API response.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
}

// ollamaTagsResponse is the Ollama tags API response.
type ollamaTagsResponse struct {
	Models []ollamaModelEntry `json:"models"`
}

// ollamaModelEntry represents a model entry in the tags response.
type ollamaModelEntry struct {
	Name string `json:"name"`
}

// HTTPClient is the interface used for HTTP requests, allowing test mocks.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// httpClient allows overriding the HTTP client for testing.
// The default client has a 30-second timeout to prevent hanging on
// unresponsive Ollama or Gemini endpoints.
var httpClient HTTPClient = &http.Client{Timeout: 30 * time.Second}

// geminiRequest is the Gemini generateContent API request body.
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// geminiResponse is the Gemini generateContent API response.
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Analyze uses AI to discover network dependencies that rule-based parsers
// might have missed. Provider can be "auto", "local" (Ollama/NuExtract),
// or "cloud" (Gemini Flash).
func Analyze(root string, existingDeps []model.NetworkDependency, provider string) ([]model.NetworkDependency, error) {
	resolvedProvider, err := resolveProvider(provider)
	if err != nil {
		return nil, err
	}

	files, err := collectFiles(root)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	existingKeys := make(map[string]bool)
	serviceName := filepath.Base(root)
	for _, dep := range existingDeps {
		existingKeys[dep.Key()] = true
	}

	var allDeps []model.NetworkDependency

	switch resolvedProvider {
	case "local":
		allDeps, err = analyzeLocal(files, serviceName, existingKeys)
	case "cloud":
		allDeps, err = analyzeCloud(files, serviceName, existingKeys)
	}
	if err != nil {
		return nil, err
	}

	return allDeps, nil
}

// resolveProvider determines which AI backend to use.
func resolveProvider(provider string) (string, error) {
	switch provider {
	case "local":
		if err := checkOllama(); err != nil {
			return "", err
		}
		return "local", nil
	case "cloud":
		if os.Getenv("GEMINI_API_KEY") == "" {
			return "", fmt.Errorf("GEMINI_API_KEY not set — get a free key at https://aistudio.google.com/apikey")
		}
		return "cloud", nil
	case "auto", "":
		// Try local first (privacy-first default), then cloud.
		if checkOllama() == nil {
			return "local", nil
		}
		if os.Getenv("GEMINI_API_KEY") != "" {
			return "cloud", nil
		}
		return "", fmt.Errorf("no AI backend available — choose one:\n\n" +
			"  Local (offline, private):  ollama pull nuextract && segspec analyze --ai local <path>\n" +
			"  Cloud (zero install):      export GEMINI_API_KEY=... && segspec analyze --ai cloud <path>\n\n" +
			"  Get a free Gemini key at:  https://aistudio.google.com/apikey")
	default:
		return "", fmt.Errorf("unknown AI provider %q — use 'local', 'cloud', or omit for auto-detect", provider)
	}
}

// analyzeLocal processes files one-by-one via Ollama/NuExtract.
func analyzeLocal(files []fileEntry, serviceName string, existingKeys map[string]bool) ([]model.NetworkDependency, error) {
	var allDeps []model.NetworkDependency

	for _, f := range files {
		prompt := buildNuExtractPrompt(f)
		responseText, err := callOllama(prompt)
		if err != nil {
			continue
		}
		deps, err := parseResponse(responseText, serviceName)
		if err != nil {
			continue
		}
		for _, dep := range deps {
			if !existingKeys[dep.Key()] {
				existingKeys[dep.Key()] = true
				allDeps = append(allDeps, dep)
			}
		}
	}

	return allDeps, nil
}

// analyzeCloud sends all files to Gemini Flash in a single request.
func analyzeCloud(files []fileEntry, serviceName string, existingKeys map[string]bool) ([]model.NetworkDependency, error) {
	fmt.Fprintln(os.Stderr, "Note: config files will be sent to Google Gemini API. Use --ai local for fully offline analysis.")
	prompt := buildGeminiPrompt(files)
	responseText, err := callGemini(os.Getenv("GEMINI_API_KEY"), prompt)
	if err != nil {
		return nil, err
	}
	deps, err := parseResponse(responseText, serviceName)
	if err != nil {
		return nil, err
	}

	var allDeps []model.NetworkDependency
	for _, dep := range deps {
		if !existingKeys[dep.Key()] {
			existingKeys[dep.Key()] = true
			allDeps = append(allDeps, dep)
		}
	}
	return allDeps, nil
}

// checkOllama verifies that Ollama is reachable and the nuextract model is available.
func checkOllama() error {
	req, err := http.NewRequest("GET", ollamaTagsURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Ollama not reachable at localhost:11434 — install from https://ollama.com and run: ollama pull nuextract")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Ollama not reachable at localhost:11434 — install from https://ollama.com and run: ollama pull nuextract")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading Ollama response: %w", err)
	}

	var tagsResp ollamaTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return fmt.Errorf("parsing Ollama response: %w", err)
	}

	for _, m := range tagsResp.Models {
		// Match "nuextract" or "nuextract:latest" or "nuextract:1.5" etc.
		if m.Name == ollamaModel || strings.HasPrefix(m.Name, ollamaModel+":") {
			return nil
		}
	}

	return fmt.Errorf("Ollama not reachable at localhost:11434 — install from https://ollama.com and run: ollama pull nuextract")
}

// collectFiles walks the directory and collects config file contents up to maxContentSize.
func collectFiles(root string) ([]fileEntry, error) {
	var files []fileEntry
	totalSize := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		if !isConfigFile(d.Name()) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > maxFileSize {
			return nil
		}

		if totalSize >= maxContentSize {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		// Skip binary-looking content.
		if isBinary(data) {
			return nil
		}

		content := string(data)
		// Truncate if adding this file would exceed the limit.
		remaining := maxContentSize - totalSize
		if len(content) > remaining {
			content = content[:remaining]
		}

		relPath, _ := filepath.Rel(root, path)
		if relPath == "" {
			relPath = path
		}

		files = append(files, fileEntry{Path: relPath, Content: content})
		totalSize += len(content)

		return nil
	})

	return files, err
}

// isConfigFile checks if a filename is a config file we should collect.
func isConfigFile(name string) bool {
	if configFilenames[name] {
		return true
	}
	ext := filepath.Ext(name)
	return configExtensions[ext]
}

// isBinary checks if data looks like binary content by scanning for null bytes.
func isBinary(data []byte) bool {
	// Check first 512 bytes for null bytes.
	limit := 512
	if len(data) < limit {
		limit = len(data)
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// buildNuExtractPrompt constructs the NuExtract prompt for a single config file.
func buildNuExtractPrompt(f fileEntry) string {
	return fmt.Sprintf(`<|input|>
Extract all network dependencies (hosts, ports, service connections) from this configuration file:

%s
<|output|>
[{"host": "", "port": 0, "protocol": "", "service_type": "", "description": ""}]`, f.Content)
}

// callOllama sends the prompt to the Ollama generate API and returns the response text.
func callOllama(prompt string) (string, error) {
	reqBody := ollamaGenerateRequest{
		Model:  ollamaModel,
		Prompt: prompt,
		Stream: false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", ollamaGenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaGenerateResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return "", fmt.Errorf("unmarshaling response: %w", err)
	}

	if ollamaResp.Response == "" {
		return "", fmt.Errorf("empty response from Ollama")
	}

	return ollamaResp.Response, nil
}

// buildGeminiPrompt constructs a prompt for Gemini Flash with all files batched.
func buildGeminiPrompt(files []fileEntry) string {
	var sb strings.Builder
	sb.WriteString(`Extract all network dependencies from these configuration files.
For each dependency, return a JSON object with: host, port (integer), protocol (TCP/UDP), service_type, description.
Return ONLY a JSON array. No explanation, no markdown fences.

`)
	for _, f := range files {
		sb.WriteString(fmt.Sprintf("--- file: %s ---\n%s\n\n", f.Path, redactSecrets(f.Content)))
	}
	return sb.String()
}

// callGemini sends a prompt to the Gemini generateContent API.
func callGemini(apiKey string, prompt string) (string, error) {
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: prompt}}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", geminiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading Gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBody, &geminiResp); err != nil {
		return "", fmt.Errorf("parsing Gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// parseResponse extracts network dependencies from NuExtract's JSON response.
func parseResponse(text string, serviceName string) ([]model.NetworkDependency, error) {
	// NuExtract might return extra whitespace or markdown fences; strip them.
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var aiDeps []aiDep
	if err := json.Unmarshal([]byte(text), &aiDeps); err != nil {
		return nil, fmt.Errorf("parsing AI JSON response: %w (response was: %s)", err, truncate(text, 200))
	}

	var deps []model.NetworkDependency
	for _, ad := range aiDeps {
		if ad.Host == "" {
			continue
		}
		desc := ad.Description
		if desc == "" {
			desc = ad.ServiceType
		}
		if !strings.HasPrefix(desc, "[AI] ") {
			desc = "[AI] " + desc
		}
		protocol := strings.ToUpper(ad.Protocol)
		if protocol == "" {
			protocol = "TCP"
		}
		deps = append(deps, model.NetworkDependency{
			Source:      serviceName,
			Target:      ad.Host,
			Port:        ad.Port,
			Protocol:    protocol,
			Description: desc,
			Confidence:  model.Medium,
			SourceFile:  "ai-analysis",
		})
	}

	return deps, nil
}

// secretKeyPattern matches key=value or key: value lines where the key
// suggests a secret (password, token, secret, api_key, apikey, AWS creds).
// It captures: (key)(separator)(value)
var secretKeyPattern = regexp.MustCompile(
	`(?i)((?:password|passwd|secret|token|api_key|apikey|` +
		`AWS_SECRET_ACCESS_KEY|AWS_SESSION_TOKEN)` +
		`)([=: ]+)(.+)`)

// jwtPattern matches JWT tokens (eyJ...) anywhere in the text.
var jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+){1,2}`)

// wellKnownKeyPattern matches well-known API key prefixes.
var wellKnownKeyPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9]{10,}|AKIA[A-Z0-9]{12,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,})`)

// redactSecrets replaces likely secret values with [REDACTED], keeping keys
// visible so the AI can still understand config structure.
func redactSecrets(content string) string {
	// First, redact key=value / key: value patterns for known secret keys.
	content = secretKeyPattern.ReplaceAllString(content, "${1}${2}[REDACTED]")
	// Then redact JWTs anywhere in the text.
	content = jwtPattern.ReplaceAllString(content, "[REDACTED]")
	// Then redact well-known API key prefixes.
	content = wellKnownKeyPattern.ReplaceAllString(content, "[REDACTED]")
	return content
}

// truncate shortens a string to max length for error messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
