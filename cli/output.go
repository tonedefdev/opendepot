package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

// styler adds ANSI color only when output is an interactive terminal and NO_COLOR is unset.
type styler struct {
	enabled bool
}

func newStyler(out io.Writer) styler {
	f, ok := out.(*os.File)
	if !ok || os.Getenv("NO_COLOR") != "" {
		return styler{}
	}

	info, err := f.Stat()
	return styler{enabled: err == nil && info.Mode()&os.ModeCharDevice != 0}
}

func (s styler) paint(code, text string) string {
	if !s.enabled || code == "" {
		return text
	}

	return code + text + ansiReset
}

func (a *app) printAssessment(s *stagedEntry) {
	st := newStyler(a.out)
	as := s.assessment

	riskLabel := as.RiskLevel
	if riskLabel == "" {
		riskLabel = "n/a"
	}

	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, st.paint(ansiBold, fmt.Sprintf("◆ %s@%s", s.source, s.version)))
	fmt.Fprintln(a.out, st.paint(ansiDim, "  TypeSafe Jev assessment · advisory scores, not written to the lock"))
	fmt.Fprintf(a.out, "  %-12s %s  %s\n", "Risk", st.paint(riskLevelColor(as.RiskLevel), riskLabel), st.paint(ansiDim, "(score "+formatJevValue(as.RiskScore, "%.1f")+")"))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Safe", probabilityLine(st, as.SafeProbability, true))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Injection", probabilityLine(st, as.InjectionProbability, false))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Exfiltration", probabilityLine(st, as.ExfiltrationProbability, false))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Review", reviewLine(st, as.NeedsReview))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Blocked", blockedLine(st, as))
	if as.Error != "" {
		fmt.Fprintf(a.out, "  %-12s %s\n", "Error", st.paint(ansiRed, as.Error))
	}
	fmt.Fprintln(a.out, st.paint(ansiYellow, "  ⚠ Skill or agent content was sent to TypeSafe for this assessment."))
}

func reviewLine(st styler, needsReview bool) string {
	if needsReview {
		return st.paint(ansiYellow, "needed")
	}

	return st.paint(ansiGreen, "not needed")
}

func blockedLine(st styler, as *opendepotv1alpha1.JevAssessment) string {
	if !as.Blocked {
		return st.paint(ansiGreen, "no")
	}

	line := "yes"
	if len(as.BlockReasons) > 0 {
		line += " · " + strings.Join(as.BlockReasons, "; ")
	}

	return st.paint(ansiRed, line)
}

func riskLevelColor(level string) string {
	switch level {
	case "Minimal", "Low":
		return ansiGreen
	case "Moderate":
		return ansiYellow
	case "High", "Critical":
		return ansiRed
	default:
		return ""
	}
}

// probabilityLine renders a probability as a bar and value. higherIsBetter flips the color so that
// a high "safe" probability is green while high injection or exfiltration probabilities are red.
func probabilityLine(st styler, v *float64, higherIsBetter bool) string {
	if v == nil {
		return st.paint(ansiDim, "n/a")
	}

	return st.paint(probabilityColor(*v, higherIsBetter), probabilityBar(*v)) + " " + fmt.Sprintf("%.2f", *v)
}

func probabilityColor(v float64, higherIsBetter bool) string {
	risk := v
	if higherIsBetter {
		risk = 1 - v
	}

	switch {
	case risk >= 0.5:
		return ansiRed
	case risk >= 0.25:
		return ansiYellow
	default:
		return ansiGreen
	}
}

func probabilityBar(v float64) string {
	const width = 10
	filled := int(math.Round(math.Min(math.Max(v, 0), 1) * width))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatJevValue(v *float64, format string) string {
	if v == nil {
		return "n/a"
	}

	return fmt.Sprintf(format, *v)
}
