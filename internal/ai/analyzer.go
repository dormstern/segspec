package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dormorgenstern/segspec/internal/model"
)

const (
	apiURL     = "https://api.anthropic.com/v1/messages"
	apiModel   = "claude-sonnet-4-20250514"
	apiVersion = "2023-06-01"
	maxTokens  = 4096

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

// aiDep is the JSON shape we ask the AI to return.
type aiDep struct {
	Target      string `json:"target"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Description string `json:"description"`
}

// apiRequest is the Anthropic Messages API request body.
type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []apiMessage `json:"messages"`
}

// apiMessage is a single message in the API request.
type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// apiResponse is the Anthropic Messages API response.
type apiResponse struct {
	Content []apiContent `json:"content"`
}

// apiContent is a content block in the API response.
type apiContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// httpClient allows overriding the HTTP client for testing.
var httpClient interface {
	Do(req *http.Request) (*http.Response, error)
} = http.DefaultClient

// Analyze uses the Claude API to discover network dependencies that rule-based
// parsers might have missed. It walks the directory collecting config file
// contents, sends them to Claude, and returns any additional dependencies found.
func Analyze(root string, existingDeps []model.NetworkDependency) ([]model.NetworkDependency, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	files, err := collectFiles(root)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	prompt := buildPrompt(files, existingDeps)

	responseText, err := callAPI(apiKey, prompt)
	if err != nil {
		return nil, fmt.Errorf("calling Claude API: %w", err)
	}

	deps, err := parseResponse(responseText, root)
	if err != nil {
		return nil, fmt.Errorf("parsing AI response: %w", err)
	}

	return deps, nil
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

// buildPrompt constructs the prompt sent to Claude.
func buildPrompt(files []fileEntry, existingDeps []model.NetworkDependency) string {
	var sb strings.Builder

	sb.WriteString(`You are analyzing application configuration files to identify network dependencies (connections to databases, caches, message brokers, APIs, etc.).

The rule-based parsers have already found these dependencies:
`)

	if len(existingDeps) == 0 {
		sb.WriteString("(none found yet)\n")
	} else {
		for _, dep := range existingDeps {
			fmt.Fprintf(&sb, "- %s:%d/%s (%s)\n", dep.Target, dep.Port, dep.Protocol, dep.Description)
		}
	}

	sb.WriteString(`
Look at the following config files and identify any ADDITIONAL network dependencies the parsers might have missed. Focus on:
- Database connections (PostgreSQL, MySQL, MongoDB, etc.)
- Cache connections (Redis, Memcached, etc.)
- Message brokers (Kafka, RabbitMQ, NATS, etc.)
- HTTP API endpoints
- Any other network services

Return ONLY a JSON array (no markdown, no explanation) with this format:
[{"target": "hostname", "port": 5432, "protocol": "TCP", "description": "PostgreSQL connection found in config.yml"}]

If you find no additional dependencies, return an empty array: []

Files:
`)

	for _, f := range files {
		fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", f.Path, f.Content)
	}

	return sb.String()
}

// callAPI sends the prompt to the Anthropic Messages API and returns the response text.
func callAPI(apiKey, prompt string) (string, error) {
	reqBody := apiRequest{
		Model:     apiModel,
		MaxTokens: maxTokens,
		Messages: []apiMessage{
			{Role: "user", Content: prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)

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
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("unmarshaling response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	return apiResp.Content[0].Text, nil
}

// parseResponse extracts network dependencies from the AI's JSON response.
func parseResponse(text string, root string) ([]model.NetworkDependency, error) {
	// The AI might wrap JSON in markdown code fences; strip them.
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var aiDeps []aiDep
	if err := json.Unmarshal([]byte(text), &aiDeps); err != nil {
		return nil, fmt.Errorf("parsing AI JSON response: %w (response was: %s)", err, truncate(text, 200))
	}

	serviceName := filepath.Base(root)
	var deps []model.NetworkDependency
	for _, ad := range aiDeps {
		desc := ad.Description
		if !strings.HasPrefix(desc, "[AI] ") {
			desc = "[AI] " + desc
		}
		protocol := strings.ToUpper(ad.Protocol)
		if protocol == "" {
			protocol = "TCP"
		}
		deps = append(deps, model.NetworkDependency{
			Source:      serviceName,
			Target:      ad.Target,
			Port:        ad.Port,
			Protocol:    protocol,
			Description: desc,
			Confidence:  model.Medium,
			SourceFile:  "ai-analysis",
		})
	}

	return deps, nil
}

// truncate shortens a string to max length for error messages.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
