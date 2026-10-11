package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

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

// jevInputUSDPerMillionTokens prices Jev input tokens. Jev does not charge for output tokens.
const jevInputUSDPerMillionTokens = 0.042

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
	riskColor := riskLevelColor(as.RiskLevel)
	if as.RiskLevel == "" {
		riskColor = ansiDim
	}

	riskCell := metricCell{label: "Risk", value: riskLabel, color: riskColor}
	if as.RiskScore != nil {
		riskCell.value += " (" + formatJevValue(as.RiskScore, "%.1f") + ")"
	}

	left := []metricCell{
		riskCell,
		probabilityCell("Safe", as.SafeProbability, true),
		probabilityCell("Injection", as.InjectionProbability, false),
		probabilityCell("Exfiltration", as.ExfiltrationProbability, false),
	}
	right := []metricCell{
		probabilityCell("Destructive actions", as.DestructiveProbability, false),
		probabilityCell("Hidden instructions", as.HiddenInstructionsProbability, false),
		probabilityCell("Scope mismatch", as.ScopeMismatchProbability, false),
		probabilityCell("Remote execution", as.RemoteExecutionProbability, false),
	}
	for i := range len(left) {
		row := fmt.Sprintf("  %s   %s", metricCellText(st, left[i]), metricCellText(st, right[i]))
		fmt.Fprintln(a.out, strings.TrimRight(row, " "))
	}

	fmt.Fprintf(a.out, "  %-12s %s\n", "Review", reviewLine(st, as.NeedsReview))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Blocked", blockedLine(st, as))
	fmt.Fprintf(a.out, "  %-12s %s\n", "Tokens", tokensLine(st, as.InputTokens))
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

// tokensLine reports the input tokens Jev reported and the estimated cost they add up to.
func tokensLine(st styler, inputTokens int64) string {
	if inputTokens == 0 {
		return st.paint(ansiDim, "n/a")
	}

	cost := float64(inputTokens) * jevInputUSDPerMillionTokens / 1_000_000
	return fmt.Sprintf("%s input · est. cost $%.6f", groupThousands(inputTokens), cost)
}

// groupThousands formats a non-negative count with commas, for example 15034 as 15,034.
func groupThousands(n int64) string {
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(c)
	}

	return b.String()
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

// metricCell is one entry in the assessment grid. The value is kept plain so its width can be measured
// before color codes are added.
type metricCell struct {
	label string
	value string
	color string
}

const (
	metricLabelWidth = 19
	metricValueWidth = 16
)

// probabilityCell renders a probability as a bar and value. higherIsBetter flips the color so that
// a high "safe" probability is green while high injection or exfiltration probabilities are red.
func probabilityCell(label string, v *float64, higherIsBetter bool) metricCell {
	if v == nil {
		return metricCell{label: label, value: "n/a", color: ansiDim}
	}

	return metricCell{label: label, value: probabilityBar(*v) + " " + fmt.Sprintf("%.2f", *v), color: probabilityColor(*v, higherIsBetter)}
}

func metricCellText(st styler, c metricCell) string {
	pad := max(metricValueWidth-utf8.RuneCountInString(c.value), 0)
	return fmt.Sprintf("%-*s %s%s", metricLabelWidth, c.label, st.paint(c.color, c.value), strings.Repeat(" ", pad))
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
