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
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const createStatsSchema = `
CREATE TABLE IF NOT EXISTS download_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  namespace     TEXT NOT NULL,
  kind          TEXT NOT NULL,
  name          TEXT NOT NULL,
  version       TEXT NOT NULL,
  downloaded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_dl_lookup
  ON download_events(namespace, kind, name, version);
CREATE INDEX IF NOT EXISTS idx_dl_namespace
  ON download_events(namespace);
`

// initStatsDB opens (or creates) the SQLite stats database at the given path,
// enables WAL mode for concurrent reads, and creates the schema.
func initStatsDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("stats_db: open: %w", err)
	}

	// WAL mode allows multiple concurrent readers alongside a single writer.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("stats_db: WAL pragma: %w", err)
	}

	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("stats_db: synchronous pragma: %w", err)
	}

	if _, err := db.Exec(createStatsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("stats_db: create schema: %w", err)
	}

	return db, nil
}

// recordDownload records a download event for the given resource. It is a
// no-op when db is nil (stats tracking disabled).
func recordDownload(ctx context.Context, db *sql.DB, namespace, kind, name, version string) error {
	if db == nil {
		return nil
	}

	_, err := db.ExecContext(ctx,
		`INSERT INTO download_events (namespace, kind, name, version) VALUES (?, ?, ?, ?)`,
		namespace, kind, name, version,
	)
	if err != nil {
		return fmt.Errorf("stats_db: record download: %w", err)
	}

	return nil
}

// queryVersionDownloads returns the total download count and the RFC3339 timestamp
// of the most recent download for the given resource version. Returns zero values
// when db is nil.
func queryVersionDownloads(ctx context.Context, db *sql.DB, namespace, kind, name, version string) (count int64, lastAt string, err error) {
	if db == nil {
		return 0, "", nil
	}

	row := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(downloaded_at), '')
		 FROM download_events
		 WHERE namespace = ? AND kind = ? AND name = ? AND version = ?`,
		namespace, kind, name, version,
	)

	if err := row.Scan(&count, &lastAt); err != nil {
		return 0, "", fmt.Errorf("stats_db: query version downloads: %w", err)
	}

	return count, lastAt, nil
}

// queryResourceDownloads returns the total download count and most recent
// download timestamp for all versions of a resource. Returns zero values when
// db is nil.
func queryResourceDownloads(ctx context.Context, db *sql.DB, namespace, kind, name string) (count int64, lastAt string, err error) {
	if db == nil {
		return 0, "", nil
	}

	row := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MAX(downloaded_at), '')
		 FROM download_events
		 WHERE namespace = ? AND kind = ? AND name = ?`,
		namespace, kind, name,
	)

	if err := row.Scan(&count, &lastAt); err != nil {
		return 0, "", fmt.Errorf("stats_db: query resource downloads: %w", err)
	}

	return count, lastAt, nil
}

// queryTotalDownloads returns the total number of recorded download events,
// optionally scoped to a namespace. When namespace is empty all namespaces are
// counted. Returns 0 when db is nil.
func queryTotalDownloads(ctx context.Context, db *sql.DB, namespace string) (int64, error) {
	if db == nil {
		return 0, nil
	}

	var count int64
	var row *sql.Row
	if namespace == "" {
		row = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_events`)
	} else {
		row = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_events WHERE namespace = ?`, namespace)
	}

	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("stats_db: query total downloads: %w", err)
	}

	return count, nil
}

// queryMostDownloaded returns the top `limit` most-downloaded resources,
// optionally scoped to a namespace. Returns nil when db is nil.
func queryMostDownloaded(ctx context.Context, db *sql.DB, namespace string, limit int) ([]PopularResource, error) {
	if db == nil {
		return nil, nil
	}

	var (
		rows *sql.Rows
		err  error
	)

	if namespace == "" {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, version, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 GROUP BY namespace, kind, name, version
			 ORDER BY cnt DESC
			 LIMIT ?`,
			limit,
		)
	} else {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, version, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 WHERE namespace = ?
			 GROUP BY namespace, kind, name, version
			 ORDER BY cnt DESC
			 LIMIT ?`,
			namespace, limit,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("stats_db: query most downloaded: %w", err)
	}
	defer rows.Close()

	var result []PopularResource
	for rows.Next() {
		var r PopularResource
		if err := rows.Scan(&r.Namespace, &r.Kind, &r.Name, &r.Version, &r.DownloadCount, &r.LastDownloadedAt); err != nil {
			return nil, fmt.Errorf("stats_db: scan most downloaded row: %w", err)
		}
		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats_db: rows error: %w", err)
	}

	return result, nil
}

// queryAllResourceDownloadStats returns a map of "namespace/kind/name" to
// aggregated download stats for all resources, optionally scoped to a
// namespace. Returns nil when db is nil.
func queryAllResourceDownloadStats(ctx context.Context, db *sql.DB, namespace string) (map[string]resourceDownloadStats, error) {
	if db == nil {
		return nil, nil
	}

	var (
		rows *sql.Rows
		err  error
	)

	if namespace == "" {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 GROUP BY namespace, kind, name`,
		)
	} else {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 WHERE namespace = ?
			 GROUP BY namespace, kind, name`,
			namespace,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("stats_db: query all resource download stats: %w", err)
	}
	defer rows.Close()

	result := make(map[string]resourceDownloadStats)
	for rows.Next() {
		var ns, kind, name, lastAt string
		var cnt int64
		if err := rows.Scan(&ns, &kind, &name, &cnt, &lastAt); err != nil {
			return nil, fmt.Errorf("stats_db: scan resource download stats row: %w", err)
		}
		key := ns + "/" + kind + "/" + name
		result[key] = resourceDownloadStats{Count: cnt, LastAt: lastAt}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats_db: rows error: %w", err)
	}

	return result, nil
}

// queryAllVersionDownloadStats returns a map of "namespace/kind/name/version"
// to aggregated download stats, optionally scoped to a namespace. Returns nil
// when db is nil.
func queryAllVersionDownloadStats(ctx context.Context, db *sql.DB, namespace string) (map[string]resourceDownloadStats, error) {
	if db == nil {
		return nil, nil
	}

	var (
		rows *sql.Rows
		err  error
	)

	if namespace == "" {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, version, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 GROUP BY namespace, kind, name, version`,
		)
	} else {
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, kind, name, version, COUNT(*) AS cnt, COALESCE(MAX(downloaded_at), '')
			 FROM download_events
			 WHERE namespace = ?
			 GROUP BY namespace, kind, name, version`,
			namespace,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("stats_db: query all version download stats: %w", err)
	}
	defer rows.Close()

	result := make(map[string]resourceDownloadStats)
	for rows.Next() {
		var ns, kind, name, version, lastAt string
		var cnt int64
		if err := rows.Scan(&ns, &kind, &name, &version, &cnt, &lastAt); err != nil {
			return nil, fmt.Errorf("stats_db: scan version download stats row: %w", err)
		}
		key := ns + "/" + kind + "/" + name + "/" + version
		result[key] = resourceDownloadStats{Count: cnt, LastAt: lastAt}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats_db: rows error: %w", err)
	}

	return result, nil
}

// resourceDownloadStats holds aggregate download data for a single resource or version.
type resourceDownloadStats struct {
	Count  int64
	LastAt string
}
