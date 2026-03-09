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

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/gorilla/handlers"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/taylor-swanson/ecs-mcp/internal/field"
	"github.com/taylor-swanson/ecs-mcp/internal/store"

	_ "modernc.org/sqlite"
)

var (
	dbFile      string
	gitRef      string
	localFile   string
	listen      string
	certFile    string
	keyFile     string
	insecure    bool
	enableDebug bool
	showVersion bool
)

type mcpExecQueryArgs struct {
	Statement string `json:"statement" jsonschema:"SQLite query to execute"`
}

type mcpTools struct {
	ddl string
	db  *sql.DB
}

func (t *mcpTools) getSQLSchema(_ context.Context, _ *mcp.CallToolRequest, _ any) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: t.ddl},
		},
	}, nil, nil
}

func (t *mcpTools) executeQuery(ctx context.Context, _ *mcp.CallToolRequest, args mcpExecQueryArgs) (*mcp.CallToolResult, any, error) {
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

func addMCPTools(s *mcp.Server, ddl string, db *sql.DB) {
	t := &mcpTools{
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
	}, t.getSQLSchema)

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

func mcpErrorf(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("ERROR: "+format, args...),
			},
		},
	}
}

func getFields() ([]*field.Field, error) {
	var raw []byte
	var err error

	if localFile != "" {
		slog.Debug("Fetching local ecs definition file", slog.String("path", localFile))

		raw, err = os.ReadFile(localFile)
	} else {
		var u *url.URL
		if u, err = url.Parse("https://raw.githubusercontent.com/elastic/ecs/" + gitRef + "/generated/ecs/ecs_flat.yml"); err != nil {
			return nil, err
		}

		slog.Debug("Fetching remote ecs definition file", slog.String("url", u.String()))

		var res *http.Response
		if res, err = http.Get(u.String()); err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("failed to fetch remote ecs file: status %s", res.Status)
		}

		raw, err = io.ReadAll(res.Body)
		res.Body.Close()
	}
	if err != nil {
		return nil, err
	}

	var fieldDefs map[string]*field.Field
	if err = yaml.Unmarshal(raw, &fieldDefs); err != nil {
		return nil, err
	}

	fields := make([]*field.Field, 0, len(fieldDefs))
	for k := range fieldDefs {
		fields = append(fields, fieldDefs[k])
	}
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Name < fields[j].Name
	})

	return fields, nil
}

func parseArgs() {
	flag.StringVar(&dbFile, "db", "ecs-mcp.db", "path to database file")
	flag.StringVar(&localFile, "ecs-file", "", "path to the ECS ecs_flat.yml file (when omitted, fetches file from GitHub)")
	flag.StringVar(&gitRef, "git-ref", "main", "when fetching from GitHub, which git ref to use")
	flag.StringVar(&listen, "listen", "", "listen for HTTP requests on this address, instead of stdin/stdout")
	flag.StringVar(&certFile, "cert", "cert.pem", "path to TLS certificate file")
	flag.StringVar(&keyFile, "key", "key.pem", "path to TLS private key file")
	flag.BoolVar(&insecure, "insecure", false, "disable TLS")
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.BoolVar(&enableDebug, "debug", false, "enable debug logging")

	flag.Parse()
}

func getStringEnv(key string, target *string) {
	if value, ok := os.LookupEnv(key); ok {
		*target = value
	}
}

func getBoolEnv(key string, target *bool) {
	if value, ok := os.LookupEnv(key); ok {
		if v, err := strconv.ParseBool(value); err == nil {
			*target = v
		} else {
			slog.Warn("Unable to parse boolean from environment variable", slog.String("env", key))
		}
	}
}

func readEnv() {
	getStringEnv("ECS_MCP_DB_FILE", &dbFile)
	getStringEnv("ECS_MCP_ECS_FILE", &localFile)
	getStringEnv("ECS_MCP_GIT_REF", &gitRef)
	getStringEnv("ECS_MCP_LISTEN", &listen)
	getStringEnv("ECS_MCP_CERT_FILE", &certFile)
	getStringEnv("ECS_MCP_KEY_FILE", &keyFile)
	getBoolEnv("ECS_MCP_INSECURE", &insecure)
	getBoolEnv("ECS_MCP_DEBUG", &enableDebug)
}

func getVersion() (modVer, vcsRef string) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}

	modVer = info.Main.Version
	vcsRef = "unknown"
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			vcsRef = setting.Value
			break
		}
	}

	return modVer, vcsRef
}

func Main() error {
	modVer, vcsRef := getVersion()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Load ECS fields.
	fields, err := getFields()
	if err != nil {
		return fmt.Errorf("unable to load ECS fields: %w", err)
	}

	// Create DB.
	db, err := store.NewDB(ctx, dbFile, fields)
	if err != nil {
		return err
	}
	defer db.Close()

	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "ecs-mcp",
		Version: modVer + "(" + vcsRef + ")",
	}, nil)
	addMCPTools(mcpSrv, store.DDL, db)

	// Run MCP server.

	if listen != "" {
		var handler http.Handler = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return mcpSrv }, nil)
		handler = handlers.CombinedLoggingHandler(os.Stderr, handler)

		httpSrv := &http.Server{
			Addr:    listen,
			Handler: handler,
		}
		doneCh := make(chan struct{})

		go func() {
			timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer timeoutCancel()

			<-ctx.Done()

			_ = httpSrv.Shutdown(timeoutCtx)
			close(doneCh)
		}()

		srvURL := listen
		if strings.HasPrefix(listen, ":") {
			srvURL = "localhost" + srvURL
		}
		if insecure {
			srvURL = "http://" + srvURL
		} else {
			srvURL = "https://" + srvURL
		}

		slog.Info("Starting server", slog.String("listen", httpSrv.Addr), slog.String("url", srvURL))

		if insecure {
			err = httpSrv.ListenAndServe()
		} else {
			err = httpSrv.ListenAndServeTLS(certFile, keyFile)
		}
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			cancel()
		}
		<-doneCh

		slog.Info("Server shut down", slog.String("listen", httpSrv.Addr))

		return err
	}

	t := &mcp.LoggingTransport{
		Transport: &mcp.StdioTransport{},
		Writer:    os.Stderr,
	}

	if err = mcpSrv.Run(ctx, t); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to run stdio server: %w", err)
	}

	return nil
}

func main() {
	parseArgs()
	readEnv()

	if showVersion {
		modVer, vcsRef := getVersion()
		_, _ = fmt.Fprintf(os.Stderr, "ecs-mcp version %s [commit %v]\n", modVer, vcsRef)
		os.Exit(0)
	}

	level := slog.LevelInfo
	if enableDebug {
		level = slog.LevelDebug
	}
	logHandler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(logHandler))

	if err := Main(); err != nil {
		slog.Error("Error running app", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
