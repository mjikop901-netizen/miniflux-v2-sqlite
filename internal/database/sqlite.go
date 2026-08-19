// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteDriverName = "sqlite"

func isSQLiteURL(dsn string) bool {
	return dsn == ":memory:" || strings.HasPrefix(dsn, "file:") || strings.HasPrefix(dsn, "sqlite:")
}

func normalizeSQLiteDSN(dsn string) (string, error) {
	switch {
	case dsn == ":memory:" || strings.HasPrefix(dsn, "file:"):
		return withSQLitePragmas(dsn), nil
	case strings.HasPrefix(dsn, "sqlite://"):
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("invalid SQLite DATABASE_URL: %w", err)
		}

		if u.Host != "" && u.Path == "" {
			return "", fmt.Errorf("invalid SQLite DATABASE_URL: use sqlite:///absolute/path or sqlite://relative/path")
		}

		path := u.Path
		if u.Host != "" {
			path = u.Host + path
		}
		if path == "" {
			return "", fmt.Errorf("invalid SQLite DATABASE_URL: database path is empty")
		}

		u.RawPath = ""
		u.Path = path
		u.Scheme = "file"
		u.Host = ""
		return withSQLitePragmas(u.String()), nil
	case strings.HasPrefix(dsn, "sqlite:"):
		path := strings.TrimPrefix(dsn, "sqlite:")
		if path == "" {
			return "", fmt.Errorf("invalid SQLite DATABASE_URL: database path is empty")
		}
		return withSQLitePragmas("file:" + path), nil
	default:
		return "", fmt.Errorf("invalid SQLite DATABASE_URL: %q", dsn)
	}
}

func withSQLitePragmas(dsn string) string {
	if dsn == ":memory:" {
		return dsn + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_time_format=datetime&_timezone=UTC&_texttotime=1"
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	query := u.Query()
	query.Del("_time_integer_format")
	query.Set("_time_format", "datetime")
	query.Set("_timezone", "UTC")
	query.Set("_texttotime", "1")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = query.Encode()
	return u.String()
}

func isSQLiteInMemoryDSN(dsn string) bool {
	if strings.HasPrefix(dsn, ":memory:") || strings.HasPrefix(dsn, "file::memory:") {
		return true
	}
	u, err := url.Parse(dsn)
	return err == nil && u.Query().Get("mode") == "memory"
}

func newSQLiteConnectionPool(dsn string, connectionLifetime time.Duration) (*sql.DB, error) {
	dsn, err := normalizeSQLiteDSN(dsn)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, err
	}

	// SQLite serializes writers. A single pooled connection avoids lock errors
	// while the busy timeout in the DSN still protects independently opened DBs.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if !isSQLiteInMemoryDSN(dsn) {
		db.SetConnMaxLifetime(connectionLifetime)
	}

	return db, nil
}

func isSQLite(db *sql.DB) bool {
	var version string
	return db.QueryRow(`SELECT sqlite_version()`).Scan(&version) == nil
}
