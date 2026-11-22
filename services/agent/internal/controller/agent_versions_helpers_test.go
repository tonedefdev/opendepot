/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func TestAgentConfigName(t *testing.T) {
	owner := &metav1.ObjectMeta{Name: "owner-name"}

	if got := agentConfigName(owner, opendepotv1alpha1.AgentSourceConfig{}); got != "owner-name" {
		t.Fatalf("expected owner name, got %q", got)
	}

	override := "custom-name"
	got := agentConfigName(owner, opendepotv1alpha1.AgentSourceConfig{Name: &override})
	if got != "custom-name" {
		t.Fatalf("expected configured name, got %q", got)
	}
}

func TestNotBefore(t *testing.T) {
	now := time.Now()
	earlier := metav1.NewTime(now.Add(-time.Hour))
	later := metav1.NewTime(now)

	if !notBefore(&later, nil) {
		t.Fatal("expected a missing bound to be treated as the oldest")
	}

	if notBefore(nil, &earlier) {
		t.Fatal("expected a missing value to be treated as the oldest")
	}

	if !notBefore(&later, &earlier) {
		t.Fatal("expected later time to be not before earlier bound")
	}

	if notBefore(&earlier, &later) {
		t.Fatal("expected earlier time to be before later bound")
	}

	same := metav1.NewTime(now)
	if !notBefore(&same, &later) {
		t.Fatal("expected equal times to be not before each other")
	}
}

func TestGenerateAgentFileName(t *testing.T) {
	fileName, err := generateAgentFileName()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fileName == nil || !strings.HasSuffix(*fileName, ".tar.gz") {
		t.Fatalf("expected a .tar.gz file name, got %v", fileName)
	}

	id := strings.TrimSuffix(*fileName, ".tar.gz")
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("expected a UUID file name, got %q: %v", id, err)
	}

	if parsed.Version() != 7 {
		t.Fatalf("expected a UUIDv7 file name, got version %d", parsed.Version())
	}

	other, err := generateAgentFileName()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *other == *fileName {
		t.Fatal("expected each generated file name to be unique")
	}
}
