package signing

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/testutils"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

var fingerprintPattern = regexp.MustCompile(`^[0-9A-F]{40}$`)

type testKey struct {
	keyID          string
	publicArmor    string
	privateArmor   []byte
	privateKeyPath string
}

// gpgHome returns a short-path GNUPGHOME. gpg-agent places its socket inside
// GNUPGHOME, and macOS temp paths are too long for the Unix socket limit.
func gpgHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "ods-gpg")
	if err != nil {
		t.Fatalf("create gpg home: %v", err)
	}

	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatalf("chmod gpg home: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("gpgconf", "--homedir", dir, "--kill", "all").Run()
		os.RemoveAll(dir)
	})

	return dir
}

func newTestKey(t *testing.T) testKey {
	t.Helper()

	home := gpgHome(t)
	t.Setenv("GNUPGHOME", home)

	keyID, publicArmor, privateBase64, err := testutils.GenerateTestGPGKeyPair(home)
	if err != nil {
		t.Fatalf("generate test gpg key pair: %v", err)
	}

	privateArmor, err := base64.StdEncoding.DecodeString(privateBase64)
	if err != nil {
		t.Fatalf("decode test private key: %v", err)
	}

	privateKeyPath := filepath.Join(t.TempDir(), "private.asc")
	if err = os.WriteFile(privateKeyPath, privateArmor, 0600); err != nil {
		t.Fatalf("write test private key: %v", err)
	}

	return testKey{
		keyID:          keyID,
		publicArmor:    publicArmor,
		privateArmor:   privateArmor,
		privateKeyPath: privateKeyPath,
	}
}

func TestSignSHA256SUMSVerifiesWithPublicKey(t *testing.T) {
	key := newTestKey(t)

	signer, err := LoadPrivateKey(key.privateKeyPath, "")
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}

	sums := []byte("abc123  terraform-provider-example_1.0.0_linux_amd64.zip\n")
	sig, fingerprint, err := signer.SignSHA256SUMS(sums)
	if err != nil {
		t.Fatalf("SignSHA256SUMS: %v", err)
	}

	if fingerprint != signer.Fingerprint() {
		t.Fatalf("fingerprint returned by SignSHA256SUMS = %q, want %q", fingerprint, signer.Fingerprint())
	}

	publicArmor, err := signer.PublicKeyArmor()
	if err != nil {
		t.Fatalf("PublicKeyArmor: %v", err)
	}

	keyRing, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicArmor))
	if err != nil {
		t.Fatalf("read derived public key: %v", err)
	}

	if _, err = openpgp.CheckDetachedSignature(keyRing, strings.NewReader(string(sums)), strings.NewReader(string(sig))); err != nil {
		t.Fatalf("signature does not verify with derived public key: %v", err)
	}

	if _, err = openpgp.CheckDetachedSignature(keyRing, strings.NewReader("tampered"), strings.NewReader(string(sig))); err == nil {
		t.Fatal("signature verified for tampered SHA256SUMS content")
	}
}

func TestFingerprintFormatAndStability(t *testing.T) {
	key := newTestKey(t)

	first, err := LoadPrivateKey(key.privateKeyPath, "")
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}

	second, err := ParsePrivateKey(key.privateArmor, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}

	if !fingerprintPattern.MatchString(first.Fingerprint()) {
		t.Fatalf("fingerprint %q is not 40 uppercase hex characters", first.Fingerprint())
	}

	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("fingerprint differs between loads: %q != %q", first.Fingerprint(), second.Fingerprint())
	}

	if !strings.HasSuffix(first.Fingerprint(), key.keyID) {
		t.Fatalf("fingerprint %q does not end with key ID %q", first.Fingerprint(), key.keyID)
	}
}

func TestPublicKeyArmorOmitsPrivateMaterial(t *testing.T) {
	key := newTestKey(t)

	signer, err := ParsePrivateKey(key.privateArmor, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}

	publicArmor, err := signer.PublicKeyArmor()
	if err != nil {
		t.Fatalf("PublicKeyArmor: %v", err)
	}

	if !strings.Contains(publicArmor, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatal("public key armor has unexpected block type")
	}

	block, err := armor.Decode(strings.NewReader(publicArmor))
	if err != nil {
		t.Fatalf("decode public key armor: %v", err)
	}

	if block.Type != openpgp.PublicKeyType {
		t.Fatalf("armor block type = %q, want %q", block.Type, openpgp.PublicKeyType)
	}

	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicArmor))
	if err != nil {
		t.Fatalf("read public key armor: %v", err)
	}

	if entities[0].PrivateKey != nil {
		t.Fatal("public key armor contains private key material")
	}
}

