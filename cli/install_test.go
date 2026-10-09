package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/pkg/archive"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
	"time"
)

const testNamespace = "acme"

type release struct {
	version   string
	yanked    bool
	blocked   bool
	protocols []string
	files     map[string]string
}

type fakeRegistry struct {
	server     *httptest.Server
	host       string
	token      string
	signer     *openpgp.Entity
	advertised *openpgp.Entity
	packages   map[string][]fakeVersion
	files      map[string][]byte
	shasums    map[string]string
	sums       map[string][]byte
	sigs       map[string][]byte
	armored    string
}

type fakeVersion struct {
	version   string
	yanked    bool
	blocked   bool
	protocols []string
	filename  string
}

func newTestKey(t *testing.T, name string) *openpgp.Entity {
	t.Helper()
	e, err := openpgp.NewEntity(name, "", name+"@example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func fingerprintOf(e *openpgp.Entity) string {
	return fmt.Sprintf("%X", e.PrimaryKey.Fingerprint[:])
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	key := newTestKey(t, "OpenDepot Test Signer")
	f := &fakeRegistry{
		signer:     key,
		advertised: key,
		token:      "test-token",
		packages:   map[string][]fakeVersion{},
		files:      map[string][]byte{},
		shasums:    map[string]string{},
		sums:       map[string][]byte{},
		sigs:       map[string][]byte{},
	}

	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	f.host = strings.TrimPrefix(f.server.URL, "http://")
	return f
}

// publish adds releases of a package and re-signs SHA256SUMS over every archive.
func (f *fakeRegistry) publish(t *testing.T, kind entryKind, name string, releases ...release) {
	t.Helper()
	key := kind.plural() + "/" + name
	for _, r := range releases {
		filename := fmt.Sprintf("%s-%s.tar.gz", name, r.version)
		dir := t.TempDir()
		for p, content := range r.files {
			if err := os.WriteFile(filepath.Join(dir, p), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		var buf bytes.Buffer
		if err := archive.WriteDeterministicTarGz(dir, &buf); err != nil {
			t.Fatal(err)
		}

		f.files[filename] = buf.Bytes()
		f.shasums[filename] = archive.SHA256Hex(buf.Bytes())
		protocols := r.protocols
		if len(protocols) == 0 {
			protocols = []string{"1.0"}
		}

		f.packages[key] = append(f.packages[key], fakeVersion{version: r.version, yanked: r.yanked, blocked: r.blocked, protocols: protocols, filename: filename})
	}

	f.resign(t)
}

func (f *fakeRegistry) resign(t *testing.T) {
	t.Helper()
	f.signFiles(t, time.Now(), false)
}

// resignAt re-signs every release with the signature creation time set to when.
func (f *fakeRegistry) resignAt(t *testing.T, when time.Time) {
	t.Helper()
	f.signFiles(t, when, true)
}

// signFiles signs each release's SHA256SUMS line. Unchanged releases keep their signature unless force is set,
// as the registry serves stable per-version sums.
func (f *fakeRegistry) signFiles(t *testing.T, when time.Time, force bool) {
	t.Helper()
	f.armored = armoredPublicKey(t, f.advertised)
	config := &packet.Config{Time: func() time.Time { return when }}
	for filename, hash := range f.shasums {
		line := []byte(fmt.Sprintf("%s  %s\n", hash, filename))
		if prev, ok := f.sums[filename]; ok && !force && bytes.Equal(prev, line) {
			continue
		}

		var sig bytes.Buffer
		if err := openpgp.DetachSign(&sig, f.signer, bytes.NewReader(line), config); err != nil {
			t.Fatal(err)
		}

		f.sums[filename] = line
		f.sigs[filename] = sig.Bytes()
	}
}

// tamperSums changes every served SHA256SUMS so signature verification fails.
func (f *fakeRegistry) tamperSums() {
	for n := range f.sums {
		f.sums[n] = append(f.sums[n], ' ')
	}
}

// rotate signs future SHA256SUMS with a new key and advertises it.
func (f *fakeRegistry) rotate(t *testing.T, key *openpgp.Entity) {
	t.Helper()
	f.signer = key
	f.advertised = key
	f.resignAt(t, time.Now())
}

func armoredPublicKey(t *testing.T, e *openpgp.Entity) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Serialize(w); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

func writeServedFile(w http.ResponseWriter, content []byte) {
	if content == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	_, _ = w.Write(content)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fakeAssessment(v fakeVersion) any {
	if !v.blocked {
		return nil
	}

	return map[string]any{
		"evaluatedAt":  "2026-10-09T00:00:00Z",
		"riskLevel":    "High",
		"riskScore":    0.8,
		"needsReview":  false,
		"blocked":      true,
		"blockReasons": []string{"risk score 0.8 above 0.5"},
	}
}

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	const agentsPrefix = "/opendepot/agents/v1/"
	switch {
	case p == "/.well-known/terraform.json":
		writeJSON(w, map[string]any{
			"agents.v1": "/opendepot/agents/v1/",
			"login.v1": map[string]any{
				"client":      "opendepot-cli",
				"grant_types": []string{"authorization_code"},
				"authz":       "/login/authorize",
				"token":       "/login/token",
				"scopes":      []string{"openid"},
				"ports":       []int{0},
			},
		})
	case strings.HasPrefix(p, "/opendepot/modules/v1/SHA256SUMS/"):
		writeServedFile(w, f.sums[strings.TrimPrefix(p, "/opendepot/modules/v1/SHA256SUMS/")])
	case strings.HasPrefix(p, "/opendepot/modules/v1/SHA256SUMS.sig/"):
		writeServedFile(w, f.sigs[strings.TrimPrefix(p, "/opendepot/modules/v1/SHA256SUMS.sig/")])
	case strings.HasPrefix(p, "/opendepot/modules/v1/download/"):
		data, ok := f.files[strings.TrimPrefix(p, "/opendepot/modules/v1/download/")]
		if !ok {
			http.NotFound(w, r)
			return
		}

		_, _ = w.Write(data)
	case strings.HasPrefix(p, agentsPrefix):
		parts := strings.Split(strings.TrimPrefix(p, agentsPrefix), "/")
		if len(parts) < 4 || parts[0] != testNamespace {
			http.NotFound(w, r)
			return
		}

		key := parts[1] + "/" + parts[2]
		versions := f.packages[key]
		if parts[3] == "versions" && len(parts) == 4 {
			var list []map[string]any
			for _, v := range versions {
				list = append(list, map[string]any{"version": v.version, "yanked": v.yanked})
			}

			writeJSON(w, map[string]any{"versions": list})
			return
		}

		if len(parts) == 5 && parts[4] == "archive" {
			if r.Header.Get("Authorization") != "Bearer "+f.token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			for _, v := range versions {
				if v.version == parts[3] {
					http.Redirect(w, r, "/opendepot/modules/v1/download/"+v.filename, http.StatusFound)
					return
				}
			}

			http.NotFound(w, r)
			return
		}

		if len(parts) != 5 || parts[4] != "download" {
			http.NotFound(w, r)
			return
		}

		for _, v := range versions {
			if v.version != parts[3] {
				continue
			}

			kind := kindAgent
			if parts[1] == "skills" {
				kind = kindSkill
			}

			writeJSON(w, map[string]any{
				"protocols":             v.protocols,
				"kind":                  kind.singular(),
				"name":                  parts[2],
				"version":               v.version,
				"yanked":                v.yanked,
				"assessment":            fakeAssessment(v),
				"filename":              v.filename,
				"download_url":          fmt.Sprintf("/opendepot/agents/v1/%s/%s/%s/%s/archive", testNamespace, kind.plural(), parts[2], v.version),
				"shasum":                f.shasums[v.filename],
				"shasums_url":           fmt.Sprintf("/opendepot/modules/v1/SHA256SUMS/%s", v.filename),
				"shasums_signature_url": fmt.Sprintf("/opendepot/modules/v1/SHA256SUMS.sig/%s", v.filename),
				"signing_keys": map[string]any{
					"gpg_public_keys": []map[string]any{{
						"key_id":      fingerprintOf(f.advertised),
						"ascii_armor": f.armored,
					}},
				},
			})
			return
		}

		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}
