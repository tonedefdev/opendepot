package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// verifySHA256SUMS checks the detached signature over SHA256SUMS against the advertised keys and
// returns the uppercase fingerprint of the key that produced it.
func verifySHA256SUMS(sums, sig []byte, armoredKeys []string) (string, error) {
	var ring openpgp.EntityList
	for _, a := range armoredKeys {
		keys, err := openpgp.ReadArmoredKeyRing(strings.NewReader(a))
		if err != nil {
			return "", fmt.Errorf("parse advertised signing key: %w", err)
		}

		ring = append(ring, keys...)
	}

	if len(ring) == 0 {
		return "", fmt.Errorf("registry advertised no signing keys")
	}

	binary, err := dearmorSignature(sig)
	if err != nil {
		return "", err
	}

	signer, err := openpgp.CheckDetachedSignature(ring, bytes.NewReader(sums), bytes.NewReader(binary))
	if err != nil {
		return "", fmt.Errorf("SHA256SUMS signature does not verify: %w", err)
	}

	return fmt.Sprintf("%X", signer.PrimaryKey.Fingerprint[:]), nil
}

// dearmorSignature returns the binary OpenPGP signature packet for an armored or binary signature.
func dearmorSignature(sig []byte) ([]byte, error) {
	if !bytes.Contains(sig, []byte("-----BEGIN PGP SIGNATURE-----")) {
		return sig, nil
	}

	block, err := armor.Decode(bytes.NewReader(sig))
	if err != nil {
		return nil, fmt.Errorf("decode SHA256SUMS.sig: %w", err)
	}

	binary, err := io.ReadAll(block.Body)
	if err != nil {
		return nil, fmt.Errorf("decode SHA256SUMS.sig: %w", err)
	}

	return binary, nil
}

// signatureHash returns the sha256: hash of the binary SHA256SUMS signature. The lock records it so
// a re-resolved version must carry the same signature it was first installed with.
func signatureHash(sig []byte) (string, error) {
	binary, err := dearmorSignature(sig)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(binary)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// decideSigningKey applies the key trust rules and returns the fingerprint to pin.
//
// A configured signing_key must match the signer and never prompts. Otherwise the lock's pinned
// key must match. The first install pins the signer (TOFU). A rotated key is re-pinned only with
// trustNewKey, fails under noPrompt, and otherwise requires confirmation.
func decideSigningKey(configPin, lockPin, signer string, trustNewKey, noPrompt bool, confirm func(string) bool) (string, error) {
	if configPin != "" {
		if configPin != signer {
			return "", fmt.Errorf("signing key %s does not match configured signing_key %s", signer, configPin)
		}

		return signer, nil
	}

	if lockPin == "" || lockPin == signer {
		return signer, nil
	}

	if trustNewKey {
		return signer, nil
	}

	if noPrompt {
		return "", fmt.Errorf("signing key changed from %s to %s; re-run with -trust-new-key to re-pin", lockPin, signer)
	}

	if !confirm(fmt.Sprintf("Signing key changed from %s to %s. Trust the new key?", lockPin, signer)) {
		return "", fmt.Errorf("signing key %s was not trusted", signer)
	}

	return signer, nil
}
