// Package webui serves the minimal lean-consensus explorer UI. It is a
// deliberately small, self-contained replacement for Dora's eth page/template
// framework: a handful of server-rendered pages backed by the lean DB schema
// plus a live-proxied fork-choice view.
package webui

import (
	"context"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
	leanindexer "github.com/ethpandaops/dora/indexer/lean"
	"github.com/ethpandaops/dora/static"
)

// Server renders the lean explorer pages.
type Server struct {
	logger       logrus.FieldLogger
	indexer      *leanindexer.Indexer
	client       leanapi.ConsensusRPCClient
	spec         *leanapi.ChainSpec
	nodeEndpoint string
	renderer     *renderer
}

// NewServer builds a web server bound to the given indexer and client.
// nodeEndpoint is the consensus node's base URL, used to embed its live
// fork-choice visualization.
func NewServer(logger logrus.FieldLogger, indexer *leanindexer.Indexer, client leanapi.ConsensusRPCClient, spec *leanapi.ChainSpec, nodeEndpoint string) *Server {
	r, err := newRenderer()
	if err != nil {
		// Template parse errors are programmer bugs (embedded templates); fail loud.
		logger.WithError(err).Fatal("failed to compile web templates")
	}
	return &Server{logger: logger, indexer: indexer, client: client, spec: spec, nodeEndpoint: nodeEndpoint, renderer: r}
}

// Routes returns the HTTP handler with all explorer routes registered.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Serve Dora's embedded static assets. The embed.FS is rooted at the static/
	// package dir, so its internal paths are css/..., js/..., webfonts/..., img/...,
	// favicon.ico and robots.txt. http.FileServerFS serves them at the root, which
	// is exactly what the layout references (/css/..., /favicon.ico, ...).
	fileServer := http.FileServerFS(static.Files)
	for _, prefix := range []string{"/css/", "/js/", "/webfonts/", "/img/", "/images/"} {
		mux.Handle(prefix, fileServer)
	}
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) { fileServer.ServeHTTP(w, r) })
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) { fileServer.ServeHTTP(w, r) })

	mux.HandleFunc("/slots", s.handleSlots)
	mux.HandleFunc("/slot/", s.handleSlotDetail)
	mux.HandleFunc("/finality", s.handleFinality)
	mux.HandleFunc("/validators", s.handleValidators)
	mux.HandleFunc("/forkchoice", s.handleForkChoice)
	mux.HandleFunc("/api/forkchoice", s.handleForkChoiceJSON)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	// "/" is the catch-all; serve the dashboard only for the exact root path.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.handleDashboard(w, r)
	})
	return logRequests(s.logger, mux)
}

func logRequests(logger logrus.FieldLogger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.WithFields(logrus.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start),
		}).Debug("served request")
	})
}

func (s *Server) reqCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 10*time.Second)
}
