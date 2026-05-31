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
	"context"
	"os"
	"testing"
)

func TestRecordDownloadAndQueryCount(t *testing.T) {
	f, err := os.CreateTemp("", "stats_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	db, err := initStatsDB(f.Name())
	if err != nil {
		t.Fatalf("initStatsDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Record three downloads: two for the same version, one for another.
	for i := 0; i < 2; i++ {
		if err := recordDownload(ctx, db, "test-ns", "module", "my-module", "1.0.0"); err != nil {
			t.Fatalf("recordDownload: %v", err)
		}
	}
	if err := recordDownload(ctx, db, "test-ns", "module", "my-module", "2.0.0"); err != nil {
		t.Fatalf("recordDownload: %v", err)
	}

	// Query per-version count.
	count, lastAt, err := queryVersionDownloads(ctx, db, "test-ns", "module", "my-module", "1.0.0")
	if err != nil {
		t.Fatalf("queryVersionDownloads: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 downloads for v1.0.0, got %d", count)
	}
	if lastAt == "" {
		t.Error("expected lastAt to be set, got empty string")
	}

	// Query resource-level count (all versions).
	rCount, _, err := queryResourceDownloads(ctx, db, "test-ns", "module", "my-module")
	if err != nil {
		t.Fatalf("queryResourceDownloads: %v", err)
	}
	if rCount != 3 {
		t.Errorf("expected 3 total downloads for my-module, got %d", rCount)
	}

	// Query total across all namespaces.
	total, err := queryTotalDownloads(ctx, db, "")
	if err != nil {
		t.Fatalf("queryTotalDownloads: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 total downloads, got %d", total)
	}

	// nil db must be a no-op (graceful degradation when stats are disabled).
	if err := recordDownload(ctx, nil, "ns", "module", "name", "1.0.0"); err != nil {
		t.Errorf("recordDownload with nil db should be a no-op, got: %v", err)
	}
}

func TestQueryMostDownloaded(t *testing.T) {
	f, err := os.CreateTemp("", "stats_most_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	db, err := initStatsDB(f.Name())
	if err != nil {
		t.Fatalf("initStatsDB: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// module-a: 3 downloads; module-b: 1 download.
	for i := 0; i < 3; i++ {
		if err := recordDownload(ctx, db, "ns", "module", "module-a", "1.0.0"); err != nil {
			t.Fatalf("recordDownload: %v", err)
		}
	}
	if err := recordDownload(ctx, db, "ns", "module", "module-b", "1.0.0"); err != nil {
		t.Fatalf("recordDownload: %v", err)
	}

	results, err := queryMostDownloaded(ctx, db, "", 5)
	if err != nil {
		t.Fatalf("queryMostDownloaded: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].DownloadCount < results[1].DownloadCount {
		t.Errorf("results must be sorted descending by download count")
	}
	if results[0].Name != "module-a" {
		t.Errorf("expected module-a to be most downloaded, got %s", results[0].Name)
	}
}
