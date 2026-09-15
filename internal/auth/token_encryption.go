package auth

// This file encrypts sessions.access_token/refresh_token at the app layer
// (AES-256-GCM) rather than relying on disk/volume encryption alone - see
// docs/plans/not-started/security-hardening-followups-plan.md's "PCO
// access/refresh tokens stored unencrypted at rest" item for why: this is
// multi-tenant SaaS holding real third-party PCO OAuth grants, and a DB
// leak today would hand out live PCO access for every connected
// organization, not just this app's own data.
//
// Key source: SESSION_TOKEN_ENCRYPTION_KEY, a base64-standard-encoded
// 32-byte key, loaded once at process startup by
// LoadTokenEncryptionKeyFromEnv (below) and threaded into NewStore. The key
// is deliberately optional, matching this repo's existing Stripe/Mailgun/R2
// "optional for local dev" convention (see README's Setup section): a Store
// built with a nil/empty key encrypts as a no-op pass-through (returns the
// plaintext unchanged, no prefix) and decrypts unprefixed values unchanged
// too, so local dev never needs a real key configured. LoadTokenEncryptionKeyFromEnv
// itself still fails hard (log + os.Exit(1)) if the env var IS set but is
// malformed - see its own doc comment.
//
// Format: an encrypted value is stored as the literal prefix "enc:v1:"
// followed by base64-standard-encoding of (nonce || ciphertext), where
// ciphertext is AES-256-GCM's sealed output (which already includes its own
// auth tag) with a fresh random nonce generated per call. decryptToken
// checks for that exact prefix first: present -> base64-decode, split off
// the nonce, and open the AEAD; absent -> the value predates encryption (or
// encryption is disabled) and is legacy plaintext, returned unchanged.
//
// This makes the plaintext -> encrypted transition self-healing, with no
// migration/backfill script needed: every session naturally rewrites its own
// access_token/refresh_token via UpdateSessionTokens/UpdateSessionTokensByID
// on its next PCO token refresh (automatic every ~2 hours per
// middleware.RequireAuth), so existing live sessions upgrade to encrypted
// storage on their own within hours of this shipping.
//
// Accepted, self-inflicted risk: a session whose stored value already
// carries the "enc:v1:" prefix but is later decrypted with no key configured
// (e.g. SESSION_TOKEN_ENCRYPTION_KEY was set when the value was written,
// then removed from the environment) will fail to decrypt with a real
// error, not silently fall back to plaintext-shaped garbage. That's treated
// as a misconfiguration the operator caused, not something to add fallback
// handling for.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"pco-assistant/internal/logger"
)

// tokenEncryptionPrefix tags a stored value as ciphertext produced by
// encryptToken, distinguishing it from legacy (or encryption-disabled)
// plaintext - see this file's doc comment for the full format.
const tokenEncryptionPrefix = "enc:v1:"

// tokenEncryptionKeySize is the required raw key length for AES-256 - a
// base64-decoded SESSION_TOKEN_ENCRYPTION_KEY that isn't exactly this many
// bytes is a real misconfiguration (see LoadTokenEncryptionKeyFromEnv).
const tokenEncryptionKeySize = 32

// LoadTokenEncryptionKeyFromEnv reads, decodes, and validates
// SESSION_TOKEN_ENCRYPTION_KEY once at process startup, shared by every
// binary that constructs an auth.Store (the main api server, plus the
// dbseed/pcoreset dev CLI tools, which only ever read a session but still
// need the same key to decrypt an already-encrypted token) so the
// getenv/decode/validate dance isn't duplicated three times with subtly
// different behavior.
//
// The env var is optional, matching this repo's Stripe/Mailgun/R2
// "optional for local dev" convention: unset -> returns nil (encryption
// disabled, see encryptToken/decryptToken) after logging a one-time WARN so
// this is never silently forgotten ahead of a real deployment. Set but
// malformed (fails base64-standard decode, or doesn't decode to exactly 32
// bytes) is treated as a real misconfiguration, not something to limp
// along with a bad/partial key for - logs the problem and exits the
// process. Generate a valid key with `openssl rand -base64 32`.
func LoadTokenEncryptionKeyFromEnv() []byte {
	raw := os.Getenv("SESSION_TOKEN_ENCRYPTION_KEY")
	if raw == "" {
		logger.Warn("SESSION_TOKEN_ENCRYPTION_KEY not set - PCO OAuth tokens will be stored unencrypted; set this before any real production/public deployment")
		return nil
	}

	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		logger.Fatal("SESSION_TOKEN_ENCRYPTION_KEY is not valid base64 - generate one with `openssl rand -base64 32`", "error", err)
	}
	if len(key) != tokenEncryptionKeySize {
		logger.Fatal(fmt.Sprintf("SESSION_TOKEN_ENCRYPTION_KEY must decode to exactly %d bytes for AES-256, got %d - generate one with `openssl rand -base64 32`", tokenEncryptionKeySize, len(key)))
	}

	return key
}

// encryptToken encrypts plaintext for storage, returning it unchanged (no
// prefix) when s has no encryption key configured - see this file's doc
// comment on why that's a deliberate no-op rather than an error.
func (s *Store) encryptToken(plaintext string) (string, error) {
	if len(s.tokenEncryptionKey) == 0 {
		return plaintext, nil
	}

	block, err := aes.NewCipher(s.tokenEncryptionKey)
	if err != nil {
		return "", fmt.Errorf("encrypt token: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encrypt token: create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("encrypt token: generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return tokenEncryptionPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptToken reverses encryptToken. A value without the "enc:v1:" prefix
// is legacy (or encryption-disabled) plaintext and is returned unchanged,
// regardless of whether s has a key configured - this is what lets existing
// plaintext rows keep working with no explicit backfill. A value *with* the
// prefix is always decrypted (and fails with a real error, not a silent
// pass-through, if that fails) - see this file's doc comment on the one
// accepted misconfiguration risk this implies.
func (s *Store) decryptToken(ciphertext string) (string, error) {
	rest, ok := strings.CutPrefix(ciphertext, tokenEncryptionPrefix)
	if !ok {
		return ciphertext, nil
	}

	if len(s.tokenEncryptionKey) == 0 {
		return "", errors.New("decrypt token: value is encrypted (enc:v1: prefix) but no SESSION_TOKEN_ENCRYPTION_KEY is configured")
	}

	sealed, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		return "", fmt.Errorf("decrypt token: base64 decode: %w", err)
	}

	block, err := aes.NewCipher(s.tokenEncryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt token: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decrypt token: create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("decrypt token: ciphertext too short to contain a nonce")
	}
	nonce, sealedRest := sealed[:nonceSize], sealed[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, sealedRest, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}

	return string(plaintext), nil
}
