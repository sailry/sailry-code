package ssh

import (
	"crypto/ed25519"
	"errors"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

func TestHostKeyTrust(t *testing.T) {
	public := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public()
	key, err := cryptossh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	var required *HostKeyRequired
	if err := VerifyHostKey(nil, key); !errors.As(err, &required) || required.Changed {
		t.Fatal("first-use SSH key did not require explicit trust")
	}
	trusted := required.Key
	if err := VerifyHostKey(&trusted, key); err != nil {
		t.Fatal("explicitly trusted SSH key was rejected")
	}
	trusted.Fingerprint = "SHA256:changed"
	if err := VerifyHostKey(&trusted, key); !errors.As(err, &required) || !required.Changed {
		t.Fatal("changed SSH key did not require explicit trust")
	}
}
