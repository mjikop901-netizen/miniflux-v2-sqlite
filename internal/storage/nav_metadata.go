// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"log/slog"

	"miniflux.app/v2/internal/config"
)

type NavMetadata struct {
	CountUnread     int
	CountErrorFeeds int
	HasSaveEntry    bool
}

// GetNavMetadata returns the navigation metadata for the given user in a
// single SQL query.
func (s *Storage) GetNavMetadata(userID int64) (NavMetadata, error) {
	query := `
		SELECT
			(SELECT count(*)
			   FROM entries e
			   JOIN feeds f ON f.id = e.feed_id
			   JOIN categories c ON c.id = f.category_id
			  WHERE e.user_id = $1
			    AND e.status = 'unread'
			    AND f.hide_globally IS FALSE
			    AND c.hide_globally IS FALSE
			) AS count_unread,
			(SELECT EXISTS(
				SELECT 1
				  FROM integrations
				 WHERE user_id = $1
				   AND (
					pinboard_enabled IS TRUE OR
					instapaper_enabled IS TRUE OR
					wallabag_enabled IS TRUE OR
					notion_enabled IS TRUE OR
					nunux_keeper_enabled IS TRUE OR
					espial_enabled IS TRUE OR
					readwise_enabled IS TRUE OR
					linkace_enabled IS TRUE OR
					linkding_enabled IS TRUE OR
					linktaco_enabled IS TRUE OR
					linkwarden_enabled IS TRUE OR
					apprise_enabled IS TRUE OR
					shiori_enabled IS TRUE OR
					readeck_enabled IS TRUE OR
					shaarli_enabled IS TRUE OR
					webhook_enabled IS TRUE OR
					omnivore_enabled IS TRUE OR
					karakeep_enabled IS TRUE OR
					raindrop_enabled IS TRUE OR
					betula_enabled IS TRUE OR
					cubox_enabled IS TRUE OR
					discord_enabled IS TRUE OR
					slack_enabled IS TRUE OR
					archiveorg_enabled IS TRUE
				   )
			)) AS has_save_entry,
	`
	if config.Opts.PollingParsingErrorLimit() == 0 {
		// zero means unlimited amount of accepted errors
		query += `(SELECT $2) AS count_error_feeds`
	} else {
		query += `(SELECT count(*)
			   FROM feeds
			  WHERE user_id = $1
			    AND parsing_error_count >= $2
			) AS count_error_feeds
			 `
	}

	var countUnread, countErrorFeeds int
	var hasSaveEntry bool

	err := s.db.QueryRow(query, userID, config.Opts.PollingParsingErrorLimit()).Scan(
		&countUnread,
		&hasSaveEntry,
		&countErrorFeeds,
	)
	if err != nil {
		slog.Error("Unable to fetch navigation metadata",
			slog.Int64("user_id", userID),
			slog.Any("error", err),
		)
		return NavMetadata{}, err
	}

	return NavMetadata{
		CountUnread:     countUnread,
		CountErrorFeeds: countErrorFeeds,
		HasSaveEntry:    hasSaveEntry,
	}, nil
}
