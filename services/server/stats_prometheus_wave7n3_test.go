package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	wave7nDownloadsQuery = "opendepot_downloads_total"
	wave7nLastSeenQuery  = "opendepot_download_last_timestamp_seconds"
)

func wave7nPromVector(samples ...string) string {
	return `{"status":"success","data":{"resultType":"vector","result":[` + strings.Join(samples, ",") + `]}}`
}

func wave7nPromSample(namespace, kind, name, version, value string) string {
	return `{"metric":{"namespace":"` + namespace + `","kind":"` + kind + `","name":"` + name + `","version":"` + version + `"},"value":[1700000000,"` + value + `"]}`
}

func wave7nPromServer(t *testing.T, downloads, lastSeen string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		query := request.URL.Query().Get("query")
		switch {
		case strings.Contains(query, wave7nLastSeenQuery):
			_, _ = writer.Write([]byte(lastSeen))
		case strings.Contains(query, wave7nDownloadsQuery):
			_, _ = writer.Write([]byte(downloads))
		default:
			t.Errorf("unexpected query %q", query)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func wave7nUsePrometheus(t *testing.T, queryURL string) {
	t.Helper()

	old := prometheusStats
	t.Cleanup(func() { prometheusStats = old })

	if err := configurePrometheusStats(queryURL, "90d", time.Second); err != nil {
		t.Fatalf("configurePrometheusStats: %v", err)
	}
}

func TestWave7NConfigurePrometheusStatsRejectsInvalidInput(t *testing.T) {
	old := prometheusStats
	t.Cleanup(func() { prometheusStats = old })

	if err := configurePrometheusStats("http://prometheus:9090", "not-a-duration", time.Second); err == nil {
		t.Fatal("expected invalid lookback to fail")
	}

	if err := configurePrometheusStats("http://prometheus:9090", "90d", 0); err == nil {
		t.Fatal("expected zero query timeout to fail")
	}

	if err := configurePrometheusStats("", "90d", time.Second); err != nil {
		t.Fatalf("empty query URL should disable Prometheus stats: %v", err)
	}
}

func TestWave7NDownloadStatsSnapshotWithoutPrometheusIsEmpty(t *testing.T) {
	wave7nUsePrometheus(t, "")

	stats, err := downloadStatsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("downloadStatsSnapshot: %v", err)
	}

	if len(stats) != 0 {
		t.Fatalf("stats = %+v, want empty", stats)
	}
}

func TestWave7NDownloadStatsSnapshotPropagatesQueryFailures(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("upstream down"))
	}))
	t.Cleanup(failing.Close)

	wave7nUsePrometheus(t, failing.URL)

	if _, err := downloadStatsSnapshot(context.Background()); err == nil {
		t.Fatal("expected downloads query failure to propagate")
	}

	lastSeenFails := wave7nPromServer(t, wave7nPromVector(wave7nPromSample("ns", "module", "mod", "1.0.0", "3")), `{"status":"error","error":"boom"}`)
	wave7nUsePrometheus(t, lastSeenFails.URL)

	if _, err := downloadStatsSnapshot(context.Background()); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want last-seen query error", err)
	}
}

func TestWave7NPrometheusQueryErrorBranches(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"non-200 status":      {status: http.StatusServiceUnavailable, body: "unavailable"},
		"status not success":  {status: http.StatusOK, body: `{"status":"error","error":"bad query"}`},
		"malformed JSON body": {status: http.StatusOK, body: `{`},
		"unparseable value skipped": {status: http.StatusOK, body: wave7nPromVector(
			`{"metric":{"name":"bad"},"value":[1,"NaN-ish"]}`,
			`{"metric":{"name":"neg"},"value":[1,"-4"]}`,
			`{"metric":{"name":"short"},"value":[1]}`,
			`{"metric":{"name":"numeric"},"value":[1,5]}`,
			`{"metric":{"name":"good"},"value":[1,"2"]}`,
		)},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, _ = writer.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)

			wave7nUsePrometheus(t, server.URL)

			values, err := prometheusQuery(context.Background(), wave7nDownloadsQuery)
			if name == "unparseable value skipped" {
				if err != nil || len(values) != 1 || values[0].Labels["name"] != "good" || values[0].Value != 2 {
					t.Fatalf("values = %+v, err = %v, want only the good sample", values, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("expected error, got values %+v", values)
			}
		})
	}
}

