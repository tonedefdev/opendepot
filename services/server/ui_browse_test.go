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

package main

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func Test_anyVersionUnsynced(t *testing.T) {
	tests := []struct {
		name string
		vs   []opendepotv1alpha1.Version
		want bool
	}{
		{
			name: "empty slice returns false",
			vs:   []opendepotv1alpha1.Version{},
			want: false,
		},
		{
			name: "all versions synced",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
			},
			want: false,
		},
		{
			name: "one version has synced false",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: false, SyncStatus: "Not synced"}},
			},
			want: true,
		},
		{
			name: "syncStatus contains failed",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "sync failed: storage unavailable"}},
			},
			want: true,
		},
		{
			name: "syncStatus contains error",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "error uploading artifact"}},
			},
			want: true,
		},
		{
			name: "syncStatus failed case-insensitive",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Failed: network timeout"}},
			},
			want: true,
		},
		{
			name: "syncStatus error case-insensitive",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Error: 403 Forbidden"}},
			},
			want: true,
		},
		{
			name: "synced true but error in syncStatus triggers warning",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "error: checksum mismatch"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
			},
			want: true,
		},
		{
			name: "mixed: first synced, second not",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: false, SyncStatus: "failed: upload error"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := anyVersionUnsynced(tt.vs)
			if got != tt.want {
				t.Errorf("anyVersionUnsynced() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func makeVersionSummaries() []BrowseVersionSummary {
	return []BrowseVersionSummary{
		{Version: "3.0.0", Synced: true, SyncStatus: "Synced", OS: "linux", Arch: "amd64"},
		{Version: "2.1.0", Synced: false, SyncStatus: "sync failed: timeout", OS: "linux", Arch: "arm64"},
		{Version: "2.0.0", Synced: true, SyncStatus: "Synced", OS: "darwin", Arch: "amd64"},
		{Version: "1.5.0", Synced: true, SyncStatus: "error: checksum mismatch", OS: "linux", Arch: "amd64"},
		{Version: "1.0.0", Synced: true, SyncStatus: "Synced", OS: "windows", Arch: "amd64"},
	}
}

func Test_filterAndPaginateVersions_noFilter(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "", "", "", "", 1, 20)
	if result.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", result.TotalCount)
	}
	if len(result.Items) != 5 {
		t.Errorf("len(Items) = %d, want 5", len(result.Items))
	}
}

func Test_filterAndPaginateVersions_qFilter(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "2.", "", "", "", 1, 20)
	if result.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", result.TotalCount)
	}
	for _, v := range result.Items {
		if v.Version != "2.1.0" && v.Version != "2.0.0" {
			t.Errorf("unexpected version %s in q=2. result", v.Version)
		}
	}
}

func Test_filterAndPaginateVersions_syncedTrue(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "", "true", "", "", 1, 20)
	// 3.0.0 and 2.0.0 and 1.0.0 are healthy (synced=true, no failed/error in status)
	if result.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3 (only healthy synced)", result.TotalCount)
	}
}

func Test_filterAndPaginateVersions_syncedFalse(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "", "false", "", "", 1, 20)
	// 2.1.0 (synced=false) and 1.5.0 (status contains error)
	if result.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2 (only problematic)", result.TotalCount)
	}
}

func Test_filterAndPaginateVersions_osFilter(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "", "", "linux", "", 1, 20)
	if result.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3 (linux versions)", result.TotalCount)
	}
}

func Test_filterAndPaginateVersions_archFilter(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "", "", "", "arm64", 1, 20)
	if result.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1 (arm64 version)", result.TotalCount)
	}
}

func Test_filterAndPaginateVersions_pagination(t *testing.T) {
	all := makeVersionSummaries()
	page1 := filterAndPaginateVersions(all, "", "", "", "", 1, 2)
	if len(page1.Items) != 2 {
		t.Errorf("page 1 len = %d, want 2", len(page1.Items))
	}
	if page1.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", page1.TotalCount)
	}

	page3 := filterAndPaginateVersions(all, "", "", "", "", 3, 2)
	if len(page3.Items) != 1 {
		t.Errorf("page 3 len = %d, want 1 (last item)", len(page3.Items))
	}

	// Beyond last page returns empty items, not an error.
	page99 := filterAndPaginateVersions(all, "", "", "", "", 99, 2)
	if len(page99.Items) != 0 {
		t.Errorf("page 99 len = %d, want 0", len(page99.Items))
	}
}

func Test_filterAndPaginateVersions_availableOSArch(t *testing.T) {
	all := makeVersionSummaries()
	// Filter to linux only — but availableOS/arch should still reflect ALL versions.
	result := filterAndPaginateVersions(all, "", "", "linux", "", 1, 20)
	if len(result.AvailableOS) != 3 { // linux, darwin, windows
		t.Errorf("AvailableOS len = %d, want 3", len(result.AvailableOS))
	}
}

