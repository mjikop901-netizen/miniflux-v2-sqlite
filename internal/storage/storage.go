// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Storage handles all operations related to the database.
type Storage struct {
	db     *sql.DB
	sqlite bool
}

// NewStorage returns a new Storage.
func NewStorage(db *sql.DB) *Storage {
	return &Storage{db: db, sqlite: isSQLite(db)}
}

// DatabaseVersion returns the version of the database which is in use.
func (s *Storage) DatabaseVersion() string {
	var query string
	if s.sqlite {
		query = `SELECT sqlite_version()`
	} else {
		query = `SELECT current_setting('server_version')`
	}

	var dbVersion string
	err := s.db.QueryRow(query).Scan(&dbVersion)
	if err != nil {
		return err.Error()
	}

	return dbVersion
}

// Ping checks if the database connection works.
func (s *Storage) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.db.PingContext(ctx)
}

// DBStats returns database statistics.
func (s *Storage) DBStats() sql.DBStats {
	return s.db.Stats()
}

// DBSize returns how much size the database is using in a pretty way.
func (s *Storage) DBSize() (string, error) {
	if !s.sqlite {
		var size string
		if err := s.db.QueryRow("SELECT pg_size_pretty(pg_database_size(current_database()))").Scan(&size); err != nil {
			return "", err
		}
		return size, nil
	}

	var pageCount, pageSize int64
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return "", err
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d bytes", pageCount*pageSize), nil
}
