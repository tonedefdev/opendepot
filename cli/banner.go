package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
)

// bannerMark is the OpenDepot icon from docs/img/opendepot_icon.svg as half-block glyphs. Colors are applied at render time.
var bannerMark = []string{
	"              ▄▄████▄▄              ",
	"           ▄▄███████████▄           ",
	"            ▀▀█████████████▄▄       ",
	"    ▄▄▄▄▄▄▄▄    ▀█████████████▄▄    ",
	"  ▄██████████▄▄    ▀▀█████████████▄ ",
	" ███████████▀▀        ▀▀████████████",
	"████████▀▀                ▀▀████████",
	"█████▀▀                      ███████",
	"█▀▀                          ███████",
	"    ▄███                     █████▀▀",
	"▄▄██████                     ██▀    ",
	"████████                         ▄▄█",
	"████████                     ▄▄█████",
	"████████▄▄                ▄▄████████",
	"████████████▄         ▄▄███████████▀",
	" ▀█████████████▄▄    ▀▀███████████  ",
	"    ▀██████████████▄    ▀▀▀██▀▀▀    ",
	"       ▀▀█████████████▄▄            ",
	"           ▀████████████▀           ",
	"              ▀▀████▀▀              ",
}

var (
	bannerBlue = [3]int{3, 80, 199}
	bannerMint = [3]int{3, 222, 184}
)

const bannerTagline = "Install and verify OpenDepot skills and agents"

// printBanner writes the icon and wordmark to w. It prints nothing unless w is an interactive terminal.
func printBanner(w io.Writer) {
	st := newStyler(w)
	if !st.enabled {
		return
	}

	mint := ansiBold + rgbCode(bannerMint)

	rows := len(bannerMark)
	cols := len([]rune(bannerMark[0]))
	text := map[int]string{
		8:  st.paint(ansiBold, "Open") + st.paint(mint, "Depot"),
		10: st.paint(ansiDim, bannerTagline),
	}

	for y, row := range bannerMark {
		src := []rune(strings.TrimRight(row, " "))
		var line strings.Builder
		line.WriteString("  ")
		for x, r := range src {
			line.WriteString(rgbCode(bannerGradient(x, y, cols, rows)))
			line.WriteRune(r)
		}

		if len(src) > 0 {
			line.WriteString(ansiReset)
		}

		if t, ok := text[y]; ok {
			line.WriteString(strings.Repeat(" ", cols-len(src)+3))
			line.WriteString(t)
		}

		fmt.Fprintln(w, line.String())
	}

	fmt.Fprintln(w)
}

// bannerGradient blends from blue at the bottom-left to mint at the top-right, matching the icon.
func bannerGradient(x, y, cols, rows int) [3]int {
	t := (float64(x)/float64(cols-1) + 1 - float64(y)/float64(rows-1)) / 2
	var c [3]int
	for i := range c {
		c[i] = int(math.Round(float64(bannerBlue[i]) + t*float64(bannerMint[i]-bannerBlue[i])))
	}

	return c
}

// rgbCode returns a foreground color escape. Terminals that advertise 24-bit color get the exact value; others get the nearest xterm 256-color cube entry, which Terminal.app supports.
func rgbCode(c [3]int) string {
	if os.Getenv("COLORTERM") == "truecolor" || os.Getenv("COLORTERM") == "24bit" {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c[0], c[1], c[2])
	}

	return fmt.Sprintf("\x1b[38;5;%dm", 16+36*cubeLevel(c[0])+6*cubeLevel(c[1])+cubeLevel(c[2]))
}

func cubeLevel(v int) int {
	switch {
	case v < 48:
		return 0
	case v < 115:
		return 1
	default:
		return (v - 35) / 40
	}
}