func TestWave7NPrometheusQueryCanceledContext(t *testing.T) {
	server := wave7nPromServer(t, wave7nPromVector(), wave7nPromVector())
	wave7nUsePrometheus(t, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := prometheusQuery(ctx, wave7nDownloadsQuery); err == nil {
		t.Fatal("expected canceled context to fail the query")
	}
}

func TestWave7NTotalAndMostDownloadedFromPrometheus(t *testing.T) {
	downloads := wave7nPromVector(
		wave7nPromSample("team", "module", "vpc", "1.0.0", "4"),
		wave7nPromSample("team", "module", "vpc", "1.1.0", "6"),
		wave7nPromSample("team", "provider", "aws", "5.0.0", "3"),
		wave7nPromSample("other", "module", "iam", "2.0.0", "9"),
	)
	lastSeen := wave7nPromVector(
		wave7nPromSample("team", "module", "vpc", "1.1.0", "1700000000"),
	)

	wave7nUsePrometheus(t, wave7nPromServer(t, downloads, lastSeen).URL)

	total, err := queryTotalDownloads(context.Background(), "team")
	if err != nil {
		t.Fatalf("queryTotalDownloads: %v", err)
	}

	if total != 13 {
		t.Fatalf("total team downloads = %d, want 13", total)
	}

	all, err := queryTotalDownloads(context.Background(), "")
	if err != nil || all != 22 {
		t.Fatalf("total downloads = %d, err = %v, want 22", all, err)
	}

	popular, err := queryMostDownloaded(context.Background(), "team", 10)
	if err != nil {
		t.Fatalf("queryMostDownloaded: %v", err)
	}

	if len(popular) != 2 || popular[0].Name != "vpc" || popular[0].DownloadCount != 10 {
		t.Fatalf("popular = %+v, want vpc with 10 downloads first", popular)
	}
}

func TestWave7NBatchDownloadStats(t *testing.T) {
	downloads := wave7nPromVector(
		wave7nPromSample("team", "module", "vpc", "1.0.0", "4"),
		wave7nPromSample("team", "module", "vpc", "1.1.0", "6"),
		wave7nPromSample("team", "module", "iam", "2.0.0", "1"),
	)
	lastSeen := wave7nPromVector(
		wave7nPromSample("team", "module", "vpc", "1.0.0", "1700000000"),
		wave7nPromSample("team", "module", "vpc", "1.1.0", "1700000500"),
	)

	wave7nUsePrometheus(t, wave7nPromServer(t, downloads, lastSeen).URL)

	none, err := batchResourceDownloadStats(context.Background(), nil)
	if err != nil || none != nil {
		t.Fatalf("empty resource keys = %+v, err = %v, want nil", none, err)
	}

	resources, err := batchResourceDownloadStats(context.Background(), []string{"team/module/vpc", "team/module/missing"})
	if err != nil {
		t.Fatalf("batchResourceDownloadStats: %v", err)
	}

	vpc, ok := resources["team/module/vpc"]
	if !ok || vpc.Count != 10 || vpc.LastAt == "" {
		t.Fatalf("vpc stats = %+v (ok=%v), want 10 downloads with last-seen", vpc, ok)
	}

	if _, ok := resources["team/module/missing"]; ok {
		t.Fatal("resource with no downloads should be omitted")
	}

	versions, err := batchVersionDownloadStats(context.Background(), []string{"team/module/vpc/1.1.0", "team/module/vpc/9.9.9"})
	if err != nil {
		t.Fatalf("batchVersionDownloadStats: %v", err)
	}

	if got := versions["team/module/vpc/1.1.0"]; got.Count != 6 {
		t.Fatalf("version stats = %+v, want 6 downloads", got)
	}

	if _, ok := versions["team/module/vpc/9.9.9"]; ok {
		t.Fatal("unknown version should be omitted")
	}

	emptyVersions, err := batchVersionDownloadStats(context.Background(), nil)
	if err != nil || emptyVersions != nil {
		t.Fatalf("empty version keys = %+v, err = %v, want nil", emptyVersions, err)
	}
}

func TestWave7NBatchDownloadStatsPropagatesErrors(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	wave7nUsePrometheus(t, failing.URL)

	if _, err := batchResourceDownloadStats(context.Background(), []string{"team/module/vpc"}); err == nil {
		t.Fatal("expected resource batch to propagate query error")
	}

	if _, err := batchVersionDownloadStats(context.Background(), []string{"team/module/vpc/1.0.0"}); err == nil {
		t.Fatal("expected version batch to propagate query error")
	}
}

func TestWave7NResourceIsVisible(t *testing.T) {
	if !resourceIsVisible(nil, "team", "module", "vpc") {
		t.Fatal("nil visibility set should allow every resource")
	}

	visible := map[string]struct{}{"team/module/vpc": {}}
	if !resourceIsVisible(visible, "team", "Module", "vpc") {
		t.Fatal("kind comparison should be case-insensitive")
	}

	if resourceIsVisible(visible, "team", "module", "iam") {
		t.Fatal("unlisted resource should be hidden")
	}
}

func TestWave7NSortPopularResourcesOrdersByDownloadsThenName(t *testing.T) {
	resources := []PopularResource{
		{Namespace: "team", Kind: "module", Name: "zeta", DownloadCount: 2},
		{Namespace: "team", Kind: "module", Name: "alpha", DownloadCount: 2},
		{Namespace: "team", Kind: "module", Name: "top", DownloadCount: 9},
	}

	sortPopularResources(resources)

	want := []string{"top", "alpha", "zeta"}
	for i, name := range want {
		if resources[i].Name != name {
			t.Fatalf("order = %+v, want %v", resources, want)
		}
	}
}
