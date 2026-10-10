/*
Copyright 2026 tonedefdev.

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

package testutils

import (
	"fmt"
	"os/exec"
)

// TrivyDBSeedJobName is the one-off Job that populates the Trivy vulnerability DB cache.
const TrivyDBSeedJobName = "trivy-db-seed"

// SeedTrivyDB runs the trivy-db-updater CronJob once and waits for it to complete so
// offline Trivy scans in the given namespace have a populated vulnerability DB.
func SeedTrivyDB(namespace string) error {
	cmd := exec.Command("kubectl", "delete", "job", TrivyDBSeedJobName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = Run(cmd)

	cmd = exec.Command("kubectl", "create", "job", TrivyDBSeedJobName,
		"--from=cronjob/trivy-db-updater",
		"--namespace", namespace,
	)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("create Trivy DB seed job: %w", err)
	}

	cmd = exec.Command("kubectl", "wait", "--for=condition=complete",
		"job/"+TrivyDBSeedJobName,
		"--namespace", namespace,
		"--timeout=20m",
	)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("Trivy DB seed job did not complete: %w", err)
	}

	return nil
}