func Test_filterAndPaginateVersions_emptyResult(t *testing.T) {
	all := makeVersionSummaries()
	result := filterAndPaginateVersions(all, "99.99.99", "", "", "", 1, 20)
	if result.TotalCount != 0 {
		t.Errorf("TotalCount = %d, want 0", result.TotalCount)
	}
	if result.Items == nil {
		t.Error("Items should not be nil")
	}
	if len(result.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(result.Items))
	}
}

func Test_compareVersionDesc(t *testing.T) {
	tests := []struct {
		a, b string
		want bool // true means a should sort before b (a is newer)
	}{
		{"3.0.0", "2.0.0", true},
		{"2.0.0", "3.0.0", false},
		{"1.10.0", "1.9.0", true},
		{"v1.2.3", "1.2.2", true},
		{"1.0.0", "1.0.0", false},
	}
	for _, tt := range tests {
		got := compareVersionDesc(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareVersionDesc(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func makeFinding(id string) opendepotv1alpha1.SecurityFinding {
	return opendepotv1alpha1.SecurityFinding{VulnerabilityID: id, Severity: "HIGH"}
}

func makeBinaryScan(findings ...opendepotv1alpha1.SecurityFinding) *opendepotv1alpha1.BinaryScan {
	return &opendepotv1alpha1.BinaryScan{Findings: findings}
}

func makeProviderVersion(semver, os, arch string, scan *opendepotv1alpha1.BinaryScan) opendepotv1alpha1.Version {
	v := opendepotv1alpha1.Version{}
	v.Spec.Version = semver
	v.Spec.OperatingSystem = os
	v.Spec.Architecture = arch
	v.Status.BinaryScan = scan

	return v
}

// Test_collectBinaryFindingsForVersion_excludesEmptyFindings asserts that a Version whose
// BinaryScan is non-nil but has zero Findings is excluded from the result map.
func Test_collectBinaryFindingsForVersion_excludesEmptyFindings(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		makeProviderVersion("6.47.0", "darwin", "arm64", makeBinaryScan()),
		makeProviderVersion("6.20.0", "darwin", "arm64", makeBinaryScan(makeFinding("CVE-2026-33186"))),
	}

	result := collectBinaryFindingsForVersion(versions, "")
	if _, ok := result["darwin/arm64"]; !ok {
		t.Fatal("expected darwin/arm64 key in result")
	}

	findings := result["darwin/arm64"]
	if len(findings) != 1 || findings[0].VulnerabilityID != "CVE-2026-33186" {
		t.Errorf("expected finding CVE-2026-33186 for darwin/arm64, got %v", findings)
	}
}

// Test_collectBinaryFindingsForVersion_nilScanExcluded asserts that a Version with a nil
// BinaryScan is excluded from the result map.
func Test_collectBinaryFindingsForVersion_nilScanExcluded(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		makeProviderVersion("6.47.0", "darwin", "arm64", nil),
		makeProviderVersion("6.20.0", "linux", "amd64", makeBinaryScan(makeFinding("CVE-2026-99999"))),
	}

	result := collectBinaryFindingsForVersion(versions, "")
	if _, ok := result["darwin/arm64"]; ok {
		t.Error("darwin/arm64 should be excluded (nil BinaryScan)")
	}

	if _, ok := result["linux/amd64"]; !ok {
		t.Error("linux/amd64 should be present")
	}
}

// Test_collectBinaryFindingsForVersion_semverFilter asserts that when a specific semver is
// requested, only versions matching that semver are included.
func Test_collectBinaryFindingsForVersion_semverFilter(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		makeProviderVersion("6.47.0", "darwin", "arm64", makeBinaryScan(makeFinding("CVE-LATEST"))),
		makeProviderVersion("6.20.0", "darwin", "arm64", makeBinaryScan(makeFinding("CVE-OLD"))),
	}

	result := collectBinaryFindingsForVersion(versions, "6.20.0")
	findings := result["darwin/arm64"]
	if len(findings) != 1 || findings[0].VulnerabilityID != "CVE-OLD" {
		t.Errorf("expected only CVE-OLD for 6.20.0, got %v", findings)
	}
}

// Test_collectBinaryFindingsForVersion_noVersionsWithFindings asserts that nil is returned
// when no versions have any binary findings.
func Test_collectBinaryFindingsForVersion_noVersionsWithFindings(t *testing.T) {
	versions := []opendepotv1alpha1.Version{
		makeProviderVersion("6.47.0", "darwin", "arm64", makeBinaryScan()),
		makeProviderVersion("6.46.0", "darwin", "arm64", nil),
	}

	result := collectBinaryFindingsForVersion(versions, "")
	if result != nil {
		t.Errorf("expected nil result when no versions have findings, got %v", result)
	}
}
