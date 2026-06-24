// Command lean-dora is the lean-consensus block explorer. It indexes an
// ethlambda node's /lean/v0 API into a SQLite (or Postgres) database and serves
// a minimal web UI.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
	"github.com/ethpandaops/dora/db"
	leanindexer "github.com/ethpandaops/dora/indexer/lean"
	"github.com/ethpandaops/dora/types"
	"github.com/ethpandaops/dora/webui"
)

func main() {
	endpoint := flag.String("consensus-endpoint", "http://127.0.0.1:5052", "ethlambda /lean/v0 base URL")
	listen := flag.String("listen", ":8080", "HTTP listen address for the explorer UI")
	sqliteFile := flag.String("sqlite", "lean-dora.sqlite", "SQLite database file")
	pgsqlDSN := flag.String("pgsql-host", "", "Postgres host (if set, uses Postgres instead of SQLite)")
	pgsqlUser := flag.String("pgsql-user", "postgres", "Postgres user")
	pgsqlPass := flag.String("pgsql-pass", "", "Postgres password")
	pgsqlName := flag.String("pgsql-name", "leandora", "Postgres database name")
	pgsqlPort := flag.String("pgsql-port", "5432", "Postgres port")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	flag.Parse()

	logger := logrus.New()
	if lvl, err := logrus.ParseLevel(*logLevel); err == nil {
		logger.SetLevel(lvl)
	}
	log := logger.WithField("module", "lean-dora")

	// Initialize the database (lean schema).
	dbcfg := &types.DatabaseConfig{Engine: "sqlite", Sqlite: &types.SqliteDatabaseConfig{File: *sqliteFile}}
	if *pgsqlDSN != "" {
		dbcfg = &types.DatabaseConfig{
			Engine: "pgsql",
			Pgsql: &types.PgsqlDatabaseConfig{
				Username: *pgsqlUser, Password: *pgsqlPass, Name: *pgsqlName,
				Host: *pgsqlDSN, Port: *pgsqlPort,
			},
		}
	}
	db.MustInitDB(dbcfg)
	defer db.MustCloseDB()
	if err := db.ApplyEmbeddedDbSchema(-2); err != nil {
		log.WithError(err).Fatal("failed to apply db schema")
	}
	log.Info("database ready")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := leanapi.NewClient(*endpoint, nil)
	indexer := leanindexer.NewIndexer(ctx, logger.WithField("module", "indexer"), client)

	// Start the indexer in the background. It blocks on the SSE loop.
	go func() {
		if err := indexer.Start(); err != nil && ctx.Err() == nil {
			log.WithError(err).Error("indexer stopped with error")
		}
	}()

	// Give the indexer a moment to fetch the chain spec for UI display.
	time.Sleep(500 * time.Millisecond)

	server := webui.NewServer(logger.WithField("module", "web"), indexer, client, indexer.Spec(), *endpoint)
	httpServer := &http.Server{
		Addr:    *listen,
		Handler: server.Routes(),
	}

	go func() {
		log.WithField("addr", *listen).Info("explorer UI listening")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("http server error")
		}
	}()

	// Wait for a shutdown signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)
	cancel()
}
