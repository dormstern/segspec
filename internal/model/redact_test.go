package model

import "testing"

func TestRedactSecretsJDBC(t *testing.T) {
	input := "spring.datasource.url: jdbc:postgresql://admin:SuperSecret123@db:5432/app"
	out := RedactSecrets(input)
	if out == input {
		t.Error("expected redaction of credentials in JDBC URL")
	}
	if contains(out, "SuperSecret123") {
		t.Error("password still present after redaction")
	}
}

func TestRedactSecretsEnvPassword(t *testing.T) {
	input := "DB_PASSWORD=hunter2"
	out := RedactSecrets(input)
	if contains(out, "hunter2") {
		t.Error("password still present after redaction")
	}
}

func TestRedactSecretsJWT(t *testing.T) {
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123"
	out := RedactSecrets(input)
	if contains(out, "eyJhbGci") {
		t.Error("JWT still present after redaction")
	}
}

func TestRedactSecretsAPIKey(t *testing.T) {
	input := "STRIPE_KEY=sk_live_abcdefghij1234567890"
	out := RedactSecrets(input)
	if contains(out, "sk_live_abcdefghij") {
		t.Error("Stripe key still present after redaction")
	}
}

func TestRedactSecretsNoFalsePositive(t *testing.T) {
	input := "spring.datasource.url: jdbc:postgresql://postgres:5432/app"
	out := RedactSecrets(input)
	if out != input {
		t.Errorf("unexpected redaction of non-secret: got %q", out)
	}
}

func TestRedactSecretsURLCredentials(t *testing.T) {
	input := "DATABASE_URL=postgresql://user:s3cret@db:5432/mydb"
	out := RedactSecrets(input)
	if contains(out, "s3cret") {
		t.Error("URL credentials still present after redaction")
	}
	if !contains(out, "user:") {
		t.Error("username should be preserved")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchString(s, substr)))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
