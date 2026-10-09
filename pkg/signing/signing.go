/*
Copyright 2026 Anthony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package signing loads ASCII-armored GPG private keys and signs SHA256SUMS
// files with them. Providers and agents share this implementation.
package signing

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// Signer holds a parsed GPG private key used to sign SHA256SUMS files.
type Signer struct {
	entity      *openpgp.Entity
	fingerprint string
}

// LoadPrivateKey reads an ASCII-armored GPG private key from path, such as a
// mounted Secret file. The passphrase is only used when the key is protected;
// pass an empty string for unprotected keys.
func LoadPrivateKey(path, passphrase string) (*Signer, error) {
	armorBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("signing: read private key: %w", err)
	}

	return ParsePrivateKey(armorBytes, passphrase)
}

// ParsePrivateKey parses an ASCII-armored GPG private key from memory. The
// passphrase is only used when the key is protected; pass an empty string for
// unprotected keys.
func ParsePrivateKey(armorBytes []byte, passphrase string) (*Signer, error) {
	entities, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(armorBytes))
	if err != nil {
		return nil, fmt.Errorf("signing: parse private key: %w", err)
	}

	if len(entities) == 0 {
		return nil, fmt.Errorf("signing: no entities found in private key")
	}

	entity := entities[0]
	if entity.PrivateKey == nil {
		return nil, fmt.Errorf("signing: key material contains no private key")
	}

	if passphrase != "" {
		if err = decryptEntity(entity, []byte(passphrase)); err != nil {
			return nil, err
		}
	}

	return &Signer{
		entity:      entity,
		fingerprint: strings.ToUpper(hex.EncodeToString(entity.PrimaryKey.Fingerprint[:])),
	}, nil
}

// SignSHA256SUMS creates a detached binary GPG signature of sums and returns it
// along with the 40-character uppercase hex fingerprint of the signing key.
func (s *Signer) SignSHA256SUMS(sums []byte) (sig []byte, fingerprint string, err error) {
	var sigBuf bytes.Buffer
	if err = openpgp.DetachSign(&sigBuf, s.entity, bytes.NewReader(sums), nil); err != nil {
		return nil, "", fmt.Errorf("signing: sign SHA256SUMS: %w", err)
	}

	return sigBuf.Bytes(), s.fingerprint, nil
}

// SignSHA256SUMSArmored is SignSHA256SUMS with the detached signature ASCII-armored
// for storage in text fields. It returns the armored signature and the key fingerprint.
func (s *Signer) SignSHA256SUMSArmored(sums []byte) (armored string, fingerprint string, err error) {
	sig, fingerprint, err := s.SignSHA256SUMS(sums)
	if err != nil {
		return "", "", err
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.SignatureType, nil)
	if err != nil {
		return "", "", fmt.Errorf("signing: create signature armor: %w", err)
	}

	if _, err = w.Write(sig); err != nil {
		return "", "", fmt.Errorf("signing: write signature armor: %w", err)
	}

	if err = w.Close(); err != nil {
		return "", "", fmt.Errorf("signing: close signature armor: %w", err)
	}

	return buf.String(), fingerprint, nil
}

// Fingerprint returns the 40-character uppercase hex fingerprint of the primary key.
func (s *Signer) Fingerprint() string {
	return s.fingerprint
}

// PublicKeyArmor returns the ASCII-armored public key derived from the private key.
// No private key material is included in the output.
func (s *Signer) PublicKeyArmor() (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("signing: create public key armor: %w", err)
	}

	if err = s.entity.Serialize(w); err != nil {
		return "", fmt.Errorf("signing: serialize public key: %w", err)
	}

	if err = w.Close(); err != nil {
		return "", fmt.Errorf("signing: close public key armor: %w", err)
	}

	return buf.String(), nil
}

// VerifyFingerprint returns an error when the stored fingerprint does not match
// the fingerprint of the signing key. Spaces and letter case are ignored.
func VerifyFingerprint(stored string, s *Signer) error {
	normalized := strings.ToUpper(strings.Join(strings.Fields(stored), ""))
	if normalized == "" {
		return fmt.Errorf("signing: stored fingerprint is empty")
	}

	if normalized != s.fingerprint {
		return fmt.Errorf("signing: fingerprint mismatch: stored %s, key %s", normalized, s.fingerprint)
	}

	return nil
}

func decryptEntity(entity *openpgp.Entity, passphrase []byte) error {
	if entity.PrivateKey.Encrypted {
		if err := entity.PrivateKey.Decrypt(passphrase); err != nil {
			return fmt.Errorf("signing: decrypt private key: %w", err)
		}
	}

	for _, subkey := range entity.Subkeys {
		if subkey.PrivateKey != nil && subkey.PrivateKey.Encrypted {
			if err := subkey.PrivateKey.Decrypt(passphrase); err != nil {
				return fmt.Errorf("signing: decrypt subkey: %w", err)
			}
		}
	}

	return nil
}
