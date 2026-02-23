package model

import "regexp"

// secretKeyPattern matches key=value or key: value lines where the key
// suggests a secret (password, token, secret, api_key, etc).
var secretKeyPattern = regexp.MustCompile(
	`(?i)((?:password|passwd|secret|token|api_key|apikey|private_key|private-key|` +
		`client_secret|access_key|auth_token|connection_string|` +
		`AWS_SECRET_ACCESS_KEY|AWS_SESSION_TOKEN)` +
		`)([=: ]+)(.+)`)

// jwtPattern matches JWT tokens (eyJ...) anywhere in the text.
var jwtPattern = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+){1,2}`)

// wellKnownKeyPattern matches well-known API key prefixes.
var wellKnownKeyPattern = regexp.MustCompile(
	`\b(sk-[A-Za-z0-9]{10,}|sk_live_[A-Za-z0-9]{10,}|` +
		`AKIA[A-Z0-9]{12,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,})`)

// credentialInURL matches user:password@ in URLs.
var credentialInURL = regexp.MustCompile(`://([^@/:]+):([^@/]+)@`)

// RedactSecrets replaces likely secret values with [REDACTED], keeping keys
// and structure visible for evidence purposes.
func RedactSecrets(content string) string {
	content = secretKeyPattern.ReplaceAllString(content, "${1}${2}[REDACTED]")
	content = credentialInURL.ReplaceAllString(content, "://${1}:[REDACTED]@")
	content = jwtPattern.ReplaceAllString(content, "[REDACTED]")
	content = wellKnownKeyPattern.ReplaceAllString(content, "[REDACTED]")
	return content
}
