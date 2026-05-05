// Command generate-keypair produces a fresh Ed25519 keypair for license signing.
//
// segspec verifies license JWTs offline against an Ed25519 public key compiled
// into the binary (internal/license/pubkey.go). The matching private key lives
// only in the Stripe-webhook signing service. To rotate the keypair (or to
// stand up a new environment), run:
//
//	go run scripts/generate-keypair/main.go
//
// The output prints the public key (32 bytes hex), the seed (32 bytes hex),
// and the full crypto/ed25519 PrivateKey (64 bytes = seed||pubkey, hex). Copy
// the public-key bytes into internal/license/pubkey.go and ship the private
// key to the webhook signer via a secrets manager — never commit it.
//
// This is a deliberate one-shot tool. Re-running it produces a different key,
// which would invalidate every previously issued license, so use it sparingly.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate keypair: %v\n", err)
		os.Exit(1)
	}

	// crypto/ed25519's PrivateKey is seed||pubkey (64 bytes). The seed alone
	// (priv.Seed(), 32 bytes) is what most other Ed25519 libraries call the
	// "private key" — print both so callers can use whichever their signer
	// expects.
	seed := priv.Seed()

	fmt.Printf("PUBLIC_KEY_HEX=%s\n", hex.EncodeToString(pub))
	fmt.Printf("SEED_HEX=%s\n", hex.EncodeToString(seed))
	fmt.Printf("PRIVATE_KEY_HEX=%s\n", hex.EncodeToString(priv))
}
