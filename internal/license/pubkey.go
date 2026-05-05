package license

import "crypto/ed25519"

// ProductionPublicKey is the Ed25519 public key used to verify license tokens
// issued by the segspec licensing service.
//
// This key is hardcoded into the binary on purpose: license verification must
// be fully offline, and trusting a key fetched at runtime would defeat that
// guarantee. To rotate the key, ship a new release.
//
// Generated locally with `go test ./scripts/generate-keypair/ -run TestEmitKeypair
// -emit-keypair` (the canonical tool is scripts/generate-keypair/main.go,
// runnable via `go run`). The matching private key lives only in the Stripe-
// webhook signer; the development-only copy is in DEVELOPMENT_PRIVATE_KEY.txt
// (gitignored). To rotate, regenerate the keypair, replace the bytes here,
// update DEVELOPMENT_PRIVATE_KEY.txt + the devPrivateKeyHex constants in
// internal/license/license_test.go and cmd/license_gate_test.go, sync
// web/PUBLIC_KEY_FOR_GO_AGENT.txt for the Stripe agent, and ship a new
// release. TestValidate guards against accidental drift.
//
// Public key (hex): f9b633b9fc16148bd15b024af8f694c7a0942d6ca3e0ed367d0010dda57689a3
var ProductionPublicKey ed25519.PublicKey = ed25519.PublicKey{
	0xf9, 0xb6, 0x33, 0xb9, 0xfc, 0x16, 0x14, 0x8b,
	0xd1, 0x5b, 0x02, 0x4a, 0xf8, 0xf6, 0x94, 0xc7,
	0xa0, 0x94, 0x2d, 0x6c, 0xa3, 0xe0, 0xed, 0x36,
	0x7d, 0x00, 0x10, 0xdd, 0xa5, 0x76, 0x89, 0xa3,
}
