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
	"testing"

	"github.com/dormorgenstern/segspec/internal/model"
)

// mockHTTPClient implements the httpClient interface for testing.
type mockHTTPClient struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.DoFunc(req)
}

// ollamaMockWithTags creates a mock that responds to both /api/tags and /api/generate.
func ollamaMockWithTags(generateResponse string) *mockHTTPClient {
	return &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/api/tags") {
				tagsResp := ollamaTagsResponse{
					Models: []ollamaModelEntry{{Name: "nuextract:latest"}},
				}
				body, _ := json.Marshal(tagsResp)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			}
			// /api/generate
			resp := ollamaGenerateResponse{Response: generateResponse}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}
}

func TestAnalyzeOllamaNotReachable(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := Analyze(t.TempDir(), nil, "local")
	if err == nil {
		t.Fatal("expected error when Ollama is not reachable")
	}
	if !strings.Contains(err.Error(), "Ollama not reachable") {
		t.Errorf("error should mention Ollama not reachable, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ollama pull nuextract") {
		t.Errorf("error should mention 'ollama pull nuextract', got: %v", err)
	}
}

func TestAnalyzeOllamaNoNuExtractModel(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			tagsResp := ollamaTagsResponse{
				Models: []ollamaModelEntry{{Name: "llama2:latest"}},
			}
			body, _ := json.Marshal(tagsResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := Analyze(t.TempDir(), nil, "local")
	if err == nil {
		t.Fatal("expected error when nuextract model is not available")
	}
	if !strings.Contains(err.Error(), "ollama pull nuextract") {
		t.Errorf("error should mention 'ollama pull nuextract', got: %v", err)
	}
}

func TestCheckOllamaSuccess(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			tagsResp := ollamaTagsResponse{
				Models: []ollamaModelEntry{
					{Name: "llama2:latest"},
					{Name: "nuextract:latest"},
				},
			}
			body, _ := json.Marshal(tagsResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	if err := checkOllama(); err != nil {
		t.Fatalf("checkOllama() should succeed, got: %v", err)
	}
}

func TestCheckOllamaMatchesUntaggedModel(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			tagsResp := ollamaTagsResponse{
				Models: []ollamaModelEntry{{Name: "nuextract"}},
			}
			body, _ := json.Marshal(tagsResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	if err := checkOllama(); err != nil {
		t.Fatalf("checkOllama() should match 'nuextract' (no tag), got: %v", err)
	}
}

func TestCollectFilesSkipsLargeFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a small config file (should be collected).
	os.WriteFile(filepath.Join(dir, "app.yml"), []byte("db.host: postgres"), 0644)

	// Write a large file (>100KB, should be skipped).
	bigContent := make([]byte, 150*1024)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	os.WriteFile(filepath.Join(dir, "huge.yml"), bigContent, 0644)

	files, err := collectFiles(dir)
	if err != nil {
		t.Fatalf("collectFiles() error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "app.yml" {
		t.Errorf("expected app.yml, got %s", files[0].Path)
	}
}

func TestCollectFilesSkipsBinaryFiles(t *testing.T) {
	dir := t.TempDir()

	// Write a text config file.
	os.WriteFile(filepath.Join(dir, "config.yml"), []byte("key: value"), 0644)

	// Write a binary file with null bytes (should be skipped).
	binaryContent := []byte("header\x00\x00binary data")
	os.WriteFile(filepath.Join(dir, "binary.xml"), binaryContent, 0644)

	files, err := collectFiles(dir)
	if err != nil {
		t.Fatalf("collectFiles() error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Path != "config.yml" {
		t.Errorf("expected config.yml, got %s", files[0].Path)
	}
}

func TestCollectFilesSkipsNonConfigFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "config.yml"), []byte("key: value"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# readme"), 0644)

	files, err := collectFiles(dir)
	if err != nil {
		t.Fatalf("collectFiles() error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestCollectFilesRespectsMaxContentSize(t *testing.T) {
	dir := t.TempDir()

	// Write many config files that together exceed maxContentSize.
	content := strings.Repeat("x", 20*1024) // 20KB each
	for i := 0; i < 5; i++ {
		name := filepath.Join(dir, strings.Repeat("a", i+1)+".yml")
		os.WriteFile(name, []byte(content), 0644)
	}

	files, err := collectFiles(dir)
	if err != nil {
		t.Fatalf("collectFiles() error: %v", err)
	}

	totalContent := 0
	for _, f := range files {
		totalContent += len(f.Content)
	}

	if totalContent > maxContentSize {
		t.Errorf("total content %d exceeds maxContentSize %d", totalContent, maxContentSize)
	}
}

func TestCollectFilesSkipsSkippedDirs(t *testing.T) {
	dir := t.TempDir()

	// Config in root (should be collected).
	os.WriteFile(filepath.Join(dir, "app.yml"), []byte("key: value"), 0644)

	// Config in node_modules (should be skipped).
	nmDir := filepath.Join(dir, "node_modules")
	os.MkdirAll(nmDir, 0755)
	os.WriteFile(filepath.Join(nmDir, "pkg.yml"), []byte("key: value"), 0644)

	// Config in .git (should be skipped).
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("key: value"), 0644)

	files, err := collectFiles(dir)
	if err != nil {
		t.Fatalf("collectFiles() error: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestBuildPromptFormat(t *testing.T) {
	f := fileEntry{Path: "app.yml", Content: "db.host: postgres\ndb.port: 5432"}

	prompt := buildNuExtractPrompt(f)

	if !strings.Contains(prompt, "<|input|>") {
		t.Error("prompt should contain <|input|> tag")
	}
	if !strings.Contains(prompt, "<|output|>") {
		t.Error("prompt should contain <|output|> tag")
	}
	if !strings.Contains(prompt, "db.host: postgres") {
		t.Error("prompt should include file content")
	}
	if !strings.Contains(prompt, `"host"`) {
		t.Error("prompt should include the JSON template with host field")
	}
	if !strings.Contains(prompt, `"service_type"`) {
		t.Error("prompt should include the JSON template with service_type field")
	}
}

func TestParseResponseValidJSON(t *testing.T) {
	response := `[
		{"host": "postgres-db", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "PostgreSQL connection"},
		{"host": "redis-cache", "port": 6379, "protocol": "TCP", "service_type": "cache", "description": "Redis connection"}
	]`

	deps, err := parseResponse(response, "my-service")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(deps))
	}

	// Verify first dep.
	if deps[0].Target != "postgres-db" {
		t.Errorf("Target = %q, want %q", deps[0].Target, "postgres-db")
	}
	if deps[0].Port != 5432 {
		t.Errorf("Port = %d, want 5432", deps[0].Port)
	}
	if deps[0].Protocol != "TCP" {
		t.Errorf("Protocol = %q, want TCP", deps[0].Protocol)
	}
	if deps[0].Confidence != model.Medium {
		t.Errorf("Confidence = %q, want %q", deps[0].Confidence, model.Medium)
	}
	if !strings.HasPrefix(deps[0].Description, "[AI] ") {
		t.Errorf("Description should start with '[AI] ', got: %q", deps[0].Description)
	}
	if deps[0].Source != "my-service" {
		t.Errorf("Source = %q, want %q", deps[0].Source, "my-service")
	}
	if deps[0].SourceFile != "ai-analysis" {
		t.Errorf("SourceFile = %q, want %q", deps[0].SourceFile, "ai-analysis")
	}
}

func TestParseResponseMarkdownFenced(t *testing.T) {
	response := "```json\n[{\"host\": \"mongo\", \"port\": 27017, \"protocol\": \"TCP\", \"service_type\": \"database\", \"description\": \"MongoDB\"}]\n```"

	deps, err := parseResponse(response, "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Target != "mongo" {
		t.Errorf("Target = %q, want mongo", deps[0].Target)
	}
}

func TestParseResponseEmptyArray(t *testing.T) {
	deps, err := parseResponse("[]", "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestParseResponseInvalidJSON(t *testing.T) {
	_, err := parseResponse("this is not json", "svc")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseResponseDefaultProtocol(t *testing.T) {
	response := `[{"host": "api-server", "port": 8080, "protocol": "", "service_type": "api", "description": "API endpoint"}]`

	deps, err := parseResponse(response, "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if deps[0].Protocol != "TCP" {
		t.Errorf("Protocol = %q, want TCP (default)", deps[0].Protocol)
	}
}

func TestParseResponseAIPrefixNotDuplicated(t *testing.T) {
	response := `[{"host": "db", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "[AI] already prefixed"}]`

	deps, err := parseResponse(response, "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if deps[0].Description != "[AI] already prefixed" {
		t.Errorf("Description = %q, should not double-prefix", deps[0].Description)
	}
}

func TestParseResponseSkipsEmptyHost(t *testing.T) {
	response := `[
		{"host": "", "port": 0, "protocol": "", "service_type": "", "description": ""},
		{"host": "real-host", "port": 5432, "protocol": "TCP", "service_type": "db", "description": "real dep"}
	]`

	deps, err := parseResponse(response, "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("expected 1 dep (empty host skipped), got %d", len(deps))
	}
	if deps[0].Target != "real-host" {
		t.Errorf("Target = %q, want real-host", deps[0].Target)
	}
}

func TestParseResponseUsesServiceTypeAsFallbackDescription(t *testing.T) {
	response := `[{"host": "db-host", "port": 5432, "protocol": "TCP", "service_type": "PostgreSQL", "description": ""}]`

	deps, err := parseResponse(response, "svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if deps[0].Description != "[AI] PostgreSQL" {
		t.Errorf("Description = %q, want '[AI] PostgreSQL'", deps[0].Description)
	}
}

func TestCallOllamaSendsCorrectRequest(t *testing.T) {
	var capturedReq *http.Request
	var capturedBody ollamaGenerateRequest

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			body, _ := io.ReadAll(req.Body)
			json.Unmarshal(body, &capturedBody)

			resp := ollamaGenerateResponse{Response: "[]"}
			respBody, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(respBody)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callOllama("test prompt")
	if err != nil {
		t.Fatalf("callOllama() error: %v", err)
	}

	if capturedReq.Method != "POST" {
		t.Errorf("Method = %q, want POST", capturedReq.Method)
	}
	if capturedReq.URL.String() != ollamaGenURL {
		t.Errorf("URL = %q, want %q", capturedReq.URL.String(), ollamaGenURL)
	}
	if capturedReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedReq.Header.Get("Content-Type"))
	}
	if capturedBody.Model != ollamaModel {
		t.Errorf("model = %q, want %q", capturedBody.Model, ollamaModel)
	}
	if capturedBody.Prompt != "test prompt" {
		t.Errorf("prompt = %q, want 'test prompt'", capturedBody.Prompt)
	}
	if capturedBody.Stream != false {
		t.Errorf("stream = %v, want false", capturedBody.Stream)
	}
}

func TestCallOllamaHandlesErrorStatus(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"error": "internal error"}`))),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callOllama("prompt")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestCallOllamaHandlesEmptyResponse(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := ollamaGenerateResponse{Response: ""}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callOllama("prompt")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention empty response, got: %v", err)
	}
}

func TestAnalyzeEndToEndWithMock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "application.yml"), []byte("spring.redis.host: my-redis\nspring.redis.port: 6379"), 0644)

	aiJSON := `[{"host": "my-redis", "port": 6379, "protocol": "TCP", "service_type": "cache", "description": "Redis connection in application.yml"}]`

	mock := ollamaMockWithTags(aiJSON)

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	existing := []model.NetworkDependency{
		{Source: "test", Target: "postgres", Port: 5432, Protocol: "TCP", Description: "PostgreSQL"},
	}

	deps, err := Analyze(dir, existing, "local")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Target != "my-redis" {
		t.Errorf("Target = %q, want my-redis", deps[0].Target)
	}
	if deps[0].Confidence != model.Medium {
		t.Errorf("Confidence = %q, want medium", deps[0].Confidence)
	}
	if !strings.HasPrefix(deps[0].Description, "[AI] ") {
		t.Errorf("Description should have [AI] prefix, got: %q", deps[0].Description)
	}
}

func TestAnalyzeDeduplicatesAgainstExisting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yml"), []byte("db: postgres:5432"), 0644)

	// NuExtract returns a dep that matches an existing one.
	aiJSON := `[{"host": "postgres", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "PostgreSQL"}]`

	mock := ollamaMockWithTags(aiJSON)

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	serviceName := filepath.Base(dir)
	existing := []model.NetworkDependency{
		{Source: serviceName, Target: "postgres", Port: 5432, Protocol: "TCP", Description: "PostgreSQL"},
	}

	deps, err := Analyze(dir, existing, "local")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if len(deps) != 0 {
		t.Errorf("expected 0 deps (deduplicated), got %d", len(deps))
	}
}

func TestAnalyzeEmptyDir(t *testing.T) {
	mock := ollamaMockWithTags("[]")

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	dir := t.TempDir()

	deps, err := Analyze(dir, nil, "local")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for empty dir, got %d", len(deps))
	}
}

func TestAnalyzeMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "db.yml"), []byte("host: postgres\nport: 5432"), 0644)
	os.WriteFile(filepath.Join(dir, "cache.yml"), []byte("redis.host: my-redis\nredis.port: 6379"), 0644)

	callCount := 0
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/api/tags") {
				tagsResp := ollamaTagsResponse{
					Models: []ollamaModelEntry{{Name: "nuextract:latest"}},
				}
				body, _ := json.Marshal(tagsResp)
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewReader(body)),
				}, nil
			}

			// Return different results for each file.
			callCount++
			var aiJSON string
			if callCount == 1 {
				aiJSON = `[{"host": "postgres", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "PostgreSQL"}]`
			} else {
				aiJSON = `[{"host": "my-redis", "port": 6379, "protocol": "TCP", "service_type": "cache", "description": "Redis"}]`
			}

			resp := ollamaGenerateResponse{Response: aiJSON}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	deps, err := Analyze(dir, nil, "local")
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if len(deps) != 2 {
		t.Fatalf("expected 2 deps from 2 files, got %d", len(deps))
	}
}

func TestIsConfigFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"application.yml", true},
		{"config.yaml", true},
		{"app.properties", true},
		{".env", true},
		{"pom.xml", true},
		{"settings.json", true},
		{"config.toml", true},
		{"Dockerfile", true},
		{"docker-compose.yml", true},
		{"main.go", false},
		{"README.md", false},
		{"app.js", false},
		{"image.png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfigFile(tt.name)
			if got != tt.want {
				t.Errorf("isConfigFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	if isBinary([]byte("plain text content")) {
		t.Error("text should not be detected as binary")
	}
	if !isBinary([]byte("has\x00null\x00bytes")) {
		t.Error("content with null bytes should be detected as binary")
	}
	if isBinary([]byte("")) {
		t.Error("empty content should not be detected as binary")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q, want short", got)
	}
	if got := truncate("this is a long string", 10); got != "this is a ..." {
		t.Errorf("truncate long = %q, want 'this is a ...'", got)
	}
}

func TestResolveProviderLocal(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			tagsResp := ollamaTagsResponse{
				Models: []ollamaModelEntry{{Name: "nuextract:latest"}},
			}
			body, _ := json.Marshal(tagsResp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}
	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	p, err := resolveProvider("local")
	if err != nil {
		t.Fatalf("resolveProvider(local) error: %v", err)
	}
	if p != "local" {
		t.Errorf("expected 'local', got %q", p)
	}
}

func TestResolveProviderCloudNoKey(t *testing.T) {
	origKey := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		}
	}()

	_, err := resolveProvider("cloud")
	if err == nil {
		t.Fatal("expected error when GEMINI_API_KEY not set")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error should mention GEMINI_API_KEY, got: %v", err)
	}
}

func TestResolveProviderCloudWithKey(t *testing.T) {
	origKey := os.Getenv("GEMINI_API_KEY")
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	p, err := resolveProvider("cloud")
	if err != nil {
		t.Fatalf("resolveProvider(cloud) error: %v", err)
	}
	if p != "cloud" {
		t.Errorf("expected 'cloud', got %q", p)
	}
}

func TestResolveProviderAutoFallsBackToCloud(t *testing.T) {
	// Ollama unreachable, but Gemini key set.
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	origKey := os.Getenv("GEMINI_API_KEY")
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	p, err := resolveProvider("auto")
	if err != nil {
		t.Fatalf("resolveProvider(auto) error: %v", err)
	}
	if p != "cloud" {
		t.Errorf("expected 'cloud' (fallback), got %q", p)
	}
}

func TestResolveProviderAutoNeitherAvailable(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	origKey := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		}
	}()

	_, err := resolveProvider("auto")
	if err == nil {
		t.Fatal("expected error when neither backend available")
	}
	if !strings.Contains(err.Error(), "no AI backend available") {
		t.Errorf("error should mention 'no AI backend available', got: %v", err)
	}
	if !strings.Contains(err.Error(), "ollama pull nuextract") {
		t.Errorf("error should mention ollama install, got: %v", err)
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error should mention GEMINI_API_KEY, got: %v", err)
	}
}

func TestResolveProviderUnknown(t *testing.T) {
	_, err := resolveProvider("potato")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "unknown AI provider") {
		t.Errorf("error should mention unknown provider, got: %v", err)
	}
}

func TestBuildGeminiPrompt(t *testing.T) {
	files := []fileEntry{
		{Path: "app.yml", Content: "db.host: postgres"},
		{Path: ".env", Content: "REDIS_URL=redis://cache:6379"},
	}

	prompt := buildGeminiPrompt(files)

	if !strings.Contains(prompt, "network dependencies") {
		t.Error("prompt should mention network dependencies")
	}
	if !strings.Contains(prompt, "--- file: app.yml ---") {
		t.Error("prompt should include file header")
	}
	if !strings.Contains(prompt, "db.host: postgres") {
		t.Error("prompt should include file content")
	}
	if !strings.Contains(prompt, "--- file: .env ---") {
		t.Error("prompt should include second file header")
	}
}

func TestCallGeminiSuccess(t *testing.T) {
	aiJSON := `[{"host": "postgres", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "PostgreSQL"}]`

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := geminiResponse{
				Candidates: []struct {
					Content struct {
						Parts []geminiPart `json:"parts"`
					} `json:"content"`
				}{
					{Content: struct {
						Parts []geminiPart `json:"parts"`
					}{Parts: []geminiPart{{Text: aiJSON}}}},
				},
			}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	text, err := callGemini("test-key", "test prompt")
	if err != nil {
		t.Fatalf("callGemini() error: %v", err)
	}
	if text != aiJSON {
		t.Errorf("expected %q, got %q", aiJSON, text)
	}
}

func TestCallGeminiErrorStatus(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 403,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"error": "forbidden"}`))),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callGemini("bad-key", "prompt")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403, got: %v", err)
	}
}

