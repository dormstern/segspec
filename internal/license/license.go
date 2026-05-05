// Package license implements offline JWT license-key validation for segspec.
//
// Licenses are signed JWTs (alg "EdDSA") issued by the segspec licensing
// service and verified locally against a public key compiled into the binary.
// This package performs ZERO network calls — verification is fully offline,
// in keeping with segspec's identity ("static, offline config-to-policy
// extractor"). See IDENTITY.md.
//
// The JWT verifier here is a small, purpose-built implementation that handles
// only the alg "EdDSA" (Ed25519) over crypto/ed25519. We avoid pulling in a
// general-purpose JWT library to keep the dependency surface minimal and the
// verification path auditable.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims is the set of claims segspec extracts from a license token.
type Claims struct {
	// Org is the organization the license was issued to (jwt "sub").
	Org string
	// Tier is the subscription tier: "free", "pro", or "enterprise".
	Tier string
	// ExpiresAt is the license expiry (jwt "exp").
	ExpiresAt time.Time
}

// rawClaims mirrors the on-the-wire JWT payload.
type rawClaims struct {
	Sub  string `json:"sub"`
	Tier string `json:"tier"`
	Exp  int64  `json:"exp"`
}

// rawHeader mirrors the on-the-wire JWT header.
type rawHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

// Errors exposed by Validate. Callers can errors.Is against these to detect
// specific failure modes.
var (
	ErrMalformedToken = errors.New("license token is malformed")
	ErrUnsupportedAlg = errors.New("license token uses unsupported algorithm (expected EdDSA)")
	ErrBadSignature   = errors.New("license token signature is invalid")
	ErrExpired        = errors.New("license has expired")
)

// ExpiredError carries the expiry time so callers can render user-friendly
// messages like "License expired YYYY-MM-DD".
type ExpiredError struct {
	ExpiredAt time.Time
}

func (e *ExpiredError) Error() string {
	return fmt.Sprintf("license expired %s", e.ExpiredAt.UTC().Format("2006-01-02"))
}

func (e *ExpiredError) Is(target error) bool {
	return target == ErrExpired
}

// Validate verifies an Ed25519-signed JWT (alg "EdDSA") and returns its claims.
// It is fully offline — no network calls, no clock-skew tolerance beyond
// trusting the local clock. If the token is expired, the returned error wraps
// ErrExpired and is of type *ExpiredError.
func Validate(token string, publicKey ed25519.PublicKey) (*Claims, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: invalid public key size %d", ErrMalformedToken, len(publicKey))
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrMalformedToken, len(parts))
	}
	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]

	headerBytes, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return nil, fmt.Errorf("%w: header not valid base64url: %v", ErrMalformedToken, err)
	}
	var hdr rawHeader
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return nil, fmt.Errorf("%w: header not valid JSON: %v", ErrMalformedToken, err)
	}
	if hdr.Alg != "EdDSA" {
		return nil, fmt.Errorf("%w: got %q", ErrUnsupportedAlg, hdr.Alg)
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("%w: signature not valid base64url: %v", ErrMalformedToken, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: signature has wrong length %d", ErrBadSignature, len(sig))
	}

	signingInput := []byte(headerB64 + "." + payloadB64)
	if !ed25519.Verify(publicKey, signingInput, sig) {
		return nil, ErrBadSignature
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("%w: payload not valid base64url: %v", ErrMalformedToken, err)
	}
	var raw rawClaims
	if err := json.Unmarshal(payloadBytes, &raw); err != nil {
		return nil, fmt.Errorf("%w: payload not valid JSON: %v", ErrMalformedToken, err)
	}
	if raw.Exp == 0 {
		return nil, fmt.Errorf("%w: missing exp claim", ErrMalformedToken)
	}
	expiresAt := time.Unix(raw.Exp, 0).UTC()
	if time.Now().UTC().After(expiresAt) {
		return nil, &ExpiredError{ExpiredAt: expiresAt}
	}

	return &Claims{
		Org:       raw.Sub,
		Tier:      strings.ToLower(strings.TrimSpace(raw.Tier)),
		ExpiresAt: expiresAt,
	}, nil
}

// Feature names recognized by IsPaidTierAllowed. Keep these as string constants
// rather than enums so callers in cmd/ can pass through cobra flag values
// directly without an extra mapping layer.
const (
	FeatureExitCode        = "exit-code"
	FeatureEvidenceFormat  = "evidence-format"
	FeaturePerServiceFormat = "per-service-format"
)

// featureMinTier returns the minimum tier that unlocks a feature.
// "pro" is the entry-level paid tier; "enterprise" is reserved for features
// that need higher commitment (none yet, but the table is here for growth).
var featureMinTier = map[string]string{
	FeatureExitCode:         "pro",
	FeatureEvidenceFormat:   "pro",
	FeaturePerServiceFormat: "pro",
}

// tierRank assigns a numeric weight so we can compare tiers with >=.
func tierRank(tier string) int {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "enterprise":
		return 2
	case "pro":
		return 1
	default:
		return 0
	}
}

// IsPaidTierAllowed reports whether the given (possibly-nil) claims unlock a
// paid feature. Nil claims (no license provided) always return false.
// Unknown feature names return false defensively.
func IsPaidTierAllowed(claims *Claims, feature string) bool {
	if claims == nil {
		return false
	}
	required, ok := featureMinTier[feature]
	if !ok {
		return false
	}
	return tierRank(claims.Tier) >= tierRank(required)
}
