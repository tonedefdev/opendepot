package main

import "testing"

func TestCovGVersionSyncState(t *testing.T) {
	tests := []struct {
		name   string
		synced bool
		status string
		want   string
	}{
		{name: "synced without failure", synced: true, status: "Ready", want: "synced"},
		{name: "unsynced without failure", synced: false, status: "Pending", want: "unsynced"},
		{name: "failed status overrides synced", synced: true, status: "Sync Failed", want: "failed"},
		{name: "error status overrides unsynced", synced: false, status: "ERROR: download", want: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionSyncState(tt.synced, tt.status); got != tt.want {
				t.Fatalf("versionSyncState(%v, %q) = %q, want %q", tt.synced, tt.status, got, tt.want)
			}
		})
	}
}

func TestCovGNormalizedSeverity(t *testing.T) {
	tests := map[string]string{
		"CRITICAL":   "critical",
		"High":       "high",
		"medium":     "medium",
		"LOW":        "low",
		"negligible": "unknown",
		"":           "unknown",
	}

	for input, want := range tests {
		if got := normalizedSeverity(input); got != want {
			t.Fatalf("normalizedSeverity(%q) = %q, want %q", input, got, want)
		}
	}
}
