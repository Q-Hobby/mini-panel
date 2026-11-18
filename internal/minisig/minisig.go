// Package minisig checks signatures in the format of minisign (https://jedisct1.github.io/minisign/), the format the releases of
// mpn are signed in. It reads only what mpn needs: an Ed25519 key, and a signature of the BLAKE2b-512 hash of the file ("ED", the
// default since minisign 0.11), with its trusted comment, which is signed too. It is a few lines over the standard library and
// golang.org/x/crypto, so checking an update needs no program on the machine.
package minisig

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// PublicKey is a minisign public key: its 8-byte id and the Ed25519 key.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

// ParsePublicKey reads the base64 line of a minisign public key ("RWQ..."), or a whole .pub file.
func ParsePublicKey(s string) (PublicKey, error) {
	line := lastLine(s)
	b, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(b) != 2+8+32 {
		return PublicKey{}, errors.New("minisig: not a minisign public key")
	}
	if string(b[:2]) != "Ed" {
		return PublicKey{}, errors.New("minisig: the key is not an Ed25519 key")
	}
	var k PublicKey
	copy(k.ID[:], b[2:10])
	k.Key = ed25519.PublicKey(append([]byte(nil), b[10:]...))
	return k, nil
}

// lastLine is the last line of s that is not empty and not a comment.
func lastLine(s string) string {
	var out string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "untrusted comment:") {
			out = l
		}
	}
	return out
}

// Signature is a parsed .minisig file.
type Signature struct {
	KeyID          [8]byte
	sig            []byte
	TrustedComment string
	global         []byte
}

// ParseSignature reads a .minisig file: an untrusted comment, the signature, the trusted comment and its signature.
func ParseSignature(s string) (Signature, error) {
	lines := []string{}
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "untrusted comment:") || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return Signature{}, errors.New("minisig: not a minisign signature")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(b) != 2+8+64 {
		return Signature{}, errors.New("minisig: the signature line is damaged")
	}
	if string(b[:2]) != "ED" {
		// "Ed" signs the file itself, which minisign no longer makes by default; mpn accepts the hashed form only.
		return Signature{}, errors.New("minisig: the signature is not of the hashed kind (ED)")
	}
	g, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || len(g) != 64 {
		return Signature{}, errors.New("minisig: the signature of the trusted comment is damaged")
	}
	var sg Signature
	copy(sg.KeyID[:], b[2:10])
	sg.sig = b[10:]
	sg.TrustedComment = strings.TrimPrefix(lines[2], "trusted comment: ")
	sg.global = g
	return sg, nil
}

// Verify checks that sig signs data with key, and that its trusted comment was signed with it too.
func Verify(key PublicKey, data []byte, sig Signature) error {
	if sig.KeyID != key.ID {
		return fmt.Errorf("minisig: signed with key %X, not with key %X", sig.KeyID, key.ID)
	}
	h := blake2b.Sum512(data)
	if !ed25519.Verify(key.Key, h[:], sig.sig) {
		return errors.New("minisig: the signature does not match the file")
	}
	if !ed25519.Verify(key.Key, append(append([]byte(nil), sig.sig...), sig.TrustedComment...), sig.global) {
		return errors.New("minisig: the trusted comment was not signed with this key")
	}
	return nil
}
