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

package ecs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExecuteQueryArgs is the arguments for the executeQuery tool.
type ExecuteQueryArgs struct {
	Statement string `json:"statement" jsonschema:"SQLite query to execute"`
}

func mcpErrorf(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("ERROR: "+format, args...),
			},
		},
	}
}

type tools struct {
	ddl string
	db  *sql.DB
}

func (t *tools) getSQLTables(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: t.ddl},
		},
	}, nil, nil
}

func (t *tools) executeQuery(ctx context.Context, request *mcp.CallToolRequest, args ExecuteQueryArgs) (*mcp.CallToolResult, any, error) {
	slog.InfoContext(ctx, "Executing query", slog.String("statement", args.Statement))

	rows, err := t.db.QueryContext(ctx, args.Statement)
	if err != nil {
		slog.ErrorContext(ctx, "Error executing query", slog.String("statement", args.Statement), slog.String("error", err.Error()))
		return mcpErrorf("failed to execute query: %v", err), nil, nil
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		slog.ErrorContext(ctx, "Error getting columns", slog.String("error", err.Error()))
		return mcpErrorf("failed to get columns: %v", err), nil, nil
	}

	var result []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}

		if err = rows.Scan(pointers...); err != nil {
			slog.ErrorContext(ctx, "Error scanning row", slog.String("error", err.Error()))
			return mcpErrorf("failed to scan row: %v", err), nil, nil
		}

		row := make(map[string]any)
		for i, column := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[column] = string(b)
			} else {
				row[column] = val
			}
		}
		result = append(result, row)
	}

	jsonRows, err := json.Marshal(result)
	if err != nil {
		slog.ErrorContext(ctx, "Error marshaling results", slog.String("error", err.Error()))
		return mcpErrorf("failed to marshal result: %v", err), nil, nil
	}

	slog.InfoContext(ctx, "Query executed successfully", slog.Int("row_count", len(result)))
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonRows)},
		},
	}, nil, nil
}

// AddTools adds the ECS tools to the MCP server.
func AddTools(s *mcp.Server, ddl string, db *sql.DB) {
	t := &tools{
		ddl: ddl,
		db:  db,
	}

	mcp.AddTool(s, &mcp.Tool{
		Annotations: &mcp.ToolAnnotations{
			IdempotentHint: true,
			ReadOnlyHint:   true,
		},
		Description: "Call this tool first. Returns the complete catalog of available tables and columns",
		Name:        "ecs_get_sql_tables",
		Title:       "Get ECS SQL tables",
	}, t.getSQLTables)

	mcp.AddTool(s, &mcp.Tool{
		Annotations: &mcp.ToolAnnotations{
			IdempotentHint: true,
			ReadOnlyHint:   true,
		},
		Description: "Call this tool to execute an arbitrary SQLite query. Be sure you have called ecs_get_sql_tables() first to understand the structure of the data.",
		Name:        "ecs_execute_query",
		Title:       "Execute SQL query",
	}, t.executeQuery)
}
