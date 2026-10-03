package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfigurePrometheusStats(t *testing.T) {
	if err := configurePrometheusStats("http://prometheus:9090/", "90d", time.Second); err != nil {
		t.Fatalf("configurePrometheusStats returned error: %v", err)
	}

	if got, want := prometheusStats.queryURL, "http://prometheus:9090"; got != want {
		t.Fatalf("queryURL = %q, want %q", got, want)
	}

	if got, want := prometheusStats.window, "90d"; got != want {
		t.Fatalf("window = %q, want %q", got, want)
	}

	if err := configurePrometheusStats("prometheus:9090", "90d", time.Second); err == nil {
		t.Fatal("expected relative Prometheus URL to fail")
	}
}

func TestPrometheusQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/query" {
			t.Errorf("path = %q, want /api/v1/query", request.URL.Path)
		}

		if !strings.Contains(request.URL.Query().Get("query"), "opendepot_downloads_total") {
			t.Errorf("query = %q, want download metric", request.URL.Query().Get("query"))
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"namespace":"ns","kind":"module","name":"mod","version":"1.0.0"},"value":["0","7"]}]}}`))
	}))

	defer server.Close()

	old := prometheusStats
	defer func() { prometheusStats = old }()
	if err := configurePrometheusStats(server.URL, "90d", time.Second); err != nil {
		t.Fatal(err)
	}

	values, err := prometheusQuery(context.Background(), "opendepot_downloads_total")
	if err != nil {
		t.Fatalf("prometheusQuery returned error: %v", err)
	}

	if len(values) != 1 || values[0].Value != 7 || values[0].Labels["name"] != "mod" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestDownloadStatsSnapshotAggregatesByServerInstance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		query := request.URL.Query().Get("query")
		if strings.Contains(query, "downloads_total") {
			_, _ = writer.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"namespace":"ns","kind":"module","name":"mod","version":"1.0.0"},"value":["0","7"]}]}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"namespace":"ns","kind":"module","name":"mod","version":"1.0.0"},"value":["0","1700000000"]}]}}`))
	}))

	defer server.Close()

	old := prometheusStats
	defer func() { prometheusStats = old }()
	if err := configurePrometheusStats(server.URL, "90d", time.Second); err != nil {
		t.Fatal(err)
	}

	stats, err := downloadStatsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("downloadStatsSnapshot returned error: %v", err)
	}

	got := stats["ns/module/mod/1.0.0"]
	if got.Count != 7 || got.LastAt == "" {
		t.Fatalf("unexpected stats: %#v", got)
	}
}

func TestDownloadSummariesFilterInvisibleResourcesBeforeLimiting(t *testing.T) {
	stats := map[string]resourceDownloadStats{
		"ns/module/private/1.0.0": {Count: 100},
		"ns/module/public/1.0.0":  {Count: 1},
	}

	visible := map[string]struct{}{"ns/module/public": {}}

	if got := totalDownloadsForStats(stats, "ns", visible); got != 1 {
		t.Fatalf("total downloads = %d, want 1", got)
	}

	popular := mostDownloadedForStats(stats, "ns", 1, visible)
	if len(popular) != 1 || popular[0].Name != "public" {
		t.Fatalf("popular resources = %#v, want public only", popular)
	}
}
