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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyGlobalTotal is the global download counter key.
const keyGlobalTotal = "stats:total"

// keyNSTotal returns the namespace-scoped download counter key.
func keyNSTotal(namespace string) string {
	return "stats:ns:" + namespace
}

// keyResourceHash returns the hash key for a resource-level rollup (count + lastAt).
func keyResourceHash(namespace, kind, name string) string {
	return fmt.Sprintf("stats:resource:%s:%s:%s", namespace, kind, name)
}

// keyVersionHash returns the hash key for a version-level rollup (count + lastAt).
func keyVersionHash(namespace, kind, name, version string) string {
	return fmt.Sprintf("stats:version:%s:%s:%s:%s", namespace, kind, name, version)
}

// keyLeaderboard returns the sorted-set leaderboard key for the given scope.
// When namespace is empty the global leaderboard key is returned.
func keyLeaderboard(namespace string) string {
	if namespace == "" {
		return "stats:leaderboard:global"
	}

	return "stats:leaderboard:" + namespace
}

// keyDailyCounter returns the per-day counter key for a version.
// The key carries a 90-day TTL and is used for future time-series analytics.
func keyDailyCounter(namespace, kind, name, version string) string {
	day := time.Now().UTC().Format("2006-01-02")
	return fmt.Sprintf("stats:day:%s:%s:%s:%s:%s", day, namespace, kind, name, version)
}

// dayTTL returns the UNIX timestamp of midnight UTC 90 days from now, used
// as the EXPIREAT deadline for daily counter keys.
func dayTTL() time.Time {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+90, 0, 0, 0, 0, time.UTC)
	return midnight
}

// recordDownload atomically records one download event for the given resource
// version using a single Valkey pipeline. All nine commands are sent in one
// network round trip, so concurrent server pods cannot produce partial writes.
func recordDownload(ctx context.Context, client *redis.Client, namespace, kind, name, version string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	member := fmt.Sprintf("%s/%s/%s/%s", namespace, kind, name, version)

	_, err := client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// Version-level hash.
		pipe.HIncrBy(ctx, keyVersionHash(namespace, kind, name, version), "count", 1)
		pipe.HSet(ctx, keyVersionHash(namespace, kind, name, version), "lastAt", now)

		// Resource-level hash.
		pipe.HIncrBy(ctx, keyResourceHash(namespace, kind, name), "count", 1)
		pipe.HSet(ctx, keyResourceHash(namespace, kind, name), "lastAt", now)

		// Global and namespace-scoped counters.
		pipe.Incr(ctx, keyGlobalTotal)
		pipe.Incr(ctx, keyNSTotal(namespace))

		// Leaderboard sorted sets.
		pipe.ZIncrBy(ctx, keyLeaderboard(""), 1, member)
		pipe.ZIncrBy(ctx, keyLeaderboard(namespace), 1, member)

		// Daily counter with 90-day TTL for future analytics.
		dayKey := keyDailyCounter(namespace, kind, name, version)
		pipe.Incr(ctx, dayKey)
		pipe.ExpireAt(ctx, dayKey, dayTTL())

		return nil
	})

	if err != nil {
		return fmt.Errorf("stats: record download: %w", err)
	}

	return nil
}

// queryTotalDownloads returns the total number of recorded download events.
// When namespace is empty the global total is returned.
func queryTotalDownloads(ctx context.Context, client *redis.Client, namespace string) (int64, error) {
	key := keyGlobalTotal
	if namespace != "" {
		key = keyNSTotal(namespace)
	}

	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("stats: query total downloads: %w", err)
	}

	count, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("stats: parse total downloads: %w", err)
	}

	return count, nil
}

