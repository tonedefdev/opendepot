//go:build jevharness

// Command jevharness runs staged Skill samples directly against Jev, using the controller's chunking and
// aggregation, so prompt changes can be tested without building images or waiting for reconciles.
//
// Usage: OPENDEPOT_JEV_API_KEY=... go run -tags jevharness ./cmd/jevharness [-samples DIR] [-work DIR] [-only NAME]
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/services/version/internal/controller"
)

var (
	paragraphEnd = regexp.MustCompile(`</w:p>`)
	xmlTag       = regexp.MustCompile(`<[^>]+>`)
	printableRun = regexp.MustCompile(`[\x20-\x7e\t\r\n]{4,}`)
)

type summary struct {
	class, name, level, err string
	score, safe             float64
	chunks                  int
	tokens                  int64
	gates                   [][]string
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("jevharness", flag.ExitOnError)
	samples := flags.String("samples", "/tmp/jev-harness/samples", "directory of staged samples named mal-* or ben-*")
	work := flags.String("work", "/tmp/jev-harness/work", "scratch directory for decoded copies")
	only := flags.String("only", "", "comma-separated sample directory names to run")
	endpoint := flags.String("endpoint", "", "optional Jev endpoint override")
	policyPaths := flags.String("policy", "", "optional comma-separated JSON files each holding a JevPolicy, such as a depot's skillConfigs[].jevPolicy; all are gated against one Jev response per sample; empty applies no thresholds")
	scannerReports := flags.String("scanner-reports", "", "optional directory of SkillSpector JSON reports named <sample name>.json; each report's findings are sent to Jev in every part. Empty sends no findings")
	scannerText := flags.String("scanner-text", "", "optional file whose contents are sent verbatim as the scanner block for every sample; cannot be combined with -scanner-reports. Used for wording controls")
	flags.Parse(args)

	if *scannerText != "" && *scannerReports != "" {
		fmt.Fprintln(os.Stderr, "-scanner-text and -scanner-reports are mutually exclusive")
		return 1
	}

	var fixedScan string
	if *scannerText != "" {
		data, err := os.ReadFile(*scannerText)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fixedScan = string(data)
	}

	policies, labels, err := loadPolicies(*policyPaths)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	token := os.Getenv("OPENDEPOT_JEV_API_KEY")
	if token == "" {
		fmt.Fprintln(os.Stderr, "OPENDEPOT_JEV_API_KEY is not set")
		return 1
	}

	entries, err := os.ReadDir(*samples)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	filter := map[string]bool{}
	for _, s := range strings.Split(*only, ",") {
		if s != "" {
			filter[s] = true
		}
	}

	var rows []summary
	for _, e := range entries {
		if !e.IsDir() || (len(filter) > 0 && !filter[e.Name()]) {
			continue
		}

		class, name := classify(e.Name())
		if class == "" {
			continue
		}

		dir := filepath.Join(*work, e.Name(), name)
		if err := stage(filepath.Join(*samples, e.Name()), dir); err != nil {
			rows = append(rows, summary{class: class, name: name, err: err.Error()})
			continue
		}

		scan, err := scannerBlock(*scannerReports, name)
		if fixedScan != "" {
			scan = fixedScan
		}
		if err != nil {
			rows = append(rows, summary{class: class, name: name, err: err.Error()})
			continue
		}

		report, err := controller.RunJevHarnessPolicies(context.Background(), dir, name, "Skill", token, *endpoint, scan, policies)
		if err != nil {
			rows = append(rows, summary{class: class, name: name, err: err.Error()})
			continue
		}

		printReport(class, name, report)
		rows = append(rows, summary{
			class:  class,
			name:   name,
			level:  report.Level,
			score:  report.RiskScore,
			safe:   report.SafeProbability,
			chunks: report.ChunkCount,
			tokens: report.InputTokens,
			gates:  report.Gates,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].class < rows[j].class })
	header := fmt.Sprintf("%-10s %-22s %-10s %6s %6s %6s %8s", "class", "name", "level", "score", "safe", "chunks", "tokens")
	for _, l := range labels {
		header += fmt.Sprintf(" %-16s", "verdict:"+l)
	}
	fmt.Printf("\n%s %s\n", header, "error")
	for _, r := range rows {
		line := fmt.Sprintf("%-10s %-22s %-10s %6.2f %6.2f %6d %8d", r.class, r.name, r.level, r.score, r.safe, r.chunks, r.tokens)
		for i := range labels {
			verdict := "pass"
			if i < len(r.gates) && len(r.gates[i]) > 0 {
				verdict = "BLOCK"
			}
			if r.err != "" {
				verdict = "error"
			}
			line += fmt.Sprintf(" %-16s", verdict)
		}
		fmt.Printf("%s %s\n", line, r.err)
	}

	return 0
}

