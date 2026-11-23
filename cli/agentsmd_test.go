package main

import (
	"strings"
	"testing"
)

const testSource = "registry.example.com/acme/lint"

func TestAgentsMDMarkersIdempotent(t *testing.T) {
	desired := map[string]string{testSource: canonicalInner("body\n")}
	content := "# Project\n\nUser notes.\n"

	first, err := planAgentsMD(content, desired, nil, false)
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}

	if !strings.HasPrefix(first, content) {
		t.Fatalf("existing content was not preserved:\n%s", first)
	}

	if !strings.Contains(first, agentsBeginPrefix+testSource+agentsMarkerSufx) {
		t.Fatalf("missing begin marker:\n%s", first)
	}

	recorded := map[string]string{}
	h, err := hashBlock(canonicalInner("body\n"))
	if err != nil {
		t.Fatal(err)
	}

	recorded[testSource] = h
	second, err := planAgentsMD(first, desired, recorded, false)
	if err != nil {
		t.Fatalf("second plan: %v", err)
	}

	if second != first {
		t.Fatalf("re-plan changed unchanged content:\n%s\n---\n%s", first, second)
	}

	updated, err := planAgentsMD(first, map[string]string{testSource: canonicalInner("new\n")}, recorded, false)
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}

	if strings.Count(updated, agentsBeginPrefix) != 1 || !strings.Contains(updated, "new\n") || strings.Contains(updated, "body\n") {
		t.Fatalf("block was not updated in place:\n%s", updated)
	}
}

func TestAgentsMDHandEditRequiresForce(t *testing.T) {
	desired := map[string]string{testSource: canonicalInner("body\n")}
	recorded := map[string]string{}
	h, err := hashBlock(canonicalInner("body\n"))
	if err != nil {
		t.Fatal(err)
	}

	recorded[testSource] = h
	first, err := planAgentsMD("", desired, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	edited := strings.Replace(first, "body\n", "hand edit\n", 1)
	if _, err := planAgentsMD(edited, desired, recorded, false); err == nil {
		t.Fatal("expected hand-edited block to fail without force")
	}

	forced, err := planAgentsMD(edited, desired, recorded, true)
	if err != nil {
		t.Fatalf("forced plan: %v", err)
	}

	if !strings.Contains(forced, "body\n") || strings.Contains(forced, "hand edit") {
		t.Fatalf("forced plan did not restore the block:\n%s", forced)
	}
}

func TestAgentsMDRemovesRecordedBlock(t *testing.T) {
	h, err := hashBlock(canonicalInner("body\n"))
	if err != nil {
		t.Fatal(err)
	}

	first, err := planAgentsMD("keep\n", map[string]string{testSource: canonicalInner("body\n")}, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	removed, err := planAgentsMD(first, nil, map[string]string{testSource: h}, false)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(removed, agentsBeginPrefix) || !strings.Contains(removed, "keep") {
		t.Fatalf("block was not removed cleanly:\n%s", removed)
	}
}

func TestAgentsMDUnbalancedMarkersFail(t *testing.T) {
	content := agentsBeginPrefix + testSource + agentsMarkerSufx + "\nbody\n"
	if _, err := planAgentsMD(content, nil, nil, false); err == nil {
		t.Fatal("expected unbalanced markers to fail")
	}
}
