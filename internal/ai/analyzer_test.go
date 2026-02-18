package ai

import (
	"bytes"
	"encoding/json"
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

func TestAnalyzeMissingAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := Analyze(t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error should mention ANTHROPIC_API_KEY, got: %v", err)
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

func TestBuildPromptIncludesExistingDeps(t *testing.T) {
	files := []fileEntry{
		{Path: "app.yml", Content: "db.host: postgres"},
	}
	existing := []model.NetworkDependency{
		{Target: "postgres", Port: 5432, Protocol: "TCP", Description: "PostgreSQL from Spring config"},
	}

	prompt := buildPrompt(files, existing)

	if !strings.Contains(prompt, "postgres:5432/TCP") {
		t.Error("prompt should list existing dependency")
	}
	if !strings.Contains(prompt, "--- app.yml ---") {
		t.Error("prompt should include file content header")
	}
	if !strings.Contains(prompt, "db.host: postgres") {
		t.Error("prompt should include file content")
	}
}

func TestBuildPromptHandlesNoDeps(t *testing.T) {
	files := []fileEntry{
		{Path: "app.yml", Content: "key: value"},
	}

	prompt := buildPrompt(files, nil)

	if !strings.Contains(prompt, "(none found yet)") {
		t.Error("prompt should indicate no existing deps")
	}
}

func TestParseResponseValidJSON(t *testing.T) {
	response := `[
		{"target": "postgres-db", "port": 5432, "protocol": "TCP", "description": "PostgreSQL connection in application.yml"},
		{"target": "redis-cache", "port": 6379, "protocol": "TCP", "description": "Redis connection in .env"}
	]`

	deps, err := parseResponse(response, "/app/my-service")
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
	response := "```json\n[{\"target\": \"mongo\", \"port\": 27017, \"protocol\": \"TCP\", \"description\": \"MongoDB\"}]\n```"

	deps, err := parseResponse(response, "/app/svc")
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
	deps, err := parseResponse("[]", "/app/svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestParseResponseInvalidJSON(t *testing.T) {
	_, err := parseResponse("this is not json", "/app/svc")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseResponseDefaultProtocol(t *testing.T) {
	response := `[{"target": "api-server", "port": 8080, "protocol": "", "description": "API endpoint"}]`

	deps, err := parseResponse(response, "/app/svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if deps[0].Protocol != "TCP" {
		t.Errorf("Protocol = %q, want TCP (default)", deps[0].Protocol)
	}
}

func TestParseResponseAIPrefixNotDuplicated(t *testing.T) {
	response := `[{"target": "db", "port": 5432, "protocol": "TCP", "description": "[AI] already prefixed"}]`

	deps, err := parseResponse(response, "/app/svc")
	if err != nil {
		t.Fatalf("parseResponse() error: %v", err)
	}

	if deps[0].Description != "[AI] already prefixed" {
		t.Errorf("Description = %q, should not double-prefix", deps[0].Description)
	}
}

func TestCallAPISendsCorrectHeaders(t *testing.T) {
	var capturedReq *http.Request

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			capturedReq = req

			resp := apiResponse{
				Content: []apiContent{
					{Type: "text", Text: "[]"},
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

	_, err := callAPI("test-key-123", "test prompt")
	if err != nil {
		t.Fatalf("callAPI() error: %v", err)
	}

	if capturedReq.Header.Get("x-api-key") != "test-key-123" {
		t.Errorf("x-api-key = %q, want test-key-123", capturedReq.Header.Get("x-api-key"))
	}
	if capturedReq.Header.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", capturedReq.Header.Get("anthropic-version"))
	}
	if capturedReq.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedReq.Header.Get("Content-Type"))
	}
	if capturedReq.URL.String() != apiURL {
		t.Errorf("URL = %q, want %q", capturedReq.URL.String(), apiURL)
	}
	if capturedReq.Method != "POST" {
		t.Errorf("Method = %q, want POST", capturedReq.Method)
	}
}

func TestCallAPISendsCorrectBody(t *testing.T) {
	var capturedBody apiRequest

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			json.Unmarshal(body, &capturedBody)

			resp := apiResponse{
				Content: []apiContent{
					{Type: "text", Text: "[]"},
				},
			}
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

	_, err := callAPI("key", "my prompt")
	if err != nil {
		t.Fatalf("callAPI() error: %v", err)
	}

	if capturedBody.Model != apiModel {
		t.Errorf("model = %q, want %q", capturedBody.Model, apiModel)
	}
	if capturedBody.MaxTokens != maxTokens {
		t.Errorf("max_tokens = %d, want %d", capturedBody.MaxTokens, maxTokens)
	}
	if len(capturedBody.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedBody.Messages))
	}
	if capturedBody.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", capturedBody.Messages[0].Role)
	}
	if capturedBody.Messages[0].Content != "my prompt" {
		t.Errorf("content = %q, want 'my prompt'", capturedBody.Messages[0].Content)
	}
}

func TestCallAPIHandlesErrorStatus(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 401,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"error": "unauthorized"}`))),
			}, nil
		},
	}

	origClient := httpClient
	httpClient = mock
	defer func() { httpClient = origClient }()

	_, err := callAPI("bad-key", "prompt")
	if err == nil {
		t.Fatal("expected error for 401 status")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestCallAPIHandlesEmptyResponse(t *testing.T) {
	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := apiResponse{Content: []apiContent{}}
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

	_, err := callAPI("key", "prompt")
	if err == nil {
		t.Fatal("expected error for empty response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error should mention empty response, got: %v", err)
	}
}

func TestAnalyzeEndToEndWithMock(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "application.yml"), []byte("spring.redis.host: my-redis\nspring.redis.port: 6379"), 0644)

	aiJSON := `[{"target": "my-redis", "port": 6379, "protocol": "TCP", "description": "Redis connection in application.yml"}]`

	mock := &mockHTTPClient{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			resp := apiResponse{
				Content: []apiContent{
					{Type: "text", Text: aiJSON},
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

	existing := []model.NetworkDependency{
		{Target: "postgres", Port: 5432, Protocol: "TCP", Description: "PostgreSQL"},
	}

	deps, err := Analyze(dir, existing)
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

func TestAnalyzeEmptyDir(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")

	dir := t.TempDir()

	deps, err := Analyze(dir, nil)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil deps for empty dir, got %d", len(deps))
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