// scannerBlock returns the SkillSpector findings block for one sample, or empty when no report directory is set.
func scannerBlock(dir, name string) (string, error) {
	if dir == "" {
		return "", nil
	}

	data, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return "", fmt.Errorf("scanner report for %s: %w", name, err)
	}

	return controller.ScannerBlockFromReport(data)
}

// loadPolicies reads each comma-separated policy file. An empty list returns one nil policy, which applies no
// thresholds, labeled "none".
func loadPolicies(list string) ([]*opendepotv1alpha1.JevPolicy, []string, error) {
	if list == "" {
		return []*opendepotv1alpha1.JevPolicy{nil}, []string{"none"}, nil
	}

	var policies []*opendepotv1alpha1.JevPolicy
	var labels []string
	for _, path := range strings.Split(list, ",") {
		policy, err := loadPolicy(path)
		if err != nil {
			return nil, nil, err
		}

		policies = append(policies, policy)
		labels = append(labels, filepath.Base(path))
	}

	return policies, labels, nil
}

// loadPolicy reads a JevPolicy from a JSON file.
func loadPolicy(path string) (*opendepotv1alpha1.JevPolicy, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var policy opendepotv1alpha1.JevPolicy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}

	return &policy, nil
}

// classify maps a staged directory name to its class and skill name. Directories are named mal-<name> or ben-<name>.
func classify(dir string) (class, name string) {
	switch {
	case strings.HasPrefix(dir, "mal-"):
		return "malicious", strings.TrimPrefix(dir, "mal-")
	case strings.HasPrefix(dir, "ben-"):
		return "benign", strings.TrimPrefix(dir, "ben-")
	}

	return "", ""
}

// stage copies a sample into dst and decodes any binary-gated file into plain text, so Jev sees the content the
// production binary gate would have blocked. The production gate itself is unchanged.
func stage(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if isText(data) {
			return os.WriteFile(target, data, 0o644)
		}

		decoded, err := decodeBinary(path, data)
		if err != nil {
			return fmt.Errorf("decode %s: %w", rel, err)
		}

		fmt.Printf("  decoded binary %s (%d -> %d bytes)\n", rel, len(data), len(decoded))
		return os.WriteFile(target, []byte(decoded), 0o644)
	})
}

func isText(b []byte) bool {
	return !bytes.Contains(b, []byte{0}) && utf8.Valid(b)
}

// decodeBinary returns the readable text of a binary file. A zip (such as a .docx) yields the text of
// word/document.xml plus every text member, so payloads hidden inside the archive are visible. Anything else
// yields its printable runs.
func decodeBinary(path string, data []byte) (string, error) {
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err == nil {
			var out strings.Builder
			for _, f := range zr.File {
				if f.FileInfo().IsDir() {
					continue
				}

				rc, err := f.Open()
				if err != nil {
					return "", err
				}

				member, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					return "", err
				}

				if f.Name == "word/document.xml" {
					text := paragraphEnd.ReplaceAllString(string(member), "\n")
					text = xmlTag.ReplaceAllString(text, "")
					fmt.Fprintf(&out, "--- %s ---\n%s\n", f.Name, html.UnescapeString(text))
					continue
				}

				if isText(member) {
					fmt.Fprintf(&out, "--- %s ---\n%s\n", f.Name, member)
				}
			}

			if out.Len() > 0 {
				return out.String(), nil
			}
		}
	}

	runs := printableRun.FindAllString(string(data), -1)
	if len(runs) == 0 {
		return "", fmt.Errorf("no readable text in %s", filepath.Base(path))
	}

	return strings.Join(runs, "\n"), nil
}

func printReport(class, name string, r *controller.HarnessReport) {
	fmt.Printf("\n== [%s] %s  chunks=%d evidence=%t tokens=%d\n", class, name, r.ChunkCount, r.Evidence, r.InputTokens)
	fmt.Printf("  %-6s %-16s %-10s %-10s %6s %6s %6s %5s %5s %5s %5s %5s %5s %7s %5s\n",
		"part", "status", "model", "level", "score", "safe", "conf", "inj", "exf", "dest", "hid", "scope", "rexec", "tokens", "files")
	for _, p := range r.Parts {
		fmt.Printf("  %-6s %-16s %-10s %-10s %6.2f %6.2f %6.2f %5.2f %5.2f %5.2f %5.2f %5.2f %5.2f %7d %5d\n",
			p.Label, p.Status, p.Model, p.Level, p.RiskScore, p.SafeProbability, p.Confidence,
			p.InjectionProb, p.ExfiltrationProb, p.DestructiveProb, p.HiddenProb, p.ScopeProb, p.RemoteExecProb,
			p.InputTokens, p.Files)
		if p.Error != "" {
			fmt.Printf("         error: %s\n", p.Error)
		}
	}

	fmt.Printf("  AGG    level=%s score=%.2f safe=%.2f conf=%.2f\n", r.Level, r.RiskScore, r.SafeProbability, r.Confidence)
	for _, g := range r.Gate {
		fmt.Printf("  gate: %s\n", g)
	}
}
