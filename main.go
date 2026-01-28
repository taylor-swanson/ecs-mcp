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
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/gorilla/handlers"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/taylor-swanson/ecs-mcp/internal/ecs"
	"github.com/taylor-swanson/ecs-mcp/internal/store"

	_ "modernc.org/sqlite"
)

var (
	dbFile      string
	ecsDir      string
	listen      string
	showVersion bool
	enableDebug bool
	pretty      bool
)

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
	getStringEnv("ECS_MCP_ECS_DIR", &ecsDir)
	getStringEnv("ECS_MCP_LISTEN", &listen)
	getBoolEnv("ECS_MCP_DEBUG", &enableDebug)
}

func parseArgs() {
	flag.StringVar(&dbFile, "db", "ecs-mcp.db", "path to database file")
	flag.StringVar(&ecsDir, "dir", "", "path to the ECS repository")
	flag.StringVar(&listen, "listen", "", "listen for HTTP requests on this address, intead of stdin/stdout")
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.BoolVar(&enableDebug, "debug", false, "enable debug logging")
	flag.BoolVar(&pretty, "pretty", false, "enable pretty logging")

	flag.Parse()
}

func runServer() (err error) {
	modVer, vcsRef := getVersion()
	slog.Info("Starting ecs-mcp", slog.String("version", modVer), slog.String("commit", vcsRef))

	// Load ECS fields.
	fields, err := loadFields(ecsDir)
	if err != nil {
		return fmt.Errorf("unable to load ECS fields: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Create DB.
	db, err := store.NewDB(ctx, dbFile, fields)
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, db.Close())
	}()

	// Create MCP server.

	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    "ecs-mcp",
		Version: modVer + "(" + vcsRef + ")",
	}, nil)
	ecs.AddTools(mcpSrv, store.DDL, db)

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
		srvURL = "http://" + srvURL

		slog.Info("Starting server", slog.String("listen", httpSrv.Addr), slog.String("url", srvURL))
		err = httpSrv.ListenAndServe()

		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		} else if err != nil {
			close(doneCh)
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
	if ecsDir == "" {
		_, _ = fmt.Fprintln(os.Stderr, "ECS directory is required")
		os.Exit(1)
	}

	level := slog.LevelInfo
	if enableDebug {
		level = slog.LevelDebug
	}
	var logHandler slog.Handler
	if pretty {
		logHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		logHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(logHandler))

	if err := runServer(); err != nil {
		slog.Error("Failed to run", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func getVersion() (modVer string, vcsRef string) {
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

func loadFields(dir string) (map[string]ecs.Field, error) {
	fields := map[string]ecs.Field{}

	ecsFlatFile := filepath.Join(dir, "generated/ecs/ecs_flat.yml")
	ecsFlat, err := os.ReadFile(ecsFlatFile)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(ecsFlat, &fields)
	if err != nil {
		return nil, err
	}

	slog.Info("Read fields file", slog.String("file", ecsFlatFile), slog.Int("count", len(fields)))

	return fields, nil
}
