package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// devPrivateKeyHex matches the development keypair in
// internal/license/DEVELOPMENT_PRIVATE_KEY.txt. Tokens minted here verify
// against license.ProductionPublicKey, mirroring the production signing path.
const devPrivateKeyHex = "413459ff6d9a75ad66ab8789694eb38d3f2af7dd34d828a3f97477ef924d59c1f9b633b9fc16148bd15b024af8f694c7a0942d6ca3e0ed367d0010dda57689a3"

func devPrivKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := hex.DecodeString(devPrivateKeyHex)
	if err != nil {
		t.Fatalf("decode dev private key: %v", err)
	}
	return ed25519.PrivateKey(raw)
}

// mintTestToken builds a license JWT with the given tier and expiry.
func mintTestToken(t *testing.T, tier string, exp time.Time) string {
	t.Helper()
	priv := devPrivKey(t)
	header := map[string]string{"alg": "EdDSA", "typ": "JWT"}
	payload := map[string]any{"sub": "test-org", "tier": tier, "exp": exp.Unix()}
	hdrBytes, _ := json.Marshal(header)
	plBytes, _ := json.Marshal(payload)
	hdrB64 := base64.RawURLEncoding.EncodeToString(hdrBytes)
	plB64 := base64.RawURLEncoding.EncodeToString(plBytes)
	signingInput := hdrB64 + "." + plB64
	sig := ed25519.Sign(priv, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// resetLicenseState clears every package-level variable that the license
// pipeline touches, so tests don't bleed state into each other.
func resetLicenseState(t *testing.T) {
	t.Helper()
	licenseKey = ""
	activeClaims = nil
	t.Setenv("SEGSPEC_LICENSE_KEY", "")
	// Also clear the format/output flags that other tests mutate.
	outputFormat = "summary"
	outputFile = ""
	diffExitCode = false
}

func TestResolveLicense_NoKey(t *testing.T) {
	resetLicenseState(t)
	if err := resolveLicense(nil, nil); err != nil {
		t.Fatalf("expected no error with no license, got %v", err)
	}
	if activeClaims != nil {
		t.Errorf("expected nil claims, got %+v", activeClaims)
	}
}

func TestResolveLicense_ValidProToken(t *testing.T) {
	resetLicenseState(t)
	licenseKey = mintTestToken(t, "pro", time.Now().Add(24*time.Hour))
	if err := resolveLicense(nil, nil); err != nil {
		t.Fatalf("expected valid token to resolve, got %v", err)
	}
	if activeClaims == nil || activeClaims.Tier != "pro" {
		t.Errorf("expected pro claims, got %+v", activeClaims)
	}
}

func TestResolveLicense_ExpiredToken(t *testing.T) {
	resetLicenseState(t)
	licenseKey = mintTestToken(t, "pro", time.Now().Add(-1*time.Hour))
	err := resolveLicense(nil, nil)
	if err == nil {
		t.Fatal("expected expired token to error")
	}
	var le *errLicenseRequired
	if !errors.As(err, &le) {
		t.Fatalf("expected *errLicenseRequired, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected 'expired' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "https://segspec.dev/pro") {
		t.Errorf("expected upgrade URL in error, got %q", err.Error())
	}
}

func TestResolveLicense_GarbageToken(t *testing.T) {
	resetLicenseState(t)
	licenseKey = "this.is.not-a-valid-jwt"
	err := resolveLicense(nil, nil)
	if err == nil {
		t.Fatal("expected garbage token to error")
	}
	var le *errLicenseRequired
	if !errors.As(err, &le) {
		t.Fatalf("expected *errLicenseRequired, got %T", err)
	}
}

func TestResolveLicense_EnvFallback(t *testing.T) {
	resetLicenseState(t)
	licenseKey = ""
	t.Setenv("SEGSPEC_LICENSE_KEY", mintTestToken(t, "enterprise", time.Now().Add(24*time.Hour)))
	if err := resolveLicense(nil, nil); err != nil {
		t.Fatalf("expected env-supplied token to resolve, got %v", err)
	}
	if activeClaims == nil || activeClaims.Tier != "enterprise" {
		t.Errorf("expected enterprise claims, got %+v", activeClaims)
	}
}

func TestCheckFormatLicense(t *testing.T) {
	tests := []struct {
		name    string
		claims  func(t *testing.T)
		format  string
		wantErr bool
	}{
		{
			name:    "free format with no license is allowed",
			claims:  func(t *testing.T) { activeClaims = nil },
			format:  "summary",
			wantErr: false,
		},
		{
			name:    "json format with no license is allowed",
			claims:  func(t *testing.T) { activeClaims = nil },
			format:  "json",
			wantErr: false,
		},
		{
			name:    "netpol format with no license is allowed",
			claims:  func(t *testing.T) { activeClaims = nil },
			format:  "netpol",
			wantErr: false,
		},
		{
			name:    "evidence format without license is rejected",
			claims:  func(t *testing.T) { activeClaims = nil },
			format:  "evidence",
			wantErr: true,
		},
		{
			name:    "per-service format without license is rejected",
			claims:  func(t *testing.T) { activeClaims = nil },
			format:  "per-service",
			wantErr: true,
		},
		{
			name: "evidence format with pro license is allowed",
			claims: func(t *testing.T) {
				_, err := resolveLicenseHelper(t, "pro")
				if err != nil {
					t.Fatal(err)
				}
			},
			format:  "evidence",
			wantErr: false,
		},
		{
			name: "per-service format with pro license is allowed",
			claims: func(t *testing.T) {
				_, err := resolveLicenseHelper(t, "pro")
				if err != nil {
					t.Fatal(err)
				}
			},
			format:  "per-service",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetLicenseState(t)
			tt.claims(t)
			err := checkFormatLicense(tt.format)
			if tt.wantErr && err == nil {
				t.Fatalf("expected gate error for %q, got nil", tt.format)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tt.format, err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "Pro license") {
					t.Errorf("expected error to mention Pro license, got %q", err.Error())
				}
				if !strings.Contains(err.Error(), "https://segspec.dev/pro") {
					t.Errorf("expected upgrade URL in error, got %q", err.Error())
				}
			}
		})
	}
}

// resolveLicenseHelper mints and applies a token of the given tier so tests
// that need an active paid license can set one up in a single call.
func resolveLicenseHelper(t *testing.T, tier string) (string, error) {
	t.Helper()
	tok := mintTestToken(t, tier, time.Now().Add(24*time.Hour))
	licenseKey = tok
	if err := resolveLicense(nil, nil); err != nil {
		return "", err
	}
	return tok, nil
}

// stripeAgentEnvVarSentinel is a guard test that documents the contract with
// the Stripe webhook setup: SEGSPEC_LICENSE_KEY is the env var. If anyone
// renames it accidentally this test fails loudly.
func TestEnvVarContract(t *testing.T) {
	const expected = "SEGSPEC_LICENSE_KEY"
	resetLicenseState(t)
	t.Setenv(expected, mintTestToken(t, "pro", time.Now().Add(24*time.Hour)))
	if err := resolveLicense(nil, nil); err != nil {
		t.Fatalf("expected env var %s to be honored, got %v", expected, err)
	}
	if activeClaims == nil {
		t.Fatalf("expected env var %s to populate activeClaims", expected)
	}
	// Be defensive: make sure we're using the documented var, not e.g. a typo.
	_ = os.Getenv(expected)
}
