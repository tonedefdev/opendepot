/*
Copyright 2026 Anthony Owens.

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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	coverageVolumeName      = "coverage"
	coverageWaitTimeout     = 60 * time.Second
	coverageWaitPollPeriod  = time.Second
	coverageCollectAttempts = 5
)

var errCoveragePodGone = errors.New("coverage pod no longer exists")

type coveragePodList struct {
	Items []coveragePod `json:"items"`
}

type coveragePod struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name         string `json:"name"`
			Image        string `json:"image"`
			VolumeMounts []struct {
				Name string `json:"name"`
			} `json:"volumeMounts"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		ContainerStatuses []struct {
			Name        string `json:"name"`
			ContainerID string `json:"containerID"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// CoverageEnabled reports whether the e2e suites run against COVER=1 images (make coverage-e2e).
func CoverageEnabled() bool {
	return os.Getenv("COVER") == "1"
}

// CoverageHelmArgs returns the Helm --set flags that enable GOCOVERDIR and the coverage
// emptyDir in the chart when COVER=1. It returns nil otherwise, so default installs are unchanged.
func CoverageHelmArgs() []string {
	if !CoverageEnabled() {
		return nil
	}

	return []string{"--set", "coverage.enabled=true"}
}

// CollectCoverage copies GOCOVERDIR output from every coverage-enabled controller pod in namespace
// into coverage/e2e/<service>/. It must run before the Helm release is uninstalled, because the emptyDir
// is removed with the pod. Distroless images have no shell or tar, so each container is sent SIGTERM
// from its Kind node. runtime/coverage writes its counters on that exit, the container restarts
// inside the same pod, and the files are copied from the node's kubelet volume path.
// It is a no-op unless COVER=1.
func CollectCoverage(namespace string) error {
	if !CoverageEnabled() {
		return nil
	}

	repoRoot, err := GetRepoRoot()
	if err != nil {
		return fmt.Errorf("resolve repo root for coverage collection: %w", err)
	}

	outDir := filepath.Join(repoRoot, "coverage", "e2e")

	out, err := exec.Command("kubectl", "get", "pods", "--namespace", namespace, "-o", "json").Output()
	if err != nil {
		return fmt.Errorf("list pods for coverage collection: %w", err)
	}

	var pods coveragePodList
	if err := json.Unmarshal(out, &pods); err != nil {
		return fmt.Errorf("decode pods for coverage collection: %w", err)
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if !mountsCoverageVolume(container.VolumeMounts) {
				continue
			}

			service := serviceFor(container.Image)
			dest := filepath.Join(outDir, service)
			err := collectContainerCoverage(namespace, pod.Metadata.Name, container.Name, dest)
			if errors.Is(err, errCoveragePodGone) {
				fmt.Fprintf(os.Stderr, "skipping coverage from %s/%s: pod was replaced before collection\n", pod.Metadata.Name, container.Name)

				continue
			}

			if err != nil {
				return fmt.Errorf("collect coverage from %s/%s: %w", pod.Metadata.Name, container.Name, err)
			}
		}
	}

	return nil
}

// collectContainerCoverage re-reads the pod before each attempt so a container that was replaced
// after the initial listing is resolved to its current container ID.
func collectContainerCoverage(namespace, podName, containerName, dest string) error {
	var err error
	for attempt := 1; attempt <= coverageCollectAttempts; attempt++ {
		var pod coveragePod
		pod, err = getCoveragePod(namespace, podName)
		if err == nil {
			containerID := containerIDFor(pod, containerName)
			if containerID == "" {
				err = fmt.Errorf("container %s has no container ID", containerName)
			} else {
				err = copyContainerCoverage(pod.Spec.NodeName, pod.Metadata.UID, containerID, dest)
			}
		}

		if err == nil {
			return nil
		}

		if errors.Is(err, errCoveragePodGone) {
			return err
		}

		time.Sleep(2 * time.Second)
	}

	return err
}

func getCoveragePod(namespace, podName string) (coveragePod, error) {
	var pod coveragePod
	out, err := exec.Command("kubectl", "get", "pod", podName, "--namespace", namespace, "-o", "json").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && strings.Contains(string(ee.Stderr), "NotFound") {
			return pod, fmt.Errorf("%w: %s", errCoveragePodGone, podName)
		}

		return pod, fmt.Errorf("get pod %s: %w", podName, err)
	}

	if err := json.Unmarshal(out, &pod); err != nil {
		return pod, fmt.Errorf("decode pod %s: %w", podName, err)
	}

	return pod, nil
}

// serviceFor derives the service name from the controller image, for example
// "agent-controller:e2e-test" -> "agent". Container names are not unique across charts.
func serviceFor(image string) string {
	repo := image
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}

	return strings.TrimSuffix(filepath.Base(repo), "-controller")
}

func mountsCoverageVolume(mounts []struct {
	Name string `json:"name"`
}) bool {
	for _, mount := range mounts {
		if mount.Name == coverageVolumeName {
			return true
		}
	}

	return false
}

func containerIDFor(pod coveragePod, containerName string) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return strings.TrimPrefix(status.ContainerID, "containerd://")
		}
	}

	return ""
}

// copyContainerCoverage flushes one container's counters and copies them to dest.
func copyContainerCoverage(node, podUID, containerID, dest string) error {
	pidOut, err := Run(exec.Command("docker", "exec", node, "crictl", "inspect",
		"-o", "go-template", "--template", "{{.info.pid}}", containerID))
	if err != nil {
		return fmt.Errorf("resolve container pid: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(pidOut))
	if err != nil {
		return fmt.Errorf("parse container pid %q: %w", pidOut, err)
	}

	if _, err := Run(exec.Command("docker", "exec", node, "sh", "-c", fmt.Sprintf("kill -TERM %d", pid))); err != nil {
		return fmt.Errorf("send SIGTERM to pid %d: %w", pid, err)
	}

	volumeDir := fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes.io~empty-dir/%s", podUID, coverageVolumeName)
	deadline := time.Now().Add(coverageWaitTimeout)
	for {
		if _, err := Run(exec.Command("docker", "exec", node, "sh", "-c",
			fmt.Sprintf("kill -0 %d 2>/dev/null", pid))); err != nil {
			break
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("pid %d did not exit within %s", pid, coverageWaitTimeout)
		}

		time.Sleep(coverageWaitPollPeriod)
	}

	if _, err := Run(exec.Command("docker", "exec", node, "sh", "-c",
		fmt.Sprintf("ls %s/covcounters.* >/dev/null", volumeDir))); err != nil {
		return fmt.Errorf("no covcounters written to %s: %w", volumeDir, err)
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}

	if _, err := Run(exec.Command("docker", "cp", node+":"+volumeDir+"/.", dest)); err != nil {
		return fmt.Errorf("copy coverage to %s: %w", dest, err)
	}

	return nil
}
