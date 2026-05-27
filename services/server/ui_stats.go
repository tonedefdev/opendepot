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

package main

import (
	"encoding/json"
	"net/http"
	"strings"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

// handleBrowseStats returns aggregate statistics for all visible registry resources.
// GET /opendepot/ui/v1/stats
//
// Query parameters:
//
//	namespace - optional namespace to scope stats (default: all namespaces)
func handleBrowseStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	namespace := r.URL.Query().Get("namespace")

	cs, err := browseSAClient()
	if err != nil {
		logger.Error("stats: failed to create SA client", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// List all modules.
	var moduleList opendepotv1alpha1.ModuleList
	{
		req := cs.RESTClient().Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Resource("modules")
		if namespace != "" {
			req = req.Namespace(namespace)
		}
		raw, err := req.DoRaw(r.Context())
		if err != nil {
			logger.Error("stats: failed to list modules", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(raw, &moduleList); err != nil {
			logger.Error("stats: failed to unmarshal modules", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	// List all providers.
	var providerList opendepotv1alpha1.ProviderList
	{
		req := cs.RESTClient().Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Resource("providers")
		if namespace != "" {
			req = req.Namespace(namespace)
		}
		raw, err := req.DoRaw(r.Context())
		if err != nil {
			logger.Error("stats: failed to list providers", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(raw, &providerList); err != nil {
			logger.Error("stats: failed to unmarshal providers", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	// List all versions.
	var versionList opendepotv1alpha1.VersionList
	{
		req := cs.RESTClient().Get().
			AbsPath("/apis/opendepot.defdev.io/v1alpha1").
			Resource("versions")
		if namespace != "" {
			req = req.Namespace(namespace)
		}
		raw, err := req.DoRaw(r.Context())
		if err != nil {
			logger.Error("stats: failed to list versions", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if err := json.Unmarshal(raw, &versionList); err != nil {
			logger.Error("stats: failed to unmarshal versions", "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Aggregate storage backend distribution from modules and providers.
	backendCounts := make(map[string]int)
	for _, m := range moduleList.Items {
		backend := storageBackendName(m.Spec.ModuleConfig.StorageConfig)
		backendCounts[backend]++
	}
	for _, p := range providerList.Items {
		backend := storageBackendName(p.Spec.ProviderConfig.StorageConfig)
		backendCounts[backend]++
	}

	storageDist := make([]StorageBackendStat, 0, len(backendCounts))
	for backend, count := range backendCounts {
		storageDist = append(storageDist, StorageBackendStat{Backend: backend, Count: count})
	}

	// Aggregate version stats: sync health, storage bytes, security posture.
	var (
		totalStorageBytes int64
		syncHealth        SyncHealthStats
		secPosture        SecurityPostureStats
		affectedResources = make(map[string]struct{})
	)

	for i := range versionList.Items {
		v := &versionList.Items[i]

		if v.Status.ArchiveSizeBytes != nil {
			totalStorageBytes += *v.Status.ArchiveSizeBytes
		}

		ss := strings.ToLower(v.Status.SyncStatus)
		isFailed := strings.Contains(ss, "failed") || strings.Contains(ss, "error")
		switch {
		case isFailed:
			syncHealth.FailedVersions++
		case v.Status.Synced:
			syncHealth.SyncedVersions++
		default:
			syncHealth.UnsyncedVersions++
		}

		// Accumulate security posture from module source scans.
		if v.Status.SourceScan != nil {
			for _, f := range v.Status.SourceScan.Findings {
				accumulateFinding(&secPosture, f)
				key := v.Namespace + "/" + v.Name
				affectedResources[key] = struct{}{}
			}
		}

		// Accumulate security posture from provider binary scans.
		if v.Status.BinaryScan != nil {
			for _, f := range v.Status.BinaryScan.Findings {
				accumulateFinding(&secPosture, f)
				key := v.Namespace + "/" + v.Name
				affectedResources[key] = struct{}{}
			}
		}
	}

	// Accumulate provider-level source scan findings.
	for i := range providerList.Items {
		p := &providerList.Items[i]
		if p.Status.SourceScan == nil {
			continue
		}
		for _, f := range p.Status.SourceScan.Findings {
			accumulateFinding(&secPosture, f)
			key := p.Namespace + "/provider/" + p.Name
			affectedResources[key] = struct{}{}
		}
	}

	secPosture.TotalAffectedResources = len(affectedResources)

	// Query download stats from SQLite.
	totalDownloads, err := queryTotalDownloads(r.Context(), statsDB, namespace)
	if err != nil {
		logger.Error("stats: failed to query total downloads", "error", err)
	}

	mostDownloaded, err := queryMostDownloaded(r.Context(), statsDB, namespace, 10)
	if err != nil {
		logger.Error("stats: failed to query most downloaded", "error", err)
	}
	if mostDownloaded == nil {
		mostDownloaded = []PopularResource{}
	}

	stats := BrowseStats{
		TotalModules:        len(moduleList.Items),
		TotalProviders:      len(providerList.Items),
		TotalVersions:       len(versionList.Items),
		TotalStorageBytes:   totalStorageBytes,
		TotalDownloads:      totalDownloads,
		SyncHealth:          syncHealth,
		SecurityPosture:     secPosture,
		StorageDistribution: storageDist,
		MostDownloaded:      mostDownloaded,
	}

	json.NewEncoder(w).Encode(stats)
}

// storageBackendName returns a display name for the active storage backend in sc.
func storageBackendName(sc *opendepotv1alpha1.StorageConfig) string {
	if sc == nil {
		return "unknown"
	}
	switch {
	case sc.S3 != nil:
		return "s3"
	case sc.AzureStorage != nil:
		return "azureStorage"
	case sc.GCS != nil:
		return "gcs"
	case sc.FileSystem != nil:
		return "fileSystem"
	default:
		return "unknown"
	}
}

// accumulateFinding increments the appropriate severity counter on secPosture.
func accumulateFinding(secPosture *SecurityPostureStats, f opendepotv1alpha1.SecurityFinding) {
	switch strings.ToUpper(f.Severity) {
	case "CRITICAL":
		secPosture.Critical++
	case "HIGH":
		secPosture.High++
	case "MEDIUM":
		secPosture.Medium++
	case "LOW":
		secPosture.Low++
	default:
		secPosture.Unknown++
	}
}