// queryMostDownloaded returns the top limit most-downloaded resource versions,
// optionally scoped to a namespace. Results are sorted descending by download count.
func queryMostDownloaded(ctx context.Context, client *redis.Client, namespace string, limit int) ([]PopularResource, error) {
	key := keyLeaderboard(namespace)

	members, err := client.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
	if err == redis.Nil {
		return []PopularResource{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("stats: query most downloaded: %w", err)
	}

	result := make([]PopularResource, 0, len(members))
	for _, m := range members {
		ns, kind, name, version, ok := splitLeaderboardMember(m.Member.(string))
		if !ok {
			continue
		}

		// Fetch lastAt from the version hash.
		lastAt, _ := client.HGet(ctx, keyVersionHash(ns, kind, name, version), "lastAt").Result()

		result = append(result, PopularResource{
			Namespace:        ns,
			Kind:             kind,
			Name:             name,
			Version:          version,
			DownloadCount:    int64(m.Score),
			LastDownloadedAt: lastAt,
		})
	}

	return result, nil
}

// batchResourceDownloadStats fetches aggregate download stats for a slice of
// resource keys in a single pipeline round trip. Each key must be in the form
// "namespace/kind/name". The returned map uses the same key format.
func batchResourceDownloadStats(ctx context.Context, client *redis.Client, keys []string) (map[string]resourceDownloadStats, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	cmds := make([]*redis.MapStringStringCmd, len(keys))
	_, err := client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, k := range keys {
			ns, kind, name, ok := splitResourceKey(k)
			if !ok {
				continue
			}

			cmds[i] = pipe.HGetAll(ctx, keyResourceHash(ns, kind, name))
		}

		return nil
	})

	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("stats: batch resource stats: %w", err)
	}

	result := make(map[string]resourceDownloadStats, len(keys))
	for i, cmd := range cmds {
		if cmd == nil {
			continue
		}

		vals, err := cmd.Result()
		if err != nil || len(vals) == 0 {
			continue
		}

		count, _ := strconv.ParseInt(vals["count"], 10, 64)
		result[keys[i]] = resourceDownloadStats{Count: count, LastAt: vals["lastAt"]}
	}

	return result, nil
}

// batchVersionDownloadStats fetches aggregate download stats for a slice of
// version keys in a single pipeline round trip. Each key must be in the form
// "namespace/kind/name/version". The returned map uses the same key format.
func batchVersionDownloadStats(ctx context.Context, client *redis.Client, keys []string) (map[string]resourceDownloadStats, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	cmds := make([]*redis.MapStringStringCmd, len(keys))
	_, err := client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for i, k := range keys {
			ns, kind, name, version, ok := splitVersionKey(k)
			if !ok {
				continue
			}

			cmds[i] = pipe.HGetAll(ctx, keyVersionHash(ns, kind, name, version))
		}

		return nil
	})

	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("stats: batch version stats: %w", err)
	}

	result := make(map[string]resourceDownloadStats, len(keys))
	for i, cmd := range cmds {
		if cmd == nil {
			continue
		}

		vals, err := cmd.Result()
		if err != nil || len(vals) == 0 {
			continue
		}

		count, _ := strconv.ParseInt(vals["count"], 10, 64)
		result[keys[i]] = resourceDownloadStats{Count: count, LastAt: vals["lastAt"]}
	}

	return result, nil
}

// resourceDownloadStats holds aggregate download data for a single resource or version.
type resourceDownloadStats struct {
	Count  int64
	LastAt string
}

// splitLeaderboardMember splits a leaderboard member string of the form
// "namespace/kind/name/version" into its components.
func splitLeaderboardMember(s string) (ns, kind, name, version string, ok bool) {
	// Members use "/" as separator; version strings can contain "/" in edge cases
	// but conventionally do not. Split on first three "/" occurrences only.
	parts := strings.SplitN(s, "/", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}

	return parts[0], parts[1], parts[2], parts[3], true
}

// splitResourceKey splits a key of the form "namespace/kind/name".
func splitResourceKey(s string) (ns, kind, name string, ok bool) {
	parts := strings.SplitN(s, "/", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}

	return parts[0], parts[1], parts[2], true
}

// splitVersionKey splits a key of the form "namespace/kind/name/version".
func splitVersionKey(s string) (ns, kind, name, version string, ok bool) {
	parts := strings.SplitN(s, "/", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}

	return parts[0], parts[1], parts[2], parts[3], true
}
