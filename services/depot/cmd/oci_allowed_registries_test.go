package main

import (
	"slices"
	"testing"
)

func TestParseOCIAllowedRegistries(t *testing.T) {
	if got := parseOCIAllowedRegistries(""); len(got) != 0 {
		t.Fatalf("parseOCIAllowedRegistries(\"\") = %v, want empty", got)
	}

	want := []string{"ghcr.io", "docker.io"}
	if got := parseOCIAllowedRegistries("ghcr.io, docker.io"); !slices.Equal(got, want) {
		t.Fatalf("parseOCIAllowedRegistries(\"ghcr.io, docker.io\") = %v, want %v", got, want)
	}
}
