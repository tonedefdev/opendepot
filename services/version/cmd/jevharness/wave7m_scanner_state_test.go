//go:build jevharness

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// wave7mRecordingServer answers every Jev request with the valid response and records each request state.
func wave7mRecordingServer(t *testing.T) (string, func() []string) {
	t.Helper()

	var (
		mu     sync.Mutex
		states []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}

		mu.Lock()
		states = append(states, payload.State)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wave7mJevOKResponse))
	}))
	t.Cleanup(server.Close)

	return server.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), states...)
	}
}

func TestWave7MRunSendsFixedScannerTextToEverySample(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	samples := t.TempDir()
	scanText := filepath.Join(t.TempDir(), "scan.txt")
	wave7mWrite(t, samples, map[string][]byte{
		"ben-sum/SKILL.md":   []byte(wave7mSkillEntry),
		"mal-exfil/SKILL.md": []byte(wave7mSkillEntry),
	})
	wave7mWrite(t, filepath.Dir(scanText), map[string][]byte{"scan.txt": []byte("MARK-FIXED-WORDING")})

	endpoint, states := wave7mRecordingServer(t)
	args := []string{
		"-samples", samples,
		"-work", t.TempDir(),
		"-endpoint", endpoint,
		"-scanner-text", scanText,
	}

	if code := run(args); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	got := states()
	if len(got) < 2 {
		t.Fatalf("recorded %d Jev states, want at least one per sample", len(got))
	}

	for i, state := range got {
		if !strings.Contains(state, "MARK-FIXED-WORDING") {
			t.Fatalf("state %d does not carry the fixed scanner text", i)
		}
	}
}

func TestWave7MRunScannerReportsStayWithTheirSample(t *testing.T) {
	t.Setenv("OPENDEPOT_JEV_API_KEY", "token")

	samples := t.TempDir()
	reports := t.TempDir()
	wave7mWrite(t, samples, map[string][]byte{
		"mal-exfil/SKILL.md": []byte(wave7mSkillEntry),
		"ben-sum/SKILL.md":   []byte(wave7mSkillEntry),
	})
	wave7mWrite(t, reports, map[string][]byte{
		"exfil.json": []byte(`{"execution_successful": true, "risk_assessment": {"recommendation": "block", "max_issue_severity": "HIGH"}, "issues": [{"id": "MARK-EXFIL", "category": "Exfil", "severity": "HIGH", "location": {"file": "SKILL.md", "start_line": 1}}]}`),
		"sum.json":   []byte(`{"execution_successful": true, "risk_assessment": {"recommendation": "safe", "max_issue_severity": "LOW"}, "issues": [{"id": "MARK-BEN", "category": "Note", "severity": "LOW", "location": {"file": "SKILL.md", "start_line": 2}}]}`),
	})

	endpoint, states := wave7mRecordingServer(t)
	args := []string{
		"-samples", samples,
		"-work", t.TempDir(),
		"-endpoint", endpoint,
		"-scanner-reports", reports,
	}

	if code := run(args); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var exfil, ben int
	for i, state := range states() {
		hasExfil := strings.Contains(state, "MARK-EXFIL")
		hasBen := strings.Contains(state, "MARK-BEN")
		if hasExfil == hasBen {
			t.Fatalf("state %d carries %t exfil and %t ben findings, want exactly one sample's findings", i, hasExfil, hasBen)
		}

		if hasExfil {
			exfil++
		} else {
			ben++
		}
	}

	if exfil == 0 || ben == 0 {
		t.Fatalf("states per sample: exfil = %d, ben = %d; want both samples sent", exfil, ben)
	}
}
