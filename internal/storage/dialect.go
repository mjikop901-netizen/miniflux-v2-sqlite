// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

func isSQLite(db *sql.DB) bool {
	var version string
	return db.QueryRow(`SELECT sqlite_version()`).Scan(&version) == nil
}

func placeholders(first, count int) string {
	values := make([]string, count)
	for i := range values {
		values[i] = "$" + strconv.Itoa(first+i)
	}
	return strings.Join(values, ", ")
}

func appendInt64s(args []any, values []int64) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func appendStrings(args []any, values []string) []any {
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func postgresTags(tags []string) any {
	return pq.Array(tags)
}

func (s *Storage) encodeTags(tags []string) (any, error) {
	if !s.sqlite {
		return pq.Array(tags), nil
	}

	encoded, err := json.Marshal(tags)
	if err != nil {
		return nil, fmt.Errorf("encode tags: %w", err)
	}
	return string(encoded), nil
}

type tagScanner struct {
	tags   *[]string
	sqlite bool
}

func (s tagScanner) Scan(src any) error {
	if !s.sqlite {
		return pq.Array(s.tags).Scan(src)
	}

	if src == nil {
		*s.tags = nil
		return nil
	}

	var raw []byte
	switch value := src.(type) {
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		return fmt.Errorf("scan tags: unsupported value type %T", src)
	}

	return json.Unmarshal(raw, s.tags)
}

type sqliteSearch struct {
	match      string
	exclusions []string
}

func sqliteSearchQuery(query string) (sqliteSearch, bool) {
	terms := sqliteSearchTerms(query)
	if len(terms) == 0 {
		return sqliteSearch{}, false
	}

	var expression strings.Builder
	hasMatch := false
	pendingOR := false
	search := sqliteSearch{}
	for _, term := range terms {
		if strings.EqualFold(term, "OR") {
			if hasMatch {
				pendingOR = true
			}
			continue
		}

		if strings.HasPrefix(term, "-") {
			term = strings.TrimPrefix(term, "-")
			if term != "" {
				search.exclusions = append(search.exclusions, term)
			}
			continue
		}
		if term == "" {
			continue
		}

		if hasMatch {
			if pendingOR {
				expression.WriteString(" OR ")
			} else {
				expression.WriteString(" AND ")
			}
		}
		expression.WriteString(sqliteFTSTerm(term))
		hasMatch = true
		pendingOR = false
	}

	search.match = expression.String()
	return search, search.match != "" || len(search.exclusions) > 0
}

func sqliteFTSTerm(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

func sqliteSearchTerms(query string) []string {
	var terms []string
	var term strings.Builder
	quoted := false

	flush := func() {
		if term.Len() > 0 {
			terms = append(terms, term.String())
			term.Reset()
		}
	}

	for _, runeValue := range query {
		switch {
		case runeValue == '"':
			quoted = !quoted
		case !quoted && (runeValue == ' ' || runeValue == '\t' || runeValue == '\n'):
			flush()
		default:
			term.WriteRune(runeValue)
		}
	}
	flush()
	return terms
}
