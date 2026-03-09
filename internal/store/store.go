// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package store implements database operations for the ECS MCP server.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/taylor-swanson/ecs-mcp/internal/field"

	_ "embed"
)

//go:generate sqlc generate -f sql/sqlc.yml

//go:embed sql/schema.sql
var DDL string

func NewDB(ctx context.Context, dbFile string, fields []*field.Field) (*sql.DB, error) {
	// Remove existing DB if it exists. Create a new DB.
	datasource := fmt.Sprintf("file:%s", dbFile)
	if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to remove existing DB: %w", err)
	}
	db, err := sql.Open("sqlite", datasource)
	if err != nil {
		return nil, fmt.Errorf("failed to open new DB: %w", err)
	}

	// Create tables.
	if _, err := db.ExecContext(ctx, DDL); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Write fields to DB.
	q := New(db)
	var added int
	for _, v := range fields {
		if _, err = q.InsertField(ctx, InsertFieldParams{
			Name:        v.FlatName,
			DashedName:  v.DashedName,
			Type:        v.Type,
			Level:       v.Level,
			Short:       nullString(v.Short),
			Description: nullString(v.Description),
			Example:     nullString(v.Example),
		}); err != nil {
			slog.Error("unable to insert field", slog.String("field", v.FlatName), slog.String("error", err.Error()))
		} else {
			slog.Debug("Added field to DB", slog.String("field", v.FlatName))
			added++
		}
	}

	slog.Info("Added fields to DB", slog.Int("count", added))

	// Open DB as read-only.
	datasource = fmt.Sprintf("file:%s?mode=ro", dbFile)
	db, err = sql.Open("sqlite", datasource)
	if err != nil {
		return nil, fmt.Errorf("failed to open read-only DB: %w", err)
	}

	return db, nil
}

// nullString converts a string to a sql.NullString. A string is null if it is empty.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: s, Valid: true}
}
