package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// devPrivateKeyHex is the development Ed25519 private key (seed||pubkey).
// Its public half matches ProductionPublicKey in pubkey.go, so tokens minted
// here verify against the in-binary production key — exactly the path the
// Stripe webhook will exercise in production. The full key lives in
// DEVELOPMENT_PRIVATE_KEY.txt (gitignored). Rotated together with pubkey.go.
const devPrivateKeyHex = "413459ff6d9a75ad66ab8789694eb38d3f2af7dd34d828a3f97477ef924d59c1f9b633b9fc16148bd15b024af8f694c7a0942d6ca3e0ed367d0010dda57689a3"

func devPrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	raw, err := hex.DecodeString(devPrivateKeyHex)
	if err != nil {
		t.Fatalf("decode dev private key: %v", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		t.Fatalf("dev private key is %d bytes, want %d", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw)
}

// mintToken signs a test license token with the given private key. We do this
// by hand rather than reaching for a JWT library so the tests exercise the
// exact wire format the production signer will produce.
func mintToken(t *testing.T, priv ed25519.PrivateKey, sub, tier string, exp time.Time, alg string) string {
	t.Helper()
	header := map[string]string{"alg": alg, "typ": "JWT"}
	payload := map[string]any{"sub": sub, "tier": tier, "exp": exp.Unix()}
	hdrBytes, _ := json.Marshal(header)
	plBytes, _ := json.Marshal(payload)
	hdrB64 := base64.RawURLEncoding.EncodeToString(hdrBytes)
	plB64 := base64.RawURLEncoding.EncodeToString(plBytes)
	signingInput := hdrB64 + "." + plB64
	sig := ed25519.Sign(priv, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestValidate(t *testing.T) {
	priv := devPrivateKey(t)
	pub := priv.Public().(ed25519.PublicKey)

	// Sanity check: the dev keypair really does match ProductionPublicKey.
	// If this fails, pubkey.go has been rotated and DEVELOPMENT_PRIVATE_KEY.txt
	// is stale.
	if string(pub) != string(ProductionPublicKey) {
		t.Fatalf("dev public key does not match ProductionPublicKey; rotate DEVELOPMENT_PRIVATE_KEY.txt")
	}

	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-1 * time.Hour)

	tests := []struct {
		name      string
		token     func() string
		key       ed25519.PublicKey
		wantErr   error // nil means no error; uses errors.Is
		wantTier  string
		wantOrg   string
	}{
		{
			name:     "valid pro token verifies",
			token:    func() string { return mintToken(t, priv, "acme-corp", "pro", future, "EdDSA") },
			key:      pub,
			wantTier: "pro",
			wantOrg:  "acme-corp",
		},
		{
			name:     "valid enterprise token verifies",
			token:    func() string { return mintToken(t, priv, "globex", "enterprise", future, "EdDSA") },
			key:      pub,
			wantTier: "enterprise",
			wantOrg:  "globex",
		},
		{
			name:    "expired token rejected",
			token:   func() string { return mintToken(t, priv, "acme-corp", "pro", past, "EdDSA") },
			key:     pub,
			wantErr: ErrExpired,
		},
		{
			name: "wrong-signature token rejected",
			token: func() string {
				// Sign with a different keypair, then try to verify against
				// ProductionPublicKey.
				_, otherPriv, err := ed25519.GenerateKey(nil)
				if err != nil {
					t.Fatalf("generate other key: %v", err)
				}
				return mintToken(t, otherPriv, "acme-corp", "pro", future, "EdDSA")
			},
			key:     pub,
			wantErr: ErrBadSignature,
		},
		{
			name:    "malformed (not 3 segments) rejected",
			token:   func() string { return "not.a.real.jwt.token" },
			key:     pub,
			wantErr: ErrMalformedToken,
		},
		{
			name:    "malformed (bad base64) rejected",
			token:   func() string { return "@@@.@@@.@@@" },
			key:     pub,
			wantErr: ErrMalformedToken,
		},
		{
			name:    "wrong algorithm rejected",
			token:   func() string { return mintToken(t, priv, "acme-corp", "pro", future, "HS256") },
			key:     pub,
			wantErr: ErrUnsupportedAlg,
		},
		{
			name: "tampered payload rejected",
			token: func() string {
				good := mintToken(t, priv, "acme-corp", "pro", future, "EdDSA")
				parts := strings.Split(good, ".")
				// Substitute a payload that says enterprise without resigning.
				tampered, _ := json.Marshal(map[string]any{
					"sub":  "acme-corp",
					"tier": "enterprise",
					"exp":  future.Unix(),
				})
				parts[1] = base64.RawURLEncoding.EncodeToString(tampered)
				return strings.Join(parts, ".")
			},
			key:     pub,
			wantErr: ErrBadSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := Validate(tt.token(), tt.key)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil claims=%+v", tt.wantErr, claims)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if claims.Tier != tt.wantTier {
				t.Errorf("tier = %q, want %q", claims.Tier, tt.wantTier)
			}
			if claims.Org != tt.wantOrg {
				t.Errorf("org = %q, want %q", claims.Org, tt.wantOrg)
			}
		})
	}
}

func TestExpiredErrorMessage(t *testing.T) {
	// The CLI surfaces "License expired YYYY-MM-DD" to users, so check that
	// ExpiredError carries the date in a parseable form.
	priv := devPrivateKey(t)
	pub := priv.Public().(ed25519.PublicKey)
	expiredAt := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	tok := mintToken(t, priv, "acme", "pro", expiredAt, "EdDSA")

	_, err := Validate(tok, pub)
	if err == nil {
		t.Fatal("expected error")
	}
	var exp *ExpiredError
	if !errors.As(err, &exp) {
		t.Fatalf("expected *ExpiredError, got %T: %v", err, err)
	}
	if !strings.Contains(exp.Error(), "2024-01-15") {
		t.Errorf("expected expiry date in error, got %q", exp.Error())
	}
}

func TestIsPaidTierAllowed(t *testing.T) {
	cases := []struct {
		name    string
		claims  *Claims
		feature string
		want    bool
	}{
		{"nil claims block paid feature", nil, FeatureEvidenceFormat, false},
		{"free tier claims block paid feature", &Claims{Tier: "free"}, FeatureEvidenceFormat, false},
		{"pro tier unlocks evidence", &Claims{Tier: "pro"}, FeatureEvidenceFormat, true},
		{"pro tier unlocks per-service", &Claims{Tier: "pro"}, FeaturePerServiceFormat, true},
		{"pro tier unlocks exit-code", &Claims{Tier: "pro"}, FeatureExitCode, true},
		{"enterprise tier unlocks pro features", &Claims{Tier: "enterprise"}, FeatureEvidenceFormat, true},
		{"unknown feature defaults to false", &Claims{Tier: "enterprise"}, "made-up-feature", false},
		{"empty tier counts as free", &Claims{Tier: ""}, FeatureEvidenceFormat, false},
		{"tier name is case-insensitive", &Claims{Tier: "PRO"}, FeatureEvidenceFormat, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsPaidTierAllowed(c.claims, c.feature); got != c.want {
				t.Errorf("IsPaidTierAllowed(%+v, %q) = %v, want %v", c.claims, c.feature, got, c.want)
			}
		})
	}
}
