package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/openpgp/armor"
)

func TestSignatureHashIgnoresArmor(t *testing.T) {
	raw := []byte("binary-signature-packet")

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, "PGP SIGNATURE", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	binaryHash, err := signatureHash(raw)
	if err != nil {
		t.Fatal(err)
	}

	armoredHash, err := signatureHash(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(raw)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if binaryHash != want || armoredHash != want {
		t.Fatalf("signatureHash binary=%s armored=%s, want %s", binaryHash, armoredHash, want)
	}
}
