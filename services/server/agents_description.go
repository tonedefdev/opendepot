package main

import (
	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// agentDescriptionFor returns the author-declared description of the Version whose spec version is latest.
// It returns an empty string when latest is empty, no Version matches, or the match has no metadata.
func agentDescriptionFor(versions []*opendepotv1alpha1.Version, latest string) string {
	if latest == "" {
		return ""
	}

	for _, v := range versions {
		if v.Spec.Version != latest {
			continue
		}

		if metadata := v.Status.AgentMetadata; metadata != nil {
			return metadata.Description
		}

		return ""
	}

	return ""
}
