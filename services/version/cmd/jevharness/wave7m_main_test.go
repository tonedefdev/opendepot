//go:build jevharness

package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonedefdev/opendepot/services/version/internal/controller"
)

const wave7mJevOKResponse = `{
  "model": "jev-1.13.0",
  "answers": {
    "safe": {"type": "noul", "noul": 0.99},
    "prompt_injection": {"type": "noul", "noul": 0.02},
    "data_exfiltration": {"type": "noul", "noul": 0.01},
    "destructive_actions": {"type": "noul", "noul": 0.03},
    "hidden_instructions": {"type": "noul", "noul": 0.04},
    "scope_mismatch": {"type": "noul", "noul": 0.05},
    "remote_execution": {"type": "noul", "noul": 0.06},
    "risk": {
      "type": "score",
      "score": 0.75,
      "legend": {"0": "Minimal", "1": "Low", "2": "Moderate", "3": "High", "4": "Critical"},
      "probabilities": {"0": 0.6, "1": 0.25, "2": 0.1, "3": 0.04, "4": 0.01},
      "confidence": 0.88
    }
  },
  "usage": {"input_tokens": 1234, "output_tokens": 56}
}`

const wave7mSkillEntry = "---\nname: agent\ndescription: Summarizes documents.\n---\nSummarize documents.\n"

// wave7mJevServer answers every Jev request with the same valid response.
func wave7mJevServer(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wave7mJevOKResponse))
	}))
	t.Cleanup(server.Close)

	return server.URL
}

