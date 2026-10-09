package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHost(t *testing.T) {
	valid := []string{"registry.example.com", "registry.example.com:8443", "localhost:8080"}
	for _, host := range valid {
		if err := validateHost(host); err != nil {
			t.Errorf("validateHost(%q) = %v, want nil", host, err)
		}
	}

	invalid := []string{"", "https://registry.example.com", "registry.example.com/path", "user@registry.example.com", ":8443"}
	for _, host := range invalid {
		if err := validateHost(host); err == nil {
			t.Errorf("validateHost(%q) = nil, want error", host)
		}
	}
}

func TestLoginCommandRequiresHostArg(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommand(strings.NewReader(""), &out, &out)
	cmd.SetArgs([]string{"login"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected login with no host argument to fail")
	}
}

func TestLoadTokenIgnoresTerraformCredentials(t *testing.T) {
	isolateHome(t)
	t.Setenv("OPENDEPOT_TOKEN", "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tfDir := filepath.Join(home, ".terraform.d")
	if err := os.MkdirAll(tfDir, 0o700); err != nil {
		t.Fatal(err)
	}

	tfrc := `{"credentials": {"registry.example.com": {"token": "tofu-token"}}}`
	if err := os.WriteFile(filepath.Join(tfDir, "credentials.tfrc.json"), []byte(tfrc), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadToken("registry.example.com"); got != "" {
		t.Fatalf("loadToken() = %q, want tofu credentials file ignored", got)
	}

	if err := saveToken("registry.example.com", "opendepot-token"); err != nil {
		t.Fatal(err)
	}

	if got := loadToken("registry.example.com"); got != "opendepot-token" {
		t.Fatalf("loadToken() = %q, want opendepot-token from OpenDepot credentials file", got)
	}
}
