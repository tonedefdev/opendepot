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
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// TestJevRetriesExhausted covers D11: automatic re-runs stop once RetryCount exceeds the cap, and 401/403 failures never exhaust.
func TestJevRetriesExhausted(t *testing.T) {
	tests := []struct {
		name       string
		assessment *opendepotv1alpha1.JevAssessment
		maxRetries int
		want       bool
	}{
		{
			name:       "nil assessment keeps requeueing",
			assessment: nil,
			maxRetries: 3,
			want:       false,
		},
		{
			name:       "retry count equal to max keeps requeueing",
			assessment: &opendepotv1alpha1.JevAssessment{Error: "failed", RetryCount: 3},
			maxRetries: 3,
			want:       false,
		},
		{
			name:       "retry count above max stops requeueing",
			assessment: &opendepotv1alpha1.JevAssessment{Error: "failed", RetryCount: 4},
			maxRetries: 3,
			want:       true,
		},
		{
			name:       "401 or 403 with unchanged low retry count keeps requeueing",
			assessment: &opendepotv1alpha1.JevAssessment{Error: "unauthorized", Blocked: true, RetryCount: 1},
			maxRetries: 3,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jevRetriesExhausted(tt.assessment, tt.maxRetries); got != tt.want {
				t.Fatalf("jevRetriesExhausted() = %v, want %v", got, tt.want)
			}
		})
	}
}
