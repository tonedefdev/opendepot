package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestBannerSilentWhenNotTerminal(t *testing.T) {
	var buf bytes.Buffer
	printBanner(&buf)

	if buf.Len() != 0 {
		t.Fatalf("banner written to non-terminal: %q", buf.String())
	}
}

func TestRootHelpHasNoBannerWhenNotTerminal(t *testing.T) {
	var out bytes.Buffer
	root := newRootCommand(strings.NewReader(""), &out, &out)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}

	if strings.Contains(out.String(), "\x1b[") || strings.Contains(out.String(), "▀") || strings.Contains(out.String(), "█") {
		t.Fatalf("help output contains banner or ANSI codes: %q", out.String())
	}
}

func TestBannerMarkIsRectangular(t *testing.T) {
	width := len([]rune(bannerMark[0]))
	for i, row := range bannerMark {
		if n := len([]rune(row)); n != width {
			t.Fatalf("row %d has width %d, want %d", i, n, width)
		}
	}
}

func TestBannerGradientRunsBlueToMint(t *testing.T) {
	cols, rows := 36, 20

	if got := bannerGradient(0, rows-1, cols, rows); got != bannerBlue {
		t.Fatalf("bottom-left = %v, want blue %v", got, bannerBlue)
	}

	if got := bannerGradient(cols-1, 0, cols, rows); got != bannerMint {
		t.Fatalf("top-right = %v, want mint %v", got, bannerMint)
	}
}

func TestRGBCodeFallsBackTo256Colors(t *testing.T) {
	t.Setenv("COLORTERM", "")

	if got := rgbCode(bannerBlue); got != "\x1b[38;5;26m" {
		t.Fatalf("blue = %q, want 256-color index 26", got)
	}

	if got := rgbCode(bannerMint); got != "\x1b[38;5;43m" {
		t.Fatalf("mint = %q, want 256-color index 43", got)
	}
}

func TestRGBCodeUsesTrueColorWhenAdvertised(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")

	if got := rgbCode(bannerMint); got != "\x1b[38;2;3;222;184m" {
		t.Fatalf("mint = %q, want 24-bit escape", got)
	}
}
