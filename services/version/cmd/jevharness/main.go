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
}

func main() {
	samples := flag.String("samples", "/tmp/jev-harness/samples", "directory of staged samples named mal-* or ben-*")
	work := flag.String("work", "/tmp/jev-harness/work", "scratch directory for decoded copies")
	only := flag.String("only", "", "comma-separated sample directory names to run")
	endpoint := flag.String("endpoint", "", "optional Jev endpoint override")
	flag.Parse()

	token := os.Getenv("OPENDEPOT_JEV_API_KEY")
	if token == "" {
		fmt.Fprintln(os.Stderr, "OPENDEPOT_JEV_API_KEY is not set")
		os.Exit(1)
	}

	entries, err := os.ReadDir(*samples)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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

		report, err := controller.RunJevHarness(context.Background(), dir, name, "Skill", token, *endpoint)
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
		})
	}

	sort.SliceStable(rows, func(i, j int) bool { return rows[i].class < rows[j].class })
	fmt.Printf("\n%-10s %-22s %-10s %6s %6s %6s %8s  %s\n", "class", "name", "level", "score", "safe", "chunks", "tokens", "error")
	for _, r := range rows {
		fmt.Printf("%-10s %-22s %-10s %6.2f %6.2f %6d %8d  %s\n", r.class, r.name, r.level, r.score, r.safe, r.chunks, r.tokens, r.err)
	}
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
	fmt.Printf("  %-6s %-16s %-10s %6s %6s %6s %5s %5s %5s %5s %5s %5s %7s %5s\n",
		"part", "status", "level", "score", "safe", "conf", "inj", "exf", "dest", "hid", "scope", "rexec", "tokens", "files")
	for _, p := range r.Parts {
		fmt.Printf("  %-6s %-16s %-10s %6.2f %6.2f %6.2f %5.2f %5.2f %5.2f %5.2f %5.2f %5.2f %7d %5d\n",
			p.Label, p.Status, p.Level, p.RiskScore, p.SafeProbability, p.Confidence,
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
