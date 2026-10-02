package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
)

var prometheusStats = struct {
	client       *http.Client
	queryURL     string
	lookback     string
	window       string
	queryTimeout time.Duration
}{
	client: http.DefaultClient,
}

func configurePrometheusStats(queryURL, lookback string, queryTimeout time.Duration) error {
	if _, err := model.ParseDuration(lookback); err != nil {
		return fmt.Errorf("invalid lookback %q: %w", lookback, err)
	}

	if queryTimeout <= 0 {
		return fmt.Errorf("query timeout must be greater than zero")
	}

	if queryURL != "" {
		parsed, err := url.Parse(queryURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("Prometheus URL must be an absolute URL")
		}

		queryURL = strings.TrimSuffix(queryURL, "/")
	}

	prometheusStats.queryURL = queryURL
	prometheusStats.lookback = lookback
	prometheusStats.window = lookback
	prometheusStats.queryTimeout = queryTimeout

	return nil
}

func queryTotalDownloads(ctx context.Context, namespace string) (int64, error) {
	stats, err := downloadStatsSnapshot(ctx)
	if err != nil {
		return 0, err
	}

	return totalDownloadsForStats(stats, namespace, nil), nil
}

func totalDownloadsForStats(stats map[string]resourceDownloadStats, namespace string, visible map[string]struct{}) int64 {
	var total int64
	for key, value := range stats {
		ns, kind, name, _, ok := splitVersionKey(key)
		if ok && (namespace == "" || namespace == ns) && resourceIsVisible(visible, ns, kind, name) {
			total += value.Count
		}
	}

	return total
}

func queryMostDownloaded(ctx context.Context, namespace string, limit int) ([]PopularResource, error) {
	stats, err := downloadStatsSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	return mostDownloadedForStats(stats, namespace, limit, nil), nil
}

func mostDownloadedForStats(stats map[string]resourceDownloadStats, namespace string, limit int, visible map[string]struct{}) []PopularResource {
	byResource := make(map[string]PopularResource)
	for key, value := range stats {
		ns, kind, name, version, ok := splitVersionKey(key)
		if !ok || namespace != "" && namespace != ns || !resourceIsVisible(visible, ns, kind, name) {
			continue
		}

		resourceKey := ns + "/" + kind + "/" + name
		resource := byResource[resourceKey]
		resource.Namespace = ns
		resource.Kind = kind
		resource.Name = name
		resource.Version = version
		resource.DownloadCount += value.Count
		if value.LastAt > resource.LastDownloadedAt {
			resource.LastDownloadedAt = value.LastAt
		}
		byResource[resourceKey] = resource
	}

	result := make([]PopularResource, 0, len(byResource))
	for _, resource := range byResource {
		result = append(result, resource)
	}

	sortPopularResources(result)
	if limit < len(result) {
		result = result[:limit]
	}

	return result
}

func resourceIsVisible(visible map[string]struct{}, namespace, kind, name string) bool {
	if visible == nil {
		return true
	}
	_, ok := visible[namespace+"/"+strings.ToLower(kind)+"/"+name]
	return ok
}

func batchResourceDownloadStats(ctx context.Context, keys []string) (map[string]resourceDownloadStats, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	stats, err := downloadStatsSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]resourceDownloadStats, len(keys))
	for _, key := range keys {
		var value resourceDownloadStats
		for versionKey, versionStats := range stats {
			ns, kind, name, _, ok := splitVersionKey(versionKey)
			if ok && key == ns+"/"+kind+"/"+name {
				value.Count += versionStats.Count
				if versionStats.LastAt > value.LastAt {
					value.LastAt = versionStats.LastAt
				}
			}
		}
		if value.Count > 0 {
			result[key] = value
		}
	}

	return result, nil
}

func batchVersionDownloadStats(ctx context.Context, keys []string) (map[string]resourceDownloadStats, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	stats, err := downloadStatsSnapshot(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]resourceDownloadStats, len(keys))
	for _, key := range keys {
		if value, ok := stats[key]; ok {
			result[key] = value
		}
	}

	return result, nil
}

type resourceDownloadStats struct {
	Count  int64
	LastAt string
}

func downloadStatsSnapshot(ctx context.Context) (map[string]resourceDownloadStats, error) {
	result := make(map[string]resourceDownloadStats)
	if prometheusStats.queryURL == "" {
		return result, nil
	}

	query := fmt.Sprintf("sum by (namespace, kind, name, version) (last_over_time(opendepot_downloads_total[%s]))", prometheusStats.lookback)
	values, err := prometheusQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	for _, value := range values {
		key := value.Labels["namespace"] + "/" + value.Labels["kind"] + "/" + value.Labels["name"] + "/" + value.Labels["version"]
		if !math.IsNaN(value.Value) && !math.IsInf(value.Value, 0) && value.Value >= 0 {
			result[key] = resourceDownloadStats{Count: int64(math.Round(value.Value))}
		}
	}

	query = fmt.Sprintf("max by (namespace, kind, name, version) (max_over_time(opendepot_download_last_timestamp_seconds[%s]))", prometheusStats.lookback)
	values, err = prometheusQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	for _, value := range values {
		key := value.Labels["namespace"] + "/" + value.Labels["kind"] + "/" + value.Labels["name"] + "/" + value.Labels["version"]
		stats := result[key]
		stats.LastAt = time.Unix(int64(value.Value), 0).UTC().Format(time.RFC3339)
		result[key] = stats
	}

	return result, nil
}

type prometheusSample struct {
	Labels map[string]string
	Value  float64
}

func prometheusQuery(ctx context.Context, query string) ([]prometheusSample, error) {
	queryCtx, cancel := context.WithTimeout(ctx, prometheusStats.queryTimeout)
	defer cancel()

	requestURL := prometheusStats.queryURL + "/api/v1/query?" + url.Values{"query": []string{query}}.Encode()
	request, err := http.NewRequestWithContext(queryCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	response, err := prometheusStats.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Prometheus query returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string            `json:"resultType"`
			Result     []json.RawMessage `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("Prometheus query failed: %s", payload.Error)
	}

	result := make([]prometheusSample, 0, len(payload.Data.Result))
	for _, raw := range payload.Data.Result {
		var sample struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(raw, &sample); err != nil || len(sample.Value) != 2 {
			continue
		}

		var value string
		if err := json.Unmarshal(sample.Value[1], &value); err != nil {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed < 0 {
			continue
		}

		result = append(result, prometheusSample{Labels: sample.Metric, Value: parsed})
	}

	return result, nil
}

func sortPopularResources(resources []PopularResource) {
	for i := range resources {
		for j := i + 1; j < len(resources); j++ {
			left := resources[i].Namespace + "/" + resources[i].Kind + "/" + resources[i].Name
			right := resources[j].Namespace + "/" + resources[j].Kind + "/" + resources[j].Name
			if resources[j].DownloadCount > resources[i].DownloadCount ||
				resources[j].DownloadCount == resources[i].DownloadCount && right < left {
				resources[i], resources[j] = resources[j], resources[i]
			}
		}
	}
}