func TestCallGeminiEmptyResponse(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := geminiResponse{}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callGemini("key", "prompt")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention empty response, got: %v", err)
	}
}

func TestAnalyzeCloudEndToEnd(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.yml"), []byte("db.host: postgres\ndb.port: 5432"), 0644)

	aiJSON := `[{"host": "postgres", "port": 5432, "protocol": "TCP", "service_type": "database", "description": "PostgreSQL"}]`

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := geminiResponse{
				Candidates: []struct {
					Content struct {
						Parts []geminiPart `json:"parts"`
					} `json:"content"`
				}{
					{Content: struct {
						Parts []geminiPart `json:"parts"`
					}{Parts: []geminiPart{{Text: aiJSON}}}},
				},
			}
			body, _ := json.Marshal(resp)
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	origKey := os.Getenv("GEMINI_API_KEY")
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer func() {
		if origKey != "" {
			os.Setenv("GEMINI_API_KEY", origKey)
		} else {
			os.Unsetenv("GEMINI_API_KEY")
		}
	}()

	deps, err := Analyze(dir, nil, "cloud")
	if err != nil {
		t.Fatalf("Analyze(cloud) error: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(deps))
	}
	if deps[0].Target != "postgres" {
		t.Errorf("Target = %q, want postgres", deps[0].Target)
	}
}
