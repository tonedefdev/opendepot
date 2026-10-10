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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
)

// TrivyDBSeedJobName is the one-off Job that populates the Trivy vulnerability DB cache.
const TrivyDBSeedJobName = "trivy-db-seed"

const (
	trivyDBSeedAttempts = 3
	trivyDBSeedTimeout  = 20 * time.Minute
	trivyDBSeedPoll     = 10 * time.Second
)

// SeedTrivyDB runs the trivy-db-updater CronJob once and waits for it to complete so
// offline Trivy scans in the given namespace have a populated vulnerability DB.
// A failed attempt (for example a transient download error from the vulnerability DB
// mirror) is retried with a fresh Job, and the failure includes the Job's logs.
func SeedTrivyDB(namespace string) error {
	var lastErr error
	for attempt := 1; attempt <= trivyDBSeedAttempts; attempt++ {
		lastErr = runTrivyDBSeedJob(namespace)
		if lastErr == nil {
			return nil
		}

		fmt.Fprintf(GinkgoWriter, "Trivy DB seed attempt %d of %d failed: %v\n", attempt, trivyDBSeedAttempts, lastErr)
	}

	return fmt.Errorf("Trivy DB seed failed after %d attempts: %w", trivyDBSeedAttempts, lastErr)
}

func runTrivyDBSeedJob(namespace string) error {
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

	deadline := time.Now().Add(trivyDBSeedTimeout)
	for {
		cmd = exec.Command("kubectl", "get", "job", TrivyDBSeedJobName,
			"--namespace", namespace,
			"-o", "jsonpath={range .status.conditions[*]}{.type}={.status} {end}",
		)
		conditions, err := Run(cmd)
		if err != nil {
			return fmt.Errorf("get Trivy DB seed job status: %w", err)
		}

		if strings.Contains(conditions, "Complete=True") {
			return nil
		}

		if strings.Contains(conditions, "Failed=True") {
			return fmt.Errorf("Trivy DB seed job failed: %s", trivyDBSeedJobLogs(namespace))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("Trivy DB seed job did not complete within %s: %s", trivyDBSeedTimeout, trivyDBSeedJobLogs(namespace))
		}

		time.Sleep(trivyDBSeedPoll)
	}
}

func trivyDBSeedJobLogs(namespace string) string {
	cmd := exec.Command("kubectl", "logs", "job/"+TrivyDBSeedJobName,
		"--namespace", namespace,
		"--all-containers",
		"--tail=100",
	)
	output, err := Run(cmd)
	if err != nil {
		return fmt.Sprintf("logs unavailable: %v", err)
	}

	return output
}
