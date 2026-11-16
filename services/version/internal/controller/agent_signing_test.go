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

import "testing"

func TestSignAgentArchiveReusesPriorSignature(t *testing.T) {
	setJevSigningKey(t)

	first, err := signAgentArchive([]byte("archive"), "agent.tar.gz", nil)
	if err != nil {
		t.Fatalf("initial sign: %v", err)
	}

	again, err := signAgentArchive([]byte("archive"), "agent.tar.gz", first)
	if err != nil {
		t.Fatalf("re-sync sign: %v", err)
	}

	if again.signature != first.signature || again.sums != first.sums {
		t.Fatal("re-sync of an unchanged archive produced a new signature")
	}

	changed, err := signAgentArchive([]byte("changed"), "agent.tar.gz", first)
	if err != nil {
		t.Fatalf("changed archive sign: %v", err)
	}

	if changed.sums == first.sums {
		t.Fatal("changed archive reused the prior sums")
	}

	rotated := &signedAgentArchive{sums: first.sums, signature: first.signature, fingerprint: "0000000000000000000000000000000000000000"}
	resigned, err := signAgentArchive([]byte("archive"), "agent.tar.gz", rotated)
	if err != nil {
		t.Fatalf("rotated key sign: %v", err)
	}

	if resigned.fingerprint != first.fingerprint {
		t.Fatalf("rotated key prior was reused; fingerprint = %s, want %s", resigned.fingerprint, first.fingerprint)
	}
}
