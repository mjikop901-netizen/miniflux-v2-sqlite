// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteConnectionPoolAndSchema(t *testing.T) {
	databaseURL := "sqlite://" + filepath.ToSlash(filepath.Join(t.TempDir(), "miniflux.db"))
	db, err := NewConnectionPool(databaseURL, 0, 20, time.Minute)
	if err != nil {
		t.Fatalf("NewConnectionPool() error = %v", err)
	}
	defer db.Close()

	if !isSQLite(db) {
		t.Fatal("NewConnectionPool() did not open a SQLite database")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := IsSchemaUpToDate(db); err != nil {
		t.Fatalf("IsSchemaUpToDate() error = %v", err)
	}

	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("schema version query error = %v", err)
	}
	if version != sqliteSchemaVersion() {
		t.Errorf("schema version = %d, want %d", version, sqliteSchemaVersion())
	}
}

func TestSQLiteMemoryDatabaseSurvivesConnectionLifetime(t *testing.T) {
	db, err := NewConnectionPool(":memory:", 0, 20, time.Nanosecond)
	if err != nil {
		t.Fatalf("NewConnectionPool() error = %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := IsSchemaUpToDate(db); err != nil {
		t.Fatalf("in-memory schema disappeared after connection lifetime elapsed: %v", err)
	}
}

func TestNormalizeSQLiteDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "file URL", dsn: "file:miniflux.db", want: "file:miniflux.db?"},
		{name: "absolute sqlite URL", dsn: "sqlite:///var/lib/miniflux.db", want: "file:///var/lib/miniflux.db?"},
		{name: "memory database", dsn: ":memory:", want: ":memory:?"},
		{name: "relative sqlite URL", dsn: "sqlite:miniflux.db", want: "file:miniflux.db?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSQLiteDSN(tc.dsn)
			if err != nil {
				t.Fatalf("normalizeSQLiteDSN() error = %v", err)
			}
			if len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
				t.Errorf("normalizeSQLiteDSN() = %q, want prefix %q", got, tc.want)
			}
		})
	}

	normalized, err := normalizeSQLiteDSN("file:miniflux.db?_timezone=America/New_York&_texttotime=0&_time_integer_format=unix")
	if err != nil {
		t.Fatalf("normalizeSQLiteDSN() error = %v", err)
	}
	u, err := url.Parse(normalized)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := u.Query()
	if got := query.Get("_timezone"); got != "UTC" {
		t.Errorf("_timezone = %q, want UTC", got)
	}
	if got := query.Get("_time_format"); got != "datetime" {
		t.Errorf("_time_format = %q, want datetime", got)
	}
	if got := query.Get("_texttotime"); got != "1" {
		t.Errorf("_texttotime = %q, want 1", got)
	}
	if got := query.Get("_time_integer_format"); got != "" {
		t.Errorf("_time_integer_format = %q, want empty", got)
	}
}
