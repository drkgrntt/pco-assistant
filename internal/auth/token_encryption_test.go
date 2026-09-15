package auth

// Plain unit tests for encryptToken/decryptToken - no DB required (unlike
// sessions_test.go's RUN_DB_TESTS-gated tests), since these operate purely
// on strings via a Store built with an in-memory key.

import (
	"crypto/rand"
	"strings"
	"testing"
)

// testKey returns a fresh random 32-byte AES-256 key for a test.
func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, tokenEncryptionKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return key
}

func TestEncryptDecryptTokenRoundTrip(t *testing.T) {
	store := &Store{tokenEncryptionKey: testKey(t)}

	const plaintext = "some-real-pco-access-token"
	encrypted, err := store.encryptToken(plaintext)
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("encryptToken with a real key returned the plaintext unchanged - want it actually encrypted")
	}
	if !strings.HasPrefix(encrypted, tokenEncryptionPrefix) {
		t.Fatalf("encryptToken result %q missing %q prefix", encrypted, tokenEncryptionPrefix)
	}

	decrypted, err := store.decryptToken(encrypted)
	if err != nil {
		t.Fatalf("decryptToken: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decryptToken round-trip = %q, want %q", decrypted, plaintext)
	}
}

func TestDecryptTokenPassesThroughUnprefixedPlaintext(t *testing.T) {
	store := &Store{tokenEncryptionKey: testKey(t)}

	const legacyPlaintext = "legacy-unencrypted-token"
	decrypted, err := store.decryptToken(legacyPlaintext)
	if err != nil {
		t.Fatalf("decryptToken (legacy plaintext): unexpected error: %v", err)
	}
	if decrypted != legacyPlaintext {
		t.Errorf("decryptToken (legacy plaintext) = %q, want %q unchanged", decrypted, legacyPlaintext)
	}
}

func TestEncryptTokenNoOpWithoutKey(t *testing.T) {
	store := &Store{} // tokenEncryptionKey left nil

	const plaintext = "some-token"
	encrypted, err := store.encryptToken(plaintext)
	if err != nil {
		t.Fatalf("encryptToken (no key): unexpected error: %v", err)
	}
	if encrypted != plaintext {
		t.Errorf("encryptToken (no key) = %q, want %q unchanged (no prefix)", encrypted, plaintext)
	}
	if strings.HasPrefix(encrypted, tokenEncryptionPrefix) {
		t.Errorf("encryptToken (no key) result %q unexpectedly carries the %q prefix", encrypted, tokenEncryptionPrefix)
	}
}

func TestDecryptTokenNoOpWithoutKeyForUnprefixedValue(t *testing.T) {
	store := &Store{} // tokenEncryptionKey left nil

	const plaintext = "some-token"
	decrypted, err := store.decryptToken(plaintext)
	if err != nil {
		t.Fatalf("decryptToken (no key, unprefixed): unexpected error: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decryptToken (no key, unprefixed) = %q, want %q unchanged", decrypted, plaintext)
	}
}

func TestDecryptTokenErrorsOnPrefixedValueWithoutKey(t *testing.T) {
	// A value that was encrypted while a key was configured, later
	// decrypted after the key was removed from the environment - the one
	// accepted misconfiguration risk documented in token_encryption.go.
	encryptingStore := &Store{tokenEncryptionKey: testKey(t)}
	encrypted, err := encryptingStore.encryptToken("some-token")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	decryptingStore := &Store{} // no key configured
	if _, err := decryptingStore.decryptToken(encrypted); err == nil {
		t.Error("decryptToken with a prefixed value but no key configured returned no error, want one")
	}
}

func TestDecryptTokenErrorsOnCorruptedCiphertext(t *testing.T) {
	store := &Store{tokenEncryptionKey: testKey(t)}

	encrypted, err := store.encryptToken("some-token")
	if err != nil {
		t.Fatalf("encryptToken: %v", err)
	}

	// Truncate the encrypted value (but keep the prefix) to simulate
	// corruption - must error, not panic or return garbage.
	truncated := encrypted[:len(encrypted)-4]
	if _, err := store.decryptToken(truncated); err == nil {
		t.Error("decryptToken on truncated ciphertext returned no error, want one")
	}

	// A different key must also fail to decrypt (bad auth tag), not
	// silently return garbage.
	otherStore := &Store{tokenEncryptionKey: testKey(t)}
	if _, err := otherStore.decryptToken(encrypted); err == nil {
		t.Error("decryptToken with the wrong key returned no error, want one")
	}

	// A prefixed value whose base64 is itself invalid must error too.
	if _, err := store.decryptToken(tokenEncryptionPrefix + "!!!not-valid-base64!!!"); err == nil {
		t.Error("decryptToken on invalid base64 returned no error, want one")
	}
}
