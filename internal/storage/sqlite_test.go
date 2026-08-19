// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database"
	"miniflux.app/v2/internal/model"
)

func newSQLiteStore(t *testing.T) *Storage {
	t.Helper()

	opts, err := config.NewConfigParser().ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("ParseEnvironmentVariables() error = %v", err)
	}
	config.Opts = opts

	db, err := database.NewConnectionPool("sqlite://"+filepath.ToSlash(filepath.Join(t.TempDir(), "miniflux.db")), 0, 20, time.Minute)
	if err != nil {
		t.Fatalf("NewConnectionPool() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return NewStorage(db)
}

func TestSQLiteStorageEntries(t *testing.T) {
	store := newSQLiteStore(t)
	user, err := store.CreateUser(&model.UserCreationRequest{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	category, err := store.FirstCategory(user.ID)
	if err != nil {
		t.Fatalf("FirstCategory() error = %v", err)
	}

	integration, err := store.Integration(user.ID)
	if err != nil {
		t.Fatalf("Integration() error = %v", err)
	}
	integration.PinboardEnabled = true
	integration.PinboardToken = "token"
	if err := store.UpdateIntegration(integration); err != nil {
		t.Fatalf("UpdateIntegration() error = %v", err)
	}
	if !store.HasSaveEntry(user.ID) {
		t.Error("HasSaveEntry() = false after enabling Pinboard")
	}
	metadata, err := store.GetNavMetadata(user.ID)
	if err != nil {
		t.Fatalf("GetNavMetadata() error = %v", err)
	}
	if !metadata.HasSaveEntry {
		t.Error("GetNavMetadata().HasSaveEntry = false after enabling Pinboard")
	}

	feed := &model.Feed{
		UserID:  user.ID,
		FeedURL: "https://example.com/feed.xml",
		SiteURL: "https://example.com",
		Title:   "Example",
		Category: &model.Category{
			ID: category.ID,
		},
	}
	if err := store.CreateFeed(feed); err != nil {
		t.Fatalf("CreateFeed() error = %v", err)
	}
	if _, err := store.FeedByID(user.ID, feed.ID); err != nil {
		t.Fatalf("FeedByID() error = %v", err)
	}

	entry := model.NewEntry()
	entry.Hash = "entry-1"
	entry.Title = "SQLite search entry"
	entry.URL = "https://example.com/entry-1"
	entry.Content = "The entry body mentions database support."
	entry.Date = time.Now().Add(-time.Hour)
	entry.Tags = []string{"Go", "Database"}

	created, err := store.InsertEntryForFeed(user.ID, feed.ID, entry)
	if err != nil {
		t.Fatalf("InsertEntryForFeed() error = %v", err)
	}
	if !created {
		t.Fatal("InsertEntryForFeed() created = false, want true")
	}

	secondEntry := model.NewEntry()
	secondEntry.Hash = "entry-2"
	secondEntry.Title = "PostgreSQL search entry"
	secondEntry.URL = "https://example.com/entry-2"
	secondEntry.Content = "The second body is used to exercise SQLite FTS queries."
	secondEntry.Date = time.Now().Add(-49 * time.Hour)
	if created, err := store.InsertEntryForFeed(user.ID, feed.ID, secondEntry); err != nil || !created {
		t.Fatalf("second InsertEntryForFeed() = %t, %v; want true, nil", created, err)
	}
	if weeklyCount, err := store.WeeklyFeedEntryCount(user.ID, feed.ID); err != nil || weeklyCount == 0 {
		t.Fatalf("WeeklyFeedEntryCount() = %d, %v; want a non-zero count and nil error", weeklyCount, err)
	}

	entries, err := store.NewEntryQueryBuilder(user.ID).WithTags("go").WithSearchQuery("SQLite").GetEntries()
	if err != nil {
		t.Fatalf("entry query error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("entry query = %#v, want entry ID %d", entries, entry.ID)
	}
	if got := entries[0].Tags; len(got) != 2 || got[0] != "Go" || got[1] != "Database" {
		t.Errorf("entry tags = %#v, want [Go Database]", got)
	}
	orResults, err := store.NewEntryQueryBuilder(user.ID).WithSearchQuery("SQLite OR PostgreSQL").GetEntries()
	if err != nil || len(orResults) != 2 {
		t.Fatalf("OR search returned %d entries and %v error; want 2 entries and nil", len(orResults), err)
	}
	if invalidResults, err := store.NewEntryQueryBuilder(user.ID).WithSearchQuery(`"`).GetEntries(); err != nil || len(invalidResults) != 0 {
		t.Fatalf("invalid search returned %d entries and %v error; want 0 entries and nil", len(invalidResults), err)
	}
	withoutPostgreSQL, err := store.NewEntryQueryBuilder(user.ID).WithSearchQuery("-PostgreSQL").GetEntries()
	if err != nil || len(withoutPostgreSQL) != 1 || withoutPostgreSQL[0].ID != entry.ID {
		t.Fatalf("negative search returned %#v and %v error; want only entry ID %d", withoutPostgreSQL, err, entry.ID)
	}

	if err := store.SetEntriesStatus(user.ID, []int64{entry.ID}, model.EntryStatusRead); err != nil {
		t.Fatalf("SetEntriesStatus() error = %v", err)
	}
	if err := store.FlushHistory(user.ID); err != nil {
		t.Fatalf("FlushHistory() error = %v", err)
	}
	if store.IsNewEntry(feed.ID, entry.Hash) {
		t.Error("IsNewEntry() = true after history flush, want false because the entry was tombstoned")
	}

	apiKey, err := store.CreateAPIKey(user.ID, "SQLite test")
	if err != nil {
		t.Fatalf("CreateAPIKey() error = %v", err)
	}
	if err := store.SetAPIKeyUsedTimestamp(user.ID, apiKey.Token); err != nil {
		t.Fatalf("SetAPIKeyUsedTimestamp() error = %v", err)
	}

	cache := NewCertificateCache(store)
	if err := cache.Put(context.Background(), "certificate", []byte("data")); err != nil {
		t.Fatalf("certificate cache Put() error = %v", err)
	}
	if data, err := cache.Get(context.Background(), "certificate"); err != nil || string(data) != "data" {
		t.Fatalf("certificate cache Get() = %q, %v; want data, nil", data, err)
	}

	session, _ := model.NewWebSession("test-agent", "127.0.0.1")
	session.SetTheme("dark")
	if err := store.CreateWebSession(session); err != nil {
		t.Fatalf("CreateWebSession() error = %v", err)
	}
	loadedSession, err := store.WebSessionByID(session.ID)
	if err != nil {
		t.Fatalf("WebSessionByID() error = %v", err)
	}
	if loadedSession == nil || loadedSession.Theme() != "dark" {
		t.Fatalf("WebSessionByID() = %#v, want a session with dark theme", loadedSession)
	}
}
