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
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, mr
}

func TestRecordDownloadAndQueryCount(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	// Record two downloads for v1.0.0 and one for v2.0.0.
	for range 2 {
		if err := recordDownload(ctx, client, "test-ns", "module", "my-module", "1.0.0"); err != nil {
			t.Fatalf("recordDownload: %v", err)
		}
	}

	if err := recordDownload(ctx, client, "test-ns", "module", "my-module", "2.0.0"); err != nil {
		t.Fatalf("recordDownload: %v", err)
	}

	// Per-version batch lookup for 1.0.0.
	vStats, err := batchVersionDownloadStats(ctx, client, []string{"test-ns/module/my-module/1.0.0"})
	if err != nil {
		t.Fatalf("batchVersionDownloadStats: %v", err)
	}

	s, ok := vStats["test-ns/module/my-module/1.0.0"]
	if !ok {
		t.Fatal("expected stats for v1.0.0, got nothing")
	}

	if s.Count != 2 {
		t.Errorf("expected 2 downloads for v1.0.0, got %d", s.Count)
	}

	if s.LastAt == "" {
		t.Error("expected lastAt to be set, got empty string")
	}

	// Resource-level batch lookup.
	rStats, err := batchResourceDownloadStats(ctx, client, []string{"test-ns/module/my-module"})
	if err != nil {
		t.Fatalf("batchResourceDownloadStats: %v", err)
	}

	rs, ok := rStats["test-ns/module/my-module"]
	if !ok {
		t.Fatal("expected resource stats for my-module, got nothing")
	}

	if rs.Count != 3 {
		t.Errorf("expected 3 total downloads for my-module, got %d", rs.Count)
	}

	// Global total.
	total, err := queryTotalDownloads(ctx, client, "")
	if err != nil {
		t.Fatalf("queryTotalDownloads: %v", err)
	}

	if total != 3 {
		t.Errorf("expected global total of 3, got %d", total)
	}

	// Namespace-scoped total.
	nsTotal, err := queryTotalDownloads(ctx, client, "test-ns")
	if err != nil {
		t.Fatalf("queryTotalDownloads namespace: %v", err)
	}

	if nsTotal != 3 {
		t.Errorf("expected ns total of 3, got %d", nsTotal)
	}
}

func TestQueryMostDownloaded(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	// module-a: 3 downloads; module-b: 1 download.
	for range 3 {
		if err := recordDownload(ctx, client, "ns", "module", "module-a", "1.0.0"); err != nil {
			t.Fatalf("recordDownload: %v", err)
		}
	}

	if err := recordDownload(ctx, client, "ns", "module", "module-b", "1.0.0"); err != nil {
		t.Fatalf("recordDownload: %v", err)
	}

	results, err := queryMostDownloaded(ctx, client, "", 5)
	if err != nil {
		t.Fatalf("queryMostDownloaded: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	if results[0].DownloadCount < results[1].DownloadCount {
		t.Error("results must be sorted descending by download count")
	}

	if results[0].Name != "module-a" {
		t.Errorf("expected module-a to be most downloaded, got %s", results[0].Name)
	}

	// Namespace-scoped leaderboard should match.
	nsResults, err := queryMostDownloaded(ctx, client, "ns", 5)
	if err != nil {
		t.Fatalf("queryMostDownloaded namespace: %v", err)
	}

	if len(nsResults) < 2 {
		t.Fatalf("expected at least 2 namespace-scoped results, got %d", len(nsResults))
	}
}

func TestBatchEmptyKeys(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	rStats, err := batchResourceDownloadStats(ctx, client, nil)
	if err != nil {
		t.Fatalf("batchResourceDownloadStats empty: %v", err)
	}

	if rStats != nil {
		t.Error("expected nil map for empty key slice")
	}

	vStats, err := batchVersionDownloadStats(ctx, client, nil)
	if err != nil {
		t.Fatalf("batchVersionDownloadStats empty: %v", err)
	}

	if vStats != nil {
		t.Error("expected nil map for empty key slice")
	}
}

func TestConcurrentRecordDownload(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			if err := recordDownload(ctx, client, "ns", "module", "concurrent-mod", "1.0.0"); err != nil {
				t.Errorf("recordDownload: %v", err)
			}
		}()
	}
	wg.Wait()

	total, err := queryTotalDownloads(ctx, client, "")
	if err != nil {
		t.Fatalf("queryTotalDownloads: %v", err)
	}

	if total != goroutines {
		t.Errorf("expected %d total downloads after concurrent writes, got %d", goroutines, total)
	}
}
