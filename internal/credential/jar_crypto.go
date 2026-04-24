package credential

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// jarMagic is the 4-byte file prefix identifying an encrypted jar. Format:
//
//	"AGD1" || nonce(12) || ciphertext+tag(N)
//
// "AGD" = agent-deepweb, "1" = format version. A future format change
// (different cipher, larger nonce) bumps the digit.
var jarMagic = []byte("AGD1")

// errMissingJarKey indicates a profile that should have a JarKey doesn't.
// This is a bug for ReadJar/WriteJar (Store provisions one at add time)
// but a useful sentinel for callers that want to distinguish "jar absent"
// from "key absent."
var errMissingJarKey = errors.New("profile has no jar encryption key")

// generateJarKey returns 32 random bytes suitable for AES-256-GCM. Used
// by Store when provisioning a new profile.
func generateJarKey() ([]byte, error) {
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// jarKeyFor loads the JarKey for the named profile. Returns
// errMissingJarKey if the profile has no key yet.
func jarKeyFor(name string) ([]byte, error) {
	r, err := Resolve(name)
	if err != nil {
		return nil, err
	}
	if len(r.Secrets.JarKey) == 0 {
		return nil, errMissingJarKey
	}
	if len(r.Secrets.JarKey) != 32 {
		return nil, fmt.Errorf("jar key for %q is %d bytes, expected 32", name, len(r.Secrets.JarKey))
	}
	return r.Secrets.JarKey, nil
}

// sealJarFrame produces magic || nonce || ciphertext+tag from the given
// key + plaintext. Pure: no I/O, no profile lookup. Errors only on
// crypto-construction failures (e.g. wrong key length) or rand-reader
// failures.
func sealJarFrame(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(jarMagic)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, jarMagic...)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// openJarFrame is the inverse of sealJarFrame. Refuses to operate on
// data without the magic prefix — there is no plaintext fallback. Pure:
// errors are descriptive but contain no profile-name context (the
// caller wraps with that).
func openJarFrame(key, frame []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	body, err := parseJarFrame(frame, gcm.NonceSize(), gcm.Overhead())
	if err != nil {
		return nil, err
	}
	nonce := body[:gcm.NonceSize()]
	ciphertext := body[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt failed (key mismatch?): %w", err)
	}
	return plaintext, nil
}

// parseJarFrame strips the magic prefix and validates that the remainder
// is at least one nonce + one tag long. Returns the post-magic body.
func parseJarFrame(frame []byte, nonceSize, overhead int) ([]byte, error) {
	if len(frame) < len(jarMagic) || !bytes.Equal(frame[:len(jarMagic)], jarMagic) {
		return nil, errors.New("unrecognised format (wrong magic)")
	}
	body := frame[len(jarMagic):]
	if len(body) < nonceSize+overhead {
		return nil, errors.New("truncated frame")
	}
	return body, nil
}

// newGCM builds an AES-GCM AEAD cipher from a key. Centralised so the
// cipher choice (AES vs ChaCha20-Poly1305, GCM vs SIV) lives in one
// place and a future format-version bump only edits this function.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptJarBytes encrypts plaintext for the named profile.
func encryptJarBytes(name string, plaintext []byte) ([]byte, error) {
	key, err := jarKeyFor(name)
	if err != nil {
		return nil, err
	}
	return sealJarFrame(key, plaintext)
}

// decryptJarBytes is the inverse of encryptJarBytes. Wraps errors with
// the profile name for caller-friendly diagnostics. Magic is checked
// before the key lookup so a malformed file produces a more useful
// error than a missing-key one.
func decryptJarBytes(name string, data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, jarMagic) {
		return nil, fmt.Errorf("jar for %q: unrecognised format (wrong magic)", name)
	}
	key, err := jarKeyFor(name)
	if err != nil {
		return nil, fmt.Errorf("jar for %q: %w", name, err)
	}
	plaintext, err := openJarFrame(key, data)
	if err != nil {
		return nil, fmt.Errorf("jar for %q: %w", name, err)
	}
	return plaintext, nil
}
