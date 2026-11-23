package jev

import (
	"strings"
	"testing"
)

func TestParseSkillSpectorReport(t *testing.T) {
	data := []byte(`{
		"execution_successful": true,
		"risk_assessment": {"score": 21, "recommendation": "CAUTION", "max_issue_severity": "MEDIUM"},
		"issues": [
			{"id": "LP3", "category": "MCP Least Privilege", "severity": "MEDIUM",
			 "finding": "ignored", "code_snippet": "ignored",
			 "location": {"file": "SKILL.md", "start_line": 1, "end_line": null}}
		]
	}`)

	report, err := ParseSkillSpectorReport(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if report.Recommendation != "CAUTION" || report.MaxSeverity != "MEDIUM" {
		t.Fatalf("assessment = %+v", report)
	}

	want := ScannerIssue{ID: "LP3", Category: "MCP Least Privilege", Severity: "MEDIUM", File: "SKILL.md", Line: 1}
	if len(report.Issues) != 1 || report.Issues[0] != want {
		t.Fatalf("issues = %+v, want [%+v]", report.Issues, want)
	}
}

func TestParseSkillSpectorReportRejectsIncompleteScan(t *testing.T) {
	_, err := ParseSkillSpectorReport([]byte(`{"execution_successful": false, "issues": []}`))
	if err == nil {
		t.Fatal("expected an error for a scan that did not complete")
	}
}

func TestParseSkillSpectorReportRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseSkillSpectorReport([]byte(`{`)); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestScannerBlockNoFindings(t *testing.T) {
	block := ScannerBlock(ScannerReport{Recommendation: "SAFE", MaxSeverity: ""}, 1024)
	if !strings.Contains(block, "Recommendation: SAFE.") || !strings.Contains(block, "No findings.") {
		t.Fatalf("block = %q", block)
	}
}

func TestScannerBlockListsFindingsWithoutSnippets(t *testing.T) {
	report := ScannerReport{
		Recommendation: "DO_NOT_INSTALL",
		MaxSeverity:    "CRITICAL",
		Issues: []ScannerIssue{
			{ID: "TT3", Category: "Taint", Severity: "CRITICAL", File: "scripts/run.py", Line: 12},
		},
	}

	block := ScannerBlock(report, 1024)
	if !strings.Contains(block, "- TT3 CRITICAL Taint at scripts/run.py:12\n") {
		t.Fatalf("block = %q", block)
	}
}

func TestScannerBlockKeepsPathOnOneLine(t *testing.T) {
	report := ScannerReport{
		Issues: []ScannerIssue{{ID: "AST1", Severity: "HIGH", Category: "Exec", File: "a\nb\r\nc.py", Line: 3}},
	}

	block := ScannerBlock(report, 1024)
	if strings.Count(block, "\n- ") != 1 {
		t.Fatalf("path forged a finding line: %q", block)
	}

	if !strings.Contains(block, "- AST1 HIGH Exec at a b c.py:3\n") {
		t.Fatalf("block = %q", block)
	}
}

func TestScannerBlockStaysWithinLimit(t *testing.T) {
	var issues []ScannerIssue
	for range 200 {
		issues = append(issues, ScannerIssue{ID: "LP3", Severity: "MEDIUM", Category: "MCP Least Privilege", File: "SKILL.md", Line: 1})
	}

	const limit = 1024
	block := ScannerBlock(ScannerReport{Recommendation: "CAUTION", MaxSeverity: "MEDIUM", Issues: issues}, limit)
	if len(block) > limit {
		t.Fatalf("block is %d bytes, over the %d byte limit", len(block), limit)
	}

	if !strings.Contains(block, "more finding(s) omitted") {
		t.Fatalf("expected an omission note, got %q", block)
	}
}

func TestTruncateLimitKeepsRuneBoundary(t *testing.T) {
	got := truncateLimit("ééé", 3)
	if got != "é" {
		t.Fatalf("truncateLimit = %q, want %q", got, "é")
	}
}

func TestScannerBlockLimitsFieldLengths(t *testing.T) {
	report := ScannerReport{
		Recommendation: strings.Repeat("R", 100),
		MaxSeverity:    strings.Repeat("S", 100),
		Issues: []ScannerIssue{{
			ID:       strings.Repeat("I", 100),
			Severity: "HIGH",
			Category: strings.Repeat("C", 300),
			File:     strings.Repeat("f", 300),
			Line:     9,
		}},
	}

	block := ScannerBlock(report, 4096)
	if !strings.Contains(block, "Recommendation: "+strings.Repeat("R", 32)+". ") || strings.Contains(block, strings.Repeat("R", 33)) {
		t.Fatalf("recommendation not limited to 32 bytes: %q", block)
	}

	if !strings.Contains(block, "- "+strings.Repeat("I", 32)+" HIGH "+strings.Repeat("C", 128)+" at ") {
		t.Fatalf("id or category not limited: %q", block)
	}

	if strings.Contains(block, strings.Repeat("C", 129)) {
		t.Fatalf("category exceeded 128 bytes: %q", block)
	}

	if !strings.Contains(block, strings.Repeat("f", 256)+":9\n") || strings.Contains(block, strings.Repeat("f", 257)) {
		t.Fatalf("path not limited to %d bytes: %q", scannerPathLimit, block)
	}
}

func TestScannerBlockShownPlusOmittedEqualsTotal(t *testing.T) {
	var issues []ScannerIssue
	for i := range 5 {
		issues = append(issues, ScannerIssue{ID: "LP3", Severity: "MEDIUM", Category: "Exec", File: "run.sh", Line: i + 1})
	}

	report := ScannerReport{Recommendation: "CAUTION", MaxSeverity: "MEDIUM", Issues: issues}

	full := ScannerBlock(report, 4096)
	if strings.Count(full, "\n- ") != 5 || strings.Contains(full, "omitted") {
		t.Fatalf("all findings should be listed without a note: %q", full)
	}

	// The header and two findings, plus room for the note that counts the three left out.
	const note = "- 3 more finding(s) omitted to fit the limit\n"
	twoFit := len(ScannerBlock(ScannerReport{Recommendation: "CAUTION", MaxSeverity: "MEDIUM", Issues: issues[:2]}, 4096)) + len(note)
	block := ScannerBlock(report, twoFit)
	if len(block) > twoFit {
		t.Fatalf("block is %d bytes, over the %d byte limit", len(block), twoFit)
	}

	shown := strings.Count(block, "\n- ") - 1
	if shown != 2 || !strings.Contains(block, "- 3 more finding(s) omitted to fit the limit\n") {
		t.Fatalf("shown = %d, want 2 with 3 omitted: %q", shown, block)
	}
}
