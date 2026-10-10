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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
)

// TrivyDBSeedJobName is the one-off Job that populates the Trivy vulnerability DB cache.
const TrivyDBSeedJobName = "trivy-db-seed"

// Seeding takes about 30 seconds when healthy. The budget is shared by all attempts in one
// SeedTrivyDB call and two calls run in the version suite, so it stays well inside the 20m
// CI go test -timeout alongside Kind and Helm setup and the specs.
const (
	trivyDBSeedAttempts = 3
	trivyDBSeedBudget   = 4 * time.Minute
	trivyDBSeedPoll     = 5 * time.Second
)

// SeedTrivyDB runs the trivy-db-updater CronJob once and waits for it to complete so
// offline Trivy scans in the given namespace have a populated vulnerability DB.
// Each attempt is a Job with backoffLimit 0 so a failure surfaces immediately. A failed
// attempt is retried with a fresh Job while the shared budget remains, and the failure
// includes the Job's logs.
func SeedTrivyDB(namespace string) error {
	deadline := time.Now().Add(trivyDBSeedBudget)
	var lastErr error
	for attempt := 1; attempt <= trivyDBSeedAttempts && time.Now().Before(deadline); attempt++ {
		lastErr = runTrivyDBSeedJob(namespace, deadline)
		if lastErr == nil {
			return nil
		}

		fmt.Fprintf(GinkgoWriter, "Trivy DB seed attempt %d of %d failed: %v\n", attempt, trivyDBSeedAttempts, lastErr)
	}

	return fmt.Errorf("Trivy DB seed failed within %s: %w", trivyDBSeedBudget, lastErr)
}

func runTrivyDBSeedJob(namespace string, deadline time.Time) error {
	cmd := exec.Command("kubectl", "delete", "job", TrivyDBSeedJobName,
		"--namespace", namespace,
		"--ignore-not-found",
	)
	_, _ = Run(cmd)

	if err := createTrivyDBSeedJob(namespace); err != nil {
		return err
	}

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
			return fmt.Errorf("Trivy DB seed job did not complete before the deadline: %s", trivyDBSeedJobLogs(namespace))
		}

		time.Sleep(trivyDBSeedPoll)
	}
}

// createTrivyDBSeedJob renders the Job from the CronJob template and sets backoffLimit to 0
// before creating it, so the first container failure fails the Job instead of restarting.
func createTrivyDBSeedJob(namespace string) error {
	render := exec.Command("kubectl", "create", "job", TrivyDBSeedJobName,
		"--from=cronjob/trivy-db-updater",
		"--namespace", namespace,
		"--dry-run=client",
		"-o", "json",
	)
	rendered, err := render.Output()
	if err != nil {
		return fmt.Errorf("render Trivy DB seed job: %w", err)
	}

	var job map[string]any
	if err := json.Unmarshal(rendered, &job); err != nil {
		return fmt.Errorf("parse rendered Trivy DB seed job: %w", err)
	}

	spec, ok := job["spec"].(map[string]any)
	if !ok {
		return errors.New("rendered Trivy DB seed job has no spec")
	}
	spec["backoffLimit"] = 0

	manifest, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode Trivy DB seed job: %w", err)
	}

	create := exec.Command("kubectl", "create", "-f", "-")
	create.Stdin = bytes.NewReader(manifest)
	if output, err := create.CombinedOutput(); err != nil {
		return fmt.Errorf("create Trivy DB seed job: %w: %s", err, output)
	}

	return nil
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
