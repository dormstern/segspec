package license

import "crypto/ed25519"

// ProductionPublicKey is the Ed25519 public key used to verify license tokens
// issued by the segspec licensing service.
//
// This key is hardcoded into the binary on purpose: license verification must
// be fully offline, and trusting a key fetched at runtime would defeat that
// guarantee. To rotate the key, ship a new release.
//
// Production / development separation:
//   - The matching production private key lives ONLY in the licensing-service
//     environment (e.g. Vercel env var SEGSPEC_LICENSE_PRIVATE_KEY). It is
//     never committed to this repository.
//   - The development keypair in DEVELOPMENT_PRIVATE_KEY.txt (gitignored) is
//     intentionally a DIFFERENT keypair. Tests sign and verify using the dev
//     keypair only — they never touch ProductionPublicKey at runtime. This
//     means a leaked dev key cannot mint tokens that any shipped binary will
//     accept.
//
// Rotating in production: regenerate via `go run scripts/generate-keypair`,
// replace the bytes here, save the new private key to
// PRODUCTION_PRIVATE_KEY.txt (gitignored) for transfer into the licensing
// service env, and ship a new release. The dev keypair stays put.
//
// Public key (hex): fa5bff61e145065cb8a2bb9a2d060bc15e36a3829d1541b26fe1a42853888264
var ProductionPublicKey ed25519.PublicKey = ed25519.PublicKey{
	0xfa, 0x5b, 0xff, 0x61, 0xe1, 0x45, 0x06, 0x5c,
	0xb8, 0xa2, 0xbb, 0x9a, 0x2d, 0x06, 0x0b, 0xc1,
	0x5e, 0x36, 0xa3, 0x82, 0x9d, 0x15, 0x41, 0xb2,
	0x6f, 0xe1, 0xa4, 0x28, 0x53, 0x88, 0x82, 0x64,
}
