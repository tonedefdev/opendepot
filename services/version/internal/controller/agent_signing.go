/*
Copyright 2026 Tony Owens.

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

package controller

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tonedefdev/opendepot/pkg/archive"
	"github.com/tonedefdev/opendepot/pkg/signing"
)

// agentSigningKeyEnv is the environment variable that holds the base64-encoded GPG private key used
// to sign agent SHA256SUMS. It is populated from the Secret referenced by version.gpg.secretName.
const agentSigningKeyEnv = "OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"

// errAgentSigningKeyNotConfigured is returned when no signing key is available to the version controller.
var errAgentSigningKeyNotConfigured = errors.New("agent signing key is not configured")

// signedAgentArchive is the signed SHA256SUMS for a stored Skill or Agent archive.
type signedAgentArchive struct {
	sums        string
	signature   string
	fingerprint string
}

// signAgentArchive builds SHA256SUMS for the stored archive and signs it with the configured GPG key.
// It is the last gate before a Skill or Agent Version is published.
func signAgentArchive(archiveBytes []byte, fileName string) (*signedAgentArchive, error) {
	keyBase64 := strings.TrimSpace(os.Getenv(agentSigningKeyEnv))
	if keyBase64 == "" {
		return nil, errAgentSigningKeyNotConfigured
	}

	armor, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode gpg private key: %w", err)
	}

	signer, err := signing.ParsePrivateKey(armor, "")
	if err != nil {
		return nil, fmt.Errorf("parse gpg private key: %w", err)
	}

	sums := fmt.Sprintf("%s  %s\n", archive.SHA256Hex(archiveBytes), fileName)
	signature, fingerprint, err := signer.SignSHA256SUMSArmored([]byte(sums))
	if err != nil {
		return nil, err
	}

	return &signedAgentArchive{sums: sums, signature: signature, fingerprint: fingerprint}, nil
}
