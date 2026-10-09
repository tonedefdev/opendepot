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
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"golang.org/x/mod/semver"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// agentKind describes how a Skill or an Agent maps onto Version resources.
type agentKind struct {
	controllerName string
	versionType    string
	labelKey       string
	namePrefix     string
}

var (
	skillKind = agentKind{
		controllerName: skillsControllerName,
		versionType:    opendepotv1alpha1.OpenDepotSkill,
		labelKey:       "opendepot.defdev.io/skill",
		namePrefix:     "skill",
	}

	agentKindDef = agentKind{
		controllerName: agentsControllerName,
		versionType:    opendepotv1alpha1.OpenDepotAgent,
		labelKey:       "opendepot.defdev.io/agent",
		namePrefix:     "agent",
	}
)

// reconcileAgentVersions creates a Version for each requested version that does not already exist.
// Existing Versions are never updated because their agentSourceRef is immutable and their spec
// carries user state such as Yanked. The returned map is keyed by the raw version string.
func reconcileAgentVersions(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	log logr.Logger,
	owner metav1.Object,
	kind agentKind,
	sourceConfig opendepotv1alpha1.AgentSourceConfig,
	versions []string,
) (map[string]*opendepotv1alpha1.Version, error) {
	configName := owner.GetName()
	if sourceConfig.Name != nil {
		configName = *sourceConfig.Name
	}

	versionObjects := make(map[string]*opendepotv1alpha1.Version, len(versions))
	for _, version := range versions {
		versionName := agentVersionName(kind, configName, version)
		object := client.ObjectKey{
			Name:      versionName,
			Namespace: owner.GetNamespace(),
		}

		existing := &opendepotv1alpha1.Version{}
		err := c.Get(ctx, object, existing)
		if err == nil {
			versionObjects[version] = existing
			continue
		}

		if !k8serr.IsNotFound(err) {
			return nil, fmt.Errorf("get %s version %s: %w", kind.namePrefix, versionName, err)
		}

		fileName, err := generateAgentFileName()
		if err != nil {
			return nil, fmt.Errorf("generate filename for %s version %s: %w", kind.namePrefix, versionName, err)
		}

		sourceRef := sourceConfig
		sourceRef.Name = &configName
		agentVersion := &opendepotv1alpha1.Version{
			ObjectMeta: metav1.ObjectMeta{
				Name:      versionName,
				Namespace: owner.GetNamespace(),
				Labels: map[string]string{
					kind.labelKey:                   owner.GetName(),
					"opendepot.defdev.io/namespace": owner.GetNamespace(),
				},
			},
			Spec: opendepotv1alpha1.VersionSpec{
				FileName:       fileName,
				Type:           kind.versionType,
				Version:        version,
				AgentSourceRef: &sourceRef,
			},
		}

		// Set the ownerRef for the Version, ensuring that the Version
		// is garbage collected when its Skill or Agent is deleted.
		if err = controllerutil.SetControllerReference(owner, agentVersion, scheme); err != nil {
			return nil, err
		}

		if err = c.Create(ctx, agentVersion, &client.CreateOptions{
			FieldManager: kind.controllerName,
		}); err != nil {
			return nil, fmt.Errorf("create %s version %s: %w", kind.namePrefix, versionName, err)
		}

		versionObjects[version] = agentVersion
		log.V(5).Info("Successfully created version",
			"kind", kind.versionType,
			"version", version,
			"name", owner.GetName(),
		)
	}

	return versionObjects, nil
}

// reconcileAgentVersionRemovals deletes Versions owned by the Skill or Agent whose version is no longer in keep.
func reconcileAgentVersionRemovals(ctx context.Context, c client.Client, log logr.Logger, owner metav1.Object, kind agentKind, keep []string) error {
	versionList := opendepotv1alpha1.VersionList{}
	labelsMap := map[string]string{
		kind.labelKey:                   owner.GetName(),
		"opendepot.defdev.io/namespace": owner.GetNamespace(),
	}

	labelSelector, err := labels.Parse(labels.FormatLabels(labelsMap))
	if err != nil {
		return err
	}

	if err = c.List(ctx, &versionList, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return err
	}

	for _, version := range versionList.Items {
		if slices.Contains(keep, version.Spec.Version) {
			continue
		}

		log.Info("Deleting version", "kind", kind.versionType, "name", owner.GetName(), "version", version.Spec.Version)
		if err = c.Delete(ctx, &version); err != nil {
			return err
		}
	}

	return nil
}

// agentVersionName returns the Version resource name for a Skill or Agent, for example
// skill-my-skill-1-0-0 or agent-my-agent-1-0-0.
func agentVersionName(kind agentKind, configName string, version string) string {
	return fmt.Sprintf("%s-%s-%s", kind.namePrefix, configName, sanitizeAgentVersion(version))
}

// sanitizeAgentVersion removes the leading 'v' and replaces '.' and '_' with '-' to produce
// a Kubernetes-safe resource name component.
func sanitizeAgentVersion(version string) string {
	version = strings.TrimPrefix(version, "v")
	version = strings.ToLower(version)
	version = strings.ReplaceAll(version, ".", "-")
	version = strings.ReplaceAll(version, "_", "-")
	return version
}

// canonicalAgentVersion returns the canonical semantic version with a leading 'v'.
func canonicalAgentVersion(version string) string {
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return semver.Canonical(version)
}

// sortAgentVersions returns the versions sorted in ascending semantic version order.
func sortAgentVersions(versions []string) []string {
	sorted := slices.Clone(versions)
	slices.SortFunc(sorted, func(a, b string) int {
		return semver.Compare(canonicalAgentVersion(a), canonicalAgentVersion(b))
	})
	return sorted
}

// latestAgentVersion returns the highest canonical semantic version or nil when there are no versions.
func latestAgentVersion(versions []string) *string {
	if len(versions) == 0 {
		return nil
	}

	sorted := sortAgentVersions(versions)
	latest := canonicalAgentVersion(sorted[len(sorted)-1])
	return &latest
}

// trimAgentVersions returns the most recent versions allowed by the version history limit.
// A nil or non-positive limit keeps every version.
func trimAgentVersions(versions []string, limit *int) []string {
	if limit == nil || *limit <= 0 || len(versions) <= *limit {
		return versions
	}

	sorted := sortAgentVersions(versions)
	return sorted[len(sorted)-*limit:]
}

// generateAgentFileName returns a randomly generated UUID7 filename for an archive.
func generateAgentFileName() (*string, error) {
	fileUUID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	fileName := fmt.Sprintf("%s.tar.gz", fileUUID)
	return &fileName, nil
}