func TestVerifyFingerprint(t *testing.T) {
	key := newTestKey(t)

	signer, err := ParsePrivateKey(key.privateArmor, "")
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}

	if err = VerifyFingerprint(signer.Fingerprint(), signer); err != nil {
		t.Fatalf("matching fingerprint returned error: %v", err)
	}

	lowerSpaced := strings.ToLower(signer.Fingerprint()[:20] + " " + signer.Fingerprint()[20:])
	if err = VerifyFingerprint(lowerSpaced, signer); err != nil {
		t.Fatalf("normalized fingerprint returned error: %v", err)
	}

	mismatched := strings.Repeat("0", 40)
	if mismatched == signer.Fingerprint() {
		mismatched = strings.Repeat("F", 40)
	}

	if err = VerifyFingerprint(mismatched, signer); err == nil {
		t.Fatal("mismatched fingerprint did not return an error")
	}

	if err = VerifyFingerprint("", signer); err == nil {
		t.Fatal("empty fingerprint did not return an error")
	}
}

func TestParsePrivateKeyErrors(t *testing.T) {
	key := newTestKey(t)

	if _, err := ParsePrivateKey([]byte("not a gpg key"), ""); err == nil {
		t.Fatal("ParsePrivateKey accepted invalid armor")
	}

	if _, err := ParsePrivateKey(nil, ""); err == nil {
		t.Fatal("ParsePrivateKey accepted empty input")
	}

	publicOnly := []byte(key.publicArmor)
	if _, err := ParsePrivateKey(publicOnly, ""); err == nil {
		t.Fatal("ParsePrivateKey accepted a public key as a private key")
	}

	if _, err := LoadPrivateKey(filepath.Join(t.TempDir(), "missing.asc"), ""); err == nil {
		t.Fatal("LoadPrivateKey accepted a missing file")
	}
}

func TestEncryptedPrivateKeyPassphrase(t *testing.T) {
	home := gpgHome(t)
	t.Setenv("GNUPGHOME", home)
	passphrase := "opendepot-test-passphrase"

	gen := exec.Command("gpg", "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", passphrase,
		"--quick-gen-key", "OpenDepot Passphrase Test <passphrase@opendepot.defdev.io>", "rsa2048", "sign", "0")
	gen.Env = append(os.Environ(), "GNUPGHOME="+home)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate passphrase-protected key: %v: %s", err, out)
	}

	export := exec.Command("gpg", "--batch", "--yes", "--pinentry-mode", "loopback", "--passphrase", passphrase,
		"--armor", "--export-secret-keys")
	export.Env = append(os.Environ(), "GNUPGHOME="+home)
	privateArmor, err := export.Output()
	if err != nil {
		t.Fatalf("export passphrase-protected key: %v", err)
	}

	if _, err = ParsePrivateKey(privateArmor, "wrong-passphrase"); err == nil {
		t.Fatal("ParsePrivateKey accepted a wrong passphrase")
	}

	signer, err := ParsePrivateKey(privateArmor, passphrase)
	if err != nil {
		t.Fatalf("ParsePrivateKey with passphrase: %v", err)
	}

	if _, _, err = signer.SignSHA256SUMS([]byte("abc  file.zip\n")); err != nil {
		t.Fatalf("SignSHA256SUMS after decrypt: %v", err)
	}
}

func TestSignSHA256SUMSArmoredVerifiesWithPublicKey(t *testing.T) {
	key := newTestKey(t)

	signer, err := LoadPrivateKey(key.privateKeyPath, "")
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}

	sums := []byte("abc123  agent.tar.gz\n")
	armored, fingerprint, err := signer.SignSHA256SUMSArmored(sums)
	if err != nil {
		t.Fatalf("SignSHA256SUMSArmored: %v", err)
	}

	if fingerprint != signer.Fingerprint() {
		t.Fatalf("fingerprint = %q, want %q", fingerprint, signer.Fingerprint())
	}

	if !strings.HasPrefix(armored, "-----BEGIN PGP SIGNATURE-----") {
		t.Fatalf("signature is not ASCII-armored: %q", armored[:min(len(armored), 40)])
	}

	publicArmor, err := signer.PublicKeyArmor()
	if err != nil {
		t.Fatalf("PublicKeyArmor: %v", err)
	}

	keyRing, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicArmor))
	if err != nil {
		t.Fatalf("read derived public key: %v", err)
	}

	if _, err = openpgp.CheckArmoredDetachedSignature(keyRing, strings.NewReader(string(sums)), strings.NewReader(armored)); err != nil {
		t.Fatalf("armored signature does not verify: %v", err)
	}

	tampered := []byte("ffff99  agent.tar.gz\n")
	if _, err = openpgp.CheckArmoredDetachedSignature(keyRing, strings.NewReader(string(tampered)), strings.NewReader(armored)); err == nil {
		t.Fatal("armored signature verified for tampered SHA256SUMS content")
	}
}
