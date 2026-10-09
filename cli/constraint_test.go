package main

import "testing"

func TestResolveVersionOperators(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "1.5.0"}, {Version: "2.0.0"}}
	tests := []struct {
		constraint string
		want       string
	}{
		{"= 1.5.0", "1.5.0"},
		{"!= 2.0.0", "1.5.0"},
		{"> 1.0.0", "2.0.0"},
		{"< 2.0.0", "1.5.0"},
		{">= 1.5.0", "2.0.0"},
		{"<= 1.5.0", "1.5.0"},
		{"~> 1.0", "1.5.0"},
		{"~> 1.0.0", "1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			got, err := resolveVersion(versions, tt.constraint, false)
			if err != nil {
				t.Fatalf("resolveVersion(%q): %v", tt.constraint, err)
			}

			if got != tt.want {
				t.Fatalf("resolveVersion(%q) = %s, want %s", tt.constraint, got, tt.want)
			}
		})
	}
}

func TestResolveVersionSkipsYanked(t *testing.T) {
	versions := []agentVersion{{Version: "1.0.0"}, {Version: "1.1.0", Yanked: true}}

	got, err := resolveVersion(versions, ">= 1.0.0", false)
	if err != nil || got != "1.0.0" {
		t.Fatalf("resolveVersion without allowYanked = %q, %v; want 1.0.0", got, err)
	}

	got, err = resolveVersion(versions, ">= 1.0.0", true)
	if err != nil || got != "1.1.0" {
		t.Fatalf("resolveVersion with allowYanked = %q, %v; want 1.1.0", got, err)
	}
}