// wave7mWrite writes files under root, creating parent directories.
func wave7mWrite(t *testing.T, root string, files map[string][]byte) {
	t.Helper()

	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// wave7mDocx returns a zip archive with a word/document.xml holding text and an extra text member.
func wave7mDocx(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	members := map[string]string{
		"word/document.xml": "<w:p><w:r><w:t>" + text + "</w:t></w:r></w:p>&amp;",
		"word/notes.txt":    "hidden member text",
	}
	for _, name := range []string{"word/document.xml", "word/notes.txt"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte(members[name])); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestWave7MClassify(t *testing.T) {
	cases := map[string][2]string{
		"mal-exfil": {"malicious", "exfil"},
		"ben-sum":   {"benign", "sum"},
		"other":     {"", ""},
	}

	for dir, want := range cases {
		class, name := classify(dir)
		if class != want[0] || name != want[1] {
			t.Fatalf("classify(%q) = (%q, %q), want (%q, %q)", dir, class, name, want[0], want[1])
		}
	}
}

func TestWave7MIsText(t *testing.T) {
	if !isText([]byte("plain text")) {
		t.Fatal("isText rejected plain text")
	}

	if isText([]byte{0x00, 'a'}) {
		t.Fatal("isText accepted a NUL byte")
	}

	if isText([]byte{0xff, 0xfe}) {
		t.Fatal("isText accepted invalid UTF-8")
	}
}

func TestWave7MDecodeBinaryDocx(t *testing.T) {
	out, err := decodeBinary("doc.docx", wave7mDocx(t, "Hello &lt;world&gt;"))
	if err != nil {
		t.Fatalf("decodeBinary: %v", err)
	}

	if !strings.Contains(out, "--- word/document.xml ---") || !strings.Contains(out, "Hello <world>") {
		t.Fatalf("docx text not decoded:\n%s", out)
	}

	if !strings.Contains(out, "hidden member text") {
		t.Fatalf("text member not decoded:\n%s", out)
	}
}

func TestWave7MDecodeBinaryPrintableRuns(t *testing.T) {
	data := []byte("\x00\x01 curl https://example.test \x02\x03")

	out, err := decodeBinary("blob.bin", data)
	if err != nil {
		t.Fatalf("decodeBinary: %v", err)
	}

	if !strings.Contains(out, "curl https://example.test") {
		t.Fatalf("printable run missing:\n%q", out)
	}
}

func TestWave7MDecodeBinaryNoText(t *testing.T) {
	if _, err := decodeBinary("blob.bin", []byte{0x00, 0x01, 0x02}); err == nil {
		t.Fatal("decodeBinary returned no error for data without readable text")
	}
}

func TestWave7MDecodeBinaryCorruptZipFallsBack(t *testing.T) {
	data := append([]byte("PK\x03\x04"), []byte("\x00 readable fallback run \x00")...)

	out, err := decodeBinary("broken.zip", data)
	if err != nil {
		t.Fatalf("decodeBinary: %v", err)
	}

	if !strings.Contains(out, "readable fallback run") {
		t.Fatalf("fallback text missing:\n%q", out)
	}
}

func TestWave7MStageCopiesTextAndDecodesBinary(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "staged")
	wave7mWrite(t, src, map[string][]byte{
		"SKILL.md":         []byte(wave7mSkillEntry),
		"docs/guide.docx":  wave7mDocx(t, "guide body"),
		"docs/readme.txt":  []byte("nested text"),
		"stale/keep.txt":   []byte("ignored"),
		"payload/blob.dat": {0x00, 'x', 'y', 'z', 'w', 0x00},
	})

	if err := stage(src, dst); err != nil {
		t.Fatalf("stage: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil || string(got) != wave7mSkillEntry {
		t.Fatalf("SKILL.md not copied verbatim: %q, %v", got, err)
	}

	decoded, err := os.ReadFile(filepath.Join(dst, "docs", "guide.docx"))
	if err != nil || !strings.Contains(string(decoded), "guide body") {
		t.Fatalf("docx not decoded into text: %q, %v", decoded, err)
	}

	blob, err := os.ReadFile(filepath.Join(dst, "payload", "blob.dat"))
	if err != nil || !strings.Contains(string(blob), "xyzw") {
		t.Fatalf("binary blob not decoded: %q, %v", blob, err)
	}
}

func TestWave7MStageFailsOnUnreadableBinary(t *testing.T) {
	src := t.TempDir()
	wave7mWrite(t, src, map[string][]byte{"bad.bin": {0x00, 0x01, 0x02}})

	err := stage(src, filepath.Join(t.TempDir(), "staged"))
	if err == nil || !strings.Contains(err.Error(), "decode bad.bin") {
		t.Fatalf("stage err = %v, want a decode error for bad.bin", err)
	}
}

func TestWave7MStageMissingSource(t *testing.T) {
	if err := stage(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("stage succeeded for a missing source")
	}
}

func TestWave7MLoadPoliciesDefault(t *testing.T) {
	policies, labels, err := loadPolicies("")
	if err != nil {
		t.Fatalf("loadPolicies: %v", err)
	}

	if len(policies) != 1 || policies[0] != nil || len(labels) != 1 || labels[0] != "none" {
		t.Fatalf("default policies = %v, labels = %v", policies, labels)
	}
}

func TestWave7MLoadPoliciesFiles(t *testing.T) {
	dir := t.TempDir()
	strict := filepath.Join(dir, "strict.json")
	lenient := filepath.Join(dir, "lenient.json")
	wave7mWrite(t, dir, map[string][]byte{
		"strict.json":  []byte(`{"maxRiskScore": 0.1}`),
		"lenient.json": []byte(`{"minSafeProbability": 0.5}`),
	})

	policies, labels, err := loadPolicies(strict + "," + lenient)
	if err != nil {
		t.Fatalf("loadPolicies: %v", err)
	}

	if len(policies) != 2 || policies[0].MaxRiskScore == nil || *policies[0].MaxRiskScore != 0.1 {
		t.Fatalf("policies = %+v", policies)
	}

	if policies[1].MinSafeProbability == nil || labels[0] != "strict.json" || labels[1] != "lenient.json" {
		t.Fatalf("policies = %+v, labels = %v", policies, labels)
	}
}

func TestWave7MLoadPolicyErrors(t *testing.T) {
	dir := t.TempDir()
	wave7mWrite(t, dir, map[string][]byte{"broken.json": []byte(`{"maxRiskScore": "high"`)})

	if _, _, err := loadPolicies(filepath.Join(dir, "broken.json")); err == nil {
		t.Fatal("loadPolicies accepted invalid JSON")
	}

	if _, _, err := loadPolicies(filepath.Join(dir, "absent.json")); err == nil {
		t.Fatal("loadPolicies accepted a missing file")
	}
}

func TestWave7MScannerBlock(t *testing.T) {
	block, err := scannerBlock("", "exfil")
	if err != nil || block != "" {
		t.Fatalf("scannerBlock without a directory = %q, %v; want empty", block, err)
	}

	dir := t.TempDir()
	report := `{"execution_successful": true, "risk_assessment": {"recommendation": "block", "max_issue_severity": "high"}, "issues": [{"id": "SS-9", "category": "exfil", "severity": "high", "location": {"file": "a.sh", "start_line": 3}}]}`
	wave7mWrite(t, dir, map[string][]byte{"exfil.json": []byte(report)})

	block, err = scannerBlock(dir, "exfil")
	if err != nil || !strings.Contains(block, "SS-9") {
		t.Fatalf("scannerBlock = %q, %v", block, err)
	}

	if _, err := scannerBlock(dir, "missing"); err == nil {
		t.Fatal("scannerBlock accepted a missing report")
	}
}

func TestWave7MPrintReport(t *testing.T) {
	printReport("malicious", "exfil", &controller.HarnessReport{
		ChunkCount: 1,
		Level:      "High",
		RiskScore:  0.8,
		Gate:       []string{"risk score 0.80 above 0.10"},
		Parts: []controller.HarnessPart{
			{Label: "1/1", Status: "Scored", Model: "jev-1.13.0", Error: "example"},
		},
	})
}

func TestWave7MRunRejectsConflictingScannerFlags(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	if code := run([]string{"-scanner-text", "a.txt", "-scanner-reports", "dir"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for mutually exclusive scanner flags", code)
	}
}

func TestWave7MRunFailsOnUnreadableScannerText(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	if code := run([]string{"-scanner-text", filepath.Join(t.TempDir(), "absent.txt")}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a missing scanner text file", code)
	}
}

func TestWave7MRunFailsOnBadPolicy(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	if code := run([]string{"-policy", filepath.Join(t.TempDir(), "absent.json")}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a missing policy file", code)
	}
}

func TestWave7MRunRequiresAPIKey(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "")

	if code := run([]string{}); code != 1 {
		t.Fatalf("exit code = %d, want 1 without an API key", code)
	}
}

func TestWave7MRunFailsOnMissingSamples(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	if code := run([]string{"-samples", filepath.Join(t.TempDir(), "absent")}); code != 1 {
		t.Fatalf("exit code = %d, want 1 for a missing samples directory", code)
	}
}

func TestWave7MRunScoresSamples(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	samples := t.TempDir()
	work := t.TempDir()
	reports := t.TempDir()
	wave7mWrite(t, samples, map[string][]byte{
		"mal-exfil/SKILL.md":        []byte(wave7mSkillEntry),
		"mal-exfil/docs/guide.docx": wave7mDocx(t, "guide body"),
		"ben-sum/SKILL.md":          []byte(wave7mSkillEntry),
		"other-skip/SKILL.md":       []byte(wave7mSkillEntry),
		"mal-broken/SKILL.md":       []byte(wave7mSkillEntry),
		"mal-broken/bad.bin":        {0x00, 0x01, 0x02},
	})
	wave7mWrite(t, reports, map[string][]byte{
		"exfil.json": []byte(`{"execution_successful": true, "risk_assessment": {"recommendation": "block", "max_issue_severity": "high"}, "issues": []}`),
	})

	policy := filepath.Join(t.TempDir(), "policy.json")
	wave7mWrite(t, filepath.Dir(policy), map[string][]byte{"policy.json": []byte(`{"maxRiskScore": 0.1}`)})

	args := []string{
		"-samples", samples,
		"-work", work,
		"-endpoint", wave7mJevServer(t),
		"-policy", policy,
		"-scanner-reports", reports,
	}

	if code := run(args); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestWave7MRunFiltersAndUsesFixedScannerText(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	samples := t.TempDir()
	scanText := filepath.Join(t.TempDir(), "scan.txt")
	wave7mWrite(t, samples, map[string][]byte{
		"ben-sum/SKILL.md":   []byte(wave7mSkillEntry),
		"mal-exfil/SKILL.md": []byte(wave7mSkillEntry),
	})
	wave7mWrite(t, filepath.Dir(scanText), map[string][]byte{"scan.txt": []byte("fixed scanner wording")})

	args := []string{
		"-samples", samples,
		"-work", t.TempDir(),
		"-endpoint", wave7mJevServer(t),
		"-only", "ben-sum",
		"-scanner-text", scanText,
	}

	if code := run(args); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestWave7MRunReportsSampleErrorsAndContinues(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	samples := t.TempDir()
	wave7mWrite(t, samples, map[string][]byte{
		"mal-undecodable/bad.bin":   {0x00, 0x01, 0x02},
		"ben-no-entry/notes.txt":    []byte("no entry file"),
		"ben-missing-scan/SKILL.md": []byte(wave7mSkillEntry),
	})

	args := []string{
		"-samples", samples,
		"-work", t.TempDir(),
		"-endpoint", wave7mJevServer(t),
		"-scanner-reports", t.TempDir(),
	}

	if code := run(args); code != 0 {
		t.Fatalf("exit code = %d, want 0 with per-sample errors recorded", code)
	}
}
