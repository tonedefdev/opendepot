package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// scannerPathLimit is the byte limit for a file path in the scanner block.
const scannerPathLimit = 256

// ScannerIssue is the part of one static scanner finding that is sent to Jev. Code snippets and explanations are
// left out, because they come from the skill under test.
type ScannerIssue struct {
	ID       string
	Category string
	Severity string
	File     string
	Line     int
}

// ScannerReport is the static scanner result for one skill.
type ScannerReport struct {
	Recommendation string
	MaxSeverity    string
	Issues         []ScannerIssue
}

// skillSpectorReport mirrors the fields of a SkillSpector JSON report that ScannerReport uses.
type skillSpectorReport struct {
	ExecutionSuccessful bool `json:"execution_successful"`
	RiskAssessment      struct {
		Recommendation   string `json:"recommendation"`
		MaxIssueSeverity string `json:"max_issue_severity"`
	} `json:"risk_assessment"`
	Issues []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		Location struct {
			File      string `json:"file"`
			StartLine int    `json:"start_line"`
		} `json:"location"`
	} `json:"issues"`
}

// ParseSkillSpectorReport reads a SkillSpector JSON report. It fails when the scan did not complete, so a report
// without full coverage is never read as clean.
func ParseSkillSpectorReport(data []byte) (ScannerReport, error) {
	var raw skillSpectorReport
	if err := json.Unmarshal(data, &raw); err != nil {
		return ScannerReport{}, fmt.Errorf("parse scanner report: %w", err)
	}

	if !raw.ExecutionSuccessful {
		return ScannerReport{}, errors.New("scanner report is not from a completed scan")
	}

	report := ScannerReport{
		Recommendation: raw.RiskAssessment.Recommendation,
		MaxSeverity:    raw.RiskAssessment.MaxIssueSeverity,
	}

	for _, issue := range raw.Issues {
		report.Issues = append(report.Issues, ScannerIssue{
			ID:       issue.ID,
			Category: issue.Category,
			Severity: issue.Severity,
			File:     issue.Location.File,
			Line:     issue.Location.StartLine,
		})
	}

	return report, nil
}

// scannerLineBreaks keeps a file path on one line in the scanner block.
var scannerLineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

// ScannerBlock renders a report as the findings block that opens a Jev state. It names the scanner and says that
// its findings can be wrong in either direction. At most limit bytes are returned; findings that do not fit are
// counted in a closing line.
func ScannerBlock(r ScannerReport, limit int) string {
	var b strings.Builder
	b.WriteString("Static scanner findings (NVIDIA SkillSpector, static analysis only; it can miss behavior or flag benign code, so judge the content yourself):\n")
	fmt.Fprintf(&b, "Recommendation: %s. Highest severity: %s.\n", scannerField(r.Recommendation, 32), scannerField(r.MaxSeverity, 32))
	if len(r.Issues) == 0 {
		b.WriteString("No findings.\n")
		return b.String()
	}

	for i, issue := range r.Issues {
		line := fmt.Sprintf("- %s %s %s at %s:%d\n", scannerField(issue.ID, 32), scannerField(issue.Severity, 32),
			scannerField(issue.Category, 128), scannerField(issue.File, scannerPathLimit), issue.Line)

		// Room for the note is kept back while later findings remain, so the note always fits.
		reserve := 0
		if rest := len(r.Issues) - i - 1; rest > 0 {
			reserve = len(scannerOmitted(rest))
		}

		if b.Len()+len(line)+reserve > limit {
			if note := scannerOmitted(len(r.Issues) - i); b.Len()+len(note) <= limit {
				b.WriteString(note)
			}

			break
		}

		b.WriteString(line)
	}

	return b.String()
}

// scannerField keeps a scanner-supplied value on one line and limits its length.
func scannerField(s string, limit int) string {
	return truncateLimit(scannerLineBreaks.Replace(s), limit)
}

// truncateLimit cuts s to at most limit bytes at a rune boundary.
func truncateLimit(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut]
}

// scannerOmitted is the closing line for findings left out of a scanner block.
func scannerOmitted(count int) string {
	return fmt.Sprintf("- %d more finding(s) omitted to fit the limit\n", count)
}
