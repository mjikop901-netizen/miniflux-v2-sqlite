// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"time"

	_ "github.com/lib/pq"
)

// NewConnectionPool configures the database connection pool. SQLite is selected
// when DATABASE_URL uses a sqlite: or file: URL; all other URLs use PostgreSQL.
func NewConnectionPool(dsn string, minConnections, maxConnections int, connectionLifetime time.Duration) (*sql.DB, error) {
	if isSQLiteURL(dsn) {
		return newSQLiteConnectionPool(dsn, connectionLifetime)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(minConnections)
	db.SetConnMaxLifetime(connectionLifetime)

	return db, nil
}
