// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// SQLite databases are created from a current-schema snapshot. The PostgreSQL
// migration history includes PostgreSQL-only types and data transformations,
// so it must not be replayed against SQLite.
const sqliteInitialSchemaVersion = 1

// Add SQLite-only migrations here after the initial schema snapshot. Each
// migration increments the SQLite schema version independently of PostgreSQL's
// historic migration sequence.
var sqliteMigrations = [...]func(tx *sql.Tx) error{}

func sqliteSchemaVersion() int {
	return sqliteInitialSchemaVersion + len(sqliteMigrations)
}

var sqliteSchema = []string{
	`CREATE TABLE schema_version (version INTEGER NOT NULL)`,
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password TEXT,
		is_admin INTEGER NOT NULL DEFAULT 0,
		language TEXT NOT NULL DEFAULT 'en_US',
		timezone TEXT NOT NULL DEFAULT 'UTC',
		theme TEXT NOT NULL DEFAULT 'light_serif',
		last_login_at DATETIME,
		entry_direction TEXT NOT NULL DEFAULT 'asc' CHECK (entry_direction IN ('asc', 'desc')),
		keyboard_shortcuts INTEGER NOT NULL DEFAULT 1,
		entries_per_page INTEGER NOT NULL DEFAULT 100,
		show_reading_time INTEGER NOT NULL DEFAULT 1,
		entry_swipe INTEGER NOT NULL DEFAULT 1,
		gesture_nav TEXT NOT NULL DEFAULT 'tap',
		stylesheet TEXT NOT NULL DEFAULT '',
		google_id TEXT NOT NULL DEFAULT '',
		openid_connect_id TEXT NOT NULL DEFAULT '',
		display_mode TEXT NOT NULL DEFAULT 'standalone',
		entry_order TEXT NOT NULL DEFAULT 'published_at',
		default_reading_speed INTEGER NOT NULL DEFAULT 265,
		cjk_reading_speed INTEGER NOT NULL DEFAULT 500,
		default_home_page TEXT NOT NULL DEFAULT 'unread',
		categories_sorting_order TEXT NOT NULL DEFAULT 'unread_count',
		mark_read_on_view INTEGER NOT NULL DEFAULT 1,
		mark_read_on_media_player_completion INTEGER NOT NULL DEFAULT 0,
		media_playback_rate REAL NOT NULL DEFAULT 1,
		block_filter_entry_rules TEXT NOT NULL DEFAULT '',
		keep_filter_entry_rules TEXT NOT NULL DEFAULT '',
		custom_js TEXT NOT NULL DEFAULT '',
		external_font_hosts TEXT NOT NULL DEFAULT '',
		always_open_external_links INTEGER NOT NULL DEFAULT 0,
		open_external_links_in_new_tab INTEGER NOT NULL DEFAULT 1
	)`,
	`CREATE UNIQUE INDEX users_google_id_idx ON users(google_id) WHERE google_id <> ''`,
	`CREATE UNIQUE INDEX users_openid_connect_id_idx ON users(openid_connect_id) WHERE openid_connect_id <> ''`,
	`CREATE TABLE categories (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		hide_globally INTEGER NOT NULL DEFAULT 0,
		UNIQUE (user_id, title)
	)`,
	`CREATE TABLE feeds (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		feed_url TEXT NOT NULL,
		site_url TEXT NOT NULL DEFAULT '',
		checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		next_check_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		etag_header TEXT NOT NULL DEFAULT '',
		last_modified_header TEXT NOT NULL DEFAULT '',
		parsing_error_msg TEXT NOT NULL DEFAULT '',
		parsing_error_count INTEGER NOT NULL DEFAULT 0,
		scraper_rules TEXT NOT NULL DEFAULT '',
		rewrite_rules TEXT NOT NULL DEFAULT '',
		url_rewrite_rules TEXT NOT NULL DEFAULT '',
		blocklist_rules TEXT NOT NULL DEFAULT '',
		keeplist_rules TEXT NOT NULL DEFAULT '',
		block_filter_entry_rules TEXT NOT NULL DEFAULT '',
		keep_filter_entry_rules TEXT NOT NULL DEFAULT '',
		crawler INTEGER NOT NULL DEFAULT 0,
		user_agent TEXT NOT NULL DEFAULT '',
		cookie TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		password TEXT NOT NULL DEFAULT '',
		disabled INTEGER NOT NULL DEFAULT 0,
		ignore_http_cache INTEGER NOT NULL DEFAULT 0,
		allow_self_signed_certificates INTEGER NOT NULL DEFAULT 0,
		fetch_via_proxy INTEGER NOT NULL DEFAULT 0,
		hide_globally INTEGER NOT NULL DEFAULT 0,
		no_media_player INTEGER NOT NULL DEFAULT 0,
		apprise_service_urls TEXT NOT NULL DEFAULT '',
		webhook_url TEXT NOT NULL DEFAULT '',
		disable_http2 INTEGER NOT NULL DEFAULT 0,
		description TEXT NOT NULL DEFAULT '',
		ntfy_enabled INTEGER NOT NULL DEFAULT 0,
		ntfy_priority INTEGER NOT NULL DEFAULT 3,
		ntfy_topic TEXT NOT NULL DEFAULT '',
		pushover_enabled INTEGER NOT NULL DEFAULT 0,
		pushover_priority INTEGER NOT NULL DEFAULT 0,
		proxy_url TEXT NOT NULL DEFAULT '',
		ignore_entry_updates INTEGER NOT NULL DEFAULT 0,
		language TEXT NOT NULL DEFAULT '',
		UNIQUE (user_id, feed_url)
	)`,
	`CREATE TABLE entries (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		hash TEXT NOT NULL,
		published_at DATETIME NOT NULL,
		title TEXT NOT NULL,
		url TEXT NOT NULL,
		comments_url TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		content TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'unread' CHECK (status IN ('unread', 'read')),
		starred INTEGER NOT NULL DEFAULT 0,
		reading_time INTEGER NOT NULL DEFAULT 0,
		share_code TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		changed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		tags TEXT NOT NULL DEFAULT '[]',
		language TEXT NOT NULL DEFAULT '',
		UNIQUE (feed_id, hash)
	)`,
	`CREATE INDEX entries_user_status_feed_idx ON entries(user_id, status, feed_id)`,
	`CREATE INDEX entries_user_status_changed_idx ON entries(user_id, status, changed_at)`,
	`CREATE INDEX entries_user_status_published_idx ON entries(user_id, status, published_at)`,
	`CREATE INDEX entries_user_status_created_idx ON entries(user_id, status, created_at)`,
	`CREATE INDEX entries_user_status_changed_published_idx ON entries(user_id, status, changed_at, published_at)`,
	`CREATE INDEX entries_id_user_status_idx ON entries(id, user_id, status)`,
	`CREATE INDEX entries_feed_id_status_hash_idx ON entries(feed_id, status, hash)`,
	`CREATE INDEX entries_user_id_status_starred_idx ON entries(user_id, status, starred)`,
	`CREATE UNIQUE INDEX entries_share_code_idx ON entries(share_code) WHERE share_code <> ''`,
	`CREATE VIRTUAL TABLE entries_fts USING fts5(title, content, content='entries', content_rowid='id', tokenize='unicode61 remove_diacritics 2')`,
	`CREATE TRIGGER entries_fts_insert AFTER INSERT ON entries BEGIN
		INSERT INTO entries_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
	END`,
	`CREATE TRIGGER entries_fts_delete AFTER DELETE ON entries BEGIN
		INSERT INTO entries_fts(entries_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
	END`,
	`CREATE TRIGGER entries_fts_update AFTER UPDATE OF title, content ON entries BEGIN
		INSERT INTO entries_fts(entries_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
		INSERT INTO entries_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
	END`,
	`CREATE TABLE enclosures (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
		url TEXT NOT NULL,
		size INTEGER NOT NULL DEFAULT 0,
		mime_type TEXT NOT NULL DEFAULT '',
		media_progression INTEGER NOT NULL DEFAULT 0,
		UNIQUE (user_id, entry_id, url)
	)`,
	`CREATE INDEX enclosures_entry_id_idx ON enclosures(entry_id)`,
	`CREATE TABLE icons (
		id INTEGER PRIMARY KEY,
		hash TEXT NOT NULL UNIQUE,
		mime_type TEXT NOT NULL,
		content BLOB NOT NULL,
		external_id TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE UNIQUE INDEX icons_external_id_idx ON icons(external_id) WHERE external_id <> ''`,
	`CREATE TABLE feed_icons (
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		icon_id INTEGER NOT NULL REFERENCES icons(id) ON DELETE CASCADE,
		PRIMARY KEY (feed_id, icon_id)
	)`,
	`CREATE TABLE api_keys (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL,
		last_used_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE (user_id, description)
	)`,
	`CREATE TABLE acme_cache (
		key TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		updated_at DATETIME NOT NULL
	)`,
	`CREATE TABLE webauthn_credentials (
		handle BLOB PRIMARY KEY,
		cred_id BLOB UNIQUE NOT NULL,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		public_key BLOB NOT NULL,
		attestation_type TEXT NOT NULL,
		aaguid BLOB,
		sign_count INTEGER,
		clone_warning INTEGER,
		name TEXT NOT NULL DEFAULT '',
		added_on DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen_on DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		backup_eligible INTEGER,
		backup_state INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE web_sessions (
		id TEXT PRIMARY KEY,
		secret_hash BLOB NOT NULL,
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		user_agent TEXT NOT NULL DEFAULT '',
		ip TEXT,
		state TEXT NOT NULL DEFAULT '{}'
	)`,
	`CREATE INDEX web_sessions_user_id_idx ON web_sessions(user_id) WHERE user_id IS NOT NULL`,
	`CREATE INDEX web_sessions_created_at_idx ON web_sessions(created_at)`,
	`CREATE TABLE entry_tombstones (
		feed_id INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
		hash TEXT NOT NULL CHECK (hash <> ''),
		deleted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (feed_id, hash)
	)`,
	`CREATE INDEX entry_tombstones_deleted_at_idx ON entry_tombstones(deleted_at)`,
	`CREATE TABLE integrations (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		pinboard_enabled INTEGER NOT NULL DEFAULT 0, pinboard_token TEXT NOT NULL DEFAULT '', pinboard_tags TEXT NOT NULL DEFAULT 'miniflux', pinboard_mark_as_unread INTEGER NOT NULL DEFAULT 0,
		instapaper_enabled INTEGER NOT NULL DEFAULT 0, instapaper_username TEXT NOT NULL DEFAULT '', instapaper_password TEXT NOT NULL DEFAULT '',
		fever_enabled INTEGER NOT NULL DEFAULT 0, fever_username TEXT NOT NULL DEFAULT '', fever_token TEXT NOT NULL DEFAULT '',
		googlereader_enabled INTEGER NOT NULL DEFAULT 0, googlereader_username TEXT NOT NULL DEFAULT '', googlereader_password TEXT NOT NULL DEFAULT '',
		wallabag_enabled INTEGER NOT NULL DEFAULT 0, wallabag_only_url INTEGER NOT NULL DEFAULT 0, wallabag_url TEXT NOT NULL DEFAULT '', wallabag_client_id TEXT NOT NULL DEFAULT '', wallabag_client_secret TEXT NOT NULL DEFAULT '', wallabag_username TEXT NOT NULL DEFAULT '', wallabag_password TEXT NOT NULL DEFAULT '', wallabag_tags TEXT NOT NULL DEFAULT '',
		notion_enabled INTEGER NOT NULL DEFAULT 0, notion_token TEXT NOT NULL DEFAULT '', notion_page_id TEXT NOT NULL DEFAULT '',
		nunux_keeper_enabled INTEGER NOT NULL DEFAULT 0, nunux_keeper_url TEXT NOT NULL DEFAULT '', nunux_keeper_api_key TEXT NOT NULL DEFAULT '',
		espial_enabled INTEGER NOT NULL DEFAULT 0, espial_url TEXT NOT NULL DEFAULT '', espial_api_key TEXT NOT NULL DEFAULT '', espial_tags TEXT NOT NULL DEFAULT '',
		readwise_enabled INTEGER NOT NULL DEFAULT 0, readwise_api_key TEXT NOT NULL DEFAULT '',
		telegram_bot_enabled INTEGER NOT NULL DEFAULT 0, telegram_bot_token TEXT NOT NULL DEFAULT '', telegram_bot_chat_id TEXT NOT NULL DEFAULT '', telegram_bot_topic_id INTEGER, telegram_bot_disable_web_page_preview INTEGER NOT NULL DEFAULT 0, telegram_bot_disable_notification INTEGER NOT NULL DEFAULT 0, telegram_bot_disable_buttons INTEGER NOT NULL DEFAULT 0,
		linkace_enabled INTEGER NOT NULL DEFAULT 0, linkace_url TEXT NOT NULL DEFAULT '', linkace_api_key TEXT NOT NULL DEFAULT '', linkace_tags TEXT NOT NULL DEFAULT '', linkace_is_private INTEGER NOT NULL DEFAULT 1, linkace_check_disabled INTEGER NOT NULL DEFAULT 1,
		linkding_enabled INTEGER NOT NULL DEFAULT 0, linkding_url TEXT NOT NULL DEFAULT '', linkding_api_key TEXT NOT NULL DEFAULT '', linkding_tags TEXT NOT NULL DEFAULT '', linkding_mark_as_unread INTEGER NOT NULL DEFAULT 0,
		linkwarden_enabled INTEGER NOT NULL DEFAULT 0, linkwarden_url TEXT NOT NULL DEFAULT '', linkwarden_api_key TEXT NOT NULL DEFAULT '', linkwarden_collection_id INTEGER,
		matrix_bot_enabled INTEGER NOT NULL DEFAULT 0, matrix_bot_user TEXT NOT NULL DEFAULT '', matrix_bot_password TEXT NOT NULL DEFAULT '', matrix_bot_url TEXT NOT NULL DEFAULT '', matrix_bot_chat_id TEXT NOT NULL DEFAULT '',
		apprise_enabled INTEGER NOT NULL DEFAULT 0, apprise_url TEXT NOT NULL DEFAULT '', apprise_services_url TEXT NOT NULL DEFAULT '',
		readeck_enabled INTEGER NOT NULL DEFAULT 0, readeck_url TEXT NOT NULL DEFAULT '', readeck_api_key TEXT NOT NULL DEFAULT '', readeck_labels TEXT NOT NULL DEFAULT '', readeck_only_url INTEGER NOT NULL DEFAULT 0, readeck_push_enabled INTEGER NOT NULL DEFAULT 0,
		shiori_enabled INTEGER NOT NULL DEFAULT 0, shiori_url TEXT NOT NULL DEFAULT '', shiori_username TEXT NOT NULL DEFAULT '', shiori_password TEXT NOT NULL DEFAULT '',
		shaarli_enabled INTEGER NOT NULL DEFAULT 0, shaarli_url TEXT NOT NULL DEFAULT '', shaarli_api_secret TEXT NOT NULL DEFAULT '',
		webhook_enabled INTEGER NOT NULL DEFAULT 0, webhook_url TEXT NOT NULL DEFAULT '', webhook_secret TEXT NOT NULL DEFAULT '',
		rssbridge_enabled INTEGER NOT NULL DEFAULT 0, rssbridge_url TEXT NOT NULL DEFAULT '', rssbridge_token TEXT NOT NULL DEFAULT '',
		omnivore_enabled INTEGER NOT NULL DEFAULT 0, omnivore_api_key TEXT NOT NULL DEFAULT '', omnivore_url TEXT NOT NULL DEFAULT '',
		raindrop_enabled INTEGER NOT NULL DEFAULT 0, raindrop_token TEXT NOT NULL DEFAULT '', raindrop_collection_id TEXT NOT NULL DEFAULT '', raindrop_tags TEXT NOT NULL DEFAULT '',
		betula_enabled INTEGER NOT NULL DEFAULT 0, betula_url TEXT NOT NULL DEFAULT '', betula_token TEXT NOT NULL DEFAULT '',
		ntfy_enabled INTEGER NOT NULL DEFAULT 0, ntfy_topic TEXT NOT NULL DEFAULT '', ntfy_url TEXT NOT NULL DEFAULT '', ntfy_api_token TEXT NOT NULL DEFAULT '', ntfy_username TEXT NOT NULL DEFAULT '', ntfy_password TEXT NOT NULL DEFAULT '', ntfy_icon_url TEXT NOT NULL DEFAULT '', ntfy_internal_links INTEGER NOT NULL DEFAULT 0,
		cubox_enabled INTEGER NOT NULL DEFAULT 0, cubox_api_link TEXT NOT NULL DEFAULT '',
		discord_enabled INTEGER NOT NULL DEFAULT 0, discord_webhook_link TEXT NOT NULL DEFAULT '',
		slack_enabled INTEGER NOT NULL DEFAULT 0, slack_webhook_link TEXT NOT NULL DEFAULT '',
		pushover_enabled INTEGER NOT NULL DEFAULT 0, pushover_user TEXT NOT NULL DEFAULT '', pushover_token TEXT NOT NULL DEFAULT '', pushover_device TEXT NOT NULL DEFAULT '', pushover_prefix TEXT NOT NULL DEFAULT '',
		karakeep_enabled INTEGER NOT NULL DEFAULT 0, karakeep_api_key TEXT NOT NULL DEFAULT '', karakeep_url TEXT NOT NULL DEFAULT '', karakeep_tags TEXT NOT NULL DEFAULT '',
		linktaco_enabled INTEGER NOT NULL DEFAULT 0, linktaco_api_token TEXT NOT NULL DEFAULT '', linktaco_org_slug TEXT NOT NULL DEFAULT '', linktaco_tags TEXT NOT NULL DEFAULT '', linktaco_visibility TEXT NOT NULL DEFAULT 'PUBLIC',
		archiveorg_enabled INTEGER NOT NULL DEFAULT 0
	)`,
}

func migrateSQLite(db *sql.DB) error {
	latestVersion := sqliteSchemaVersion()
	var currentVersion int
	err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion)
	switch {
	case err == nil:
		switch {
		case currentVersion == latestVersion:
			return nil
		case currentVersion < sqliteInitialSchemaVersion || currentVersion > latestVersion:
			return fmt.Errorf("SQLite database schema is not supported: current=v%d expected=v%d", currentVersion, latestVersion)
		}
	case errors.Is(err, sql.ErrNoRows):
		return errors.New("SQLite database schema is invalid: schema_version has no version")
	default:
		var schemaExists bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM sqlite_master WHERE type='table' AND name='schema_version')`).Scan(&schemaExists); err != nil {
			return err
		}
		if schemaExists {
			return err
		}

		slog.Info("Creating SQLite database schema", slog.Int("version", latestVersion))
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		for _, statement := range sqliteSchema {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("create SQLite schema: %w", err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES ($1)`, latestVersion); err != nil {
			return fmt.Errorf("record SQLite schema version: %w", err)
		}
		return tx.Commit()
	}

	slog.Info("Running SQLite database migrations", slog.Int("current_version", currentVersion), slog.Int("latest_version", latestVersion))
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for version := currentVersion; version < latestVersion; version++ {
		if err := sqliteMigrations[version-sqliteInitialSchemaVersion](tx); err != nil {
			return fmt.Errorf("SQLite migration v%d: %w", version+1, err)
		}
		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			return fmt.Errorf("record SQLite schema version: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES ($1)`, version+1); err != nil {
			return fmt.Errorf("record SQLite schema version: %w", err)
		}
	}
	return tx.Commit()
}

func isSQLiteSchemaUpToDate(db *sql.DB) error {
	var currentVersion int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion != sqliteSchemaVersion() {
		return fmt.Errorf("the database schema is not up to date: current=v%d expected=v%d", currentVersion, sqliteSchemaVersion())
	}
	return nil
}
