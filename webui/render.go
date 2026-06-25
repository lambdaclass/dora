package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	doratemplates "github.com/ethpandaops/dora/templates"
)

// leanTemplates holds the lean page templates plus the shared lean chrome
// (_lean.html, which (re)defines header/footer/css/js). They are embedded so the
// binary is self-contained and never reads templates from disk at runtime.
//
//go:embed templates/lean/*.html
var leanTemplates embed.FS

// buildTime is stamped once at process start. It is used as a cache-busting
// query param on the layout's css/js includes and surfaced via <meta server-time>.
// We intentionally avoid time.Now() at template-parse time.
var buildTime = time.Now().UTC().Format("20060102150405")

// pageMeta carries the per-page metadata the Dora layout expects.
type pageMeta struct {
	Title       string
	Description string
	Domain      string
	Path        string
}

// pageRoot wraps a per-page data struct with the fields Dora's _layout template
// (and our lean header/footer) read off the root.
type pageRoot struct {
	BuildTime  string
	ServerTime string
	Meta       pageMeta
	Data       any
}

// renderer compiles and caches the lean page templates. Each cache entry is the
// Dora layout + lean chrome + one page template, executable as the "layout"
// template against a pageRoot.
type renderer struct {
	cache map[string]*template.Template
}

func newRenderer() (*renderer, error) {
	r := &renderer{cache: make(map[string]*template.Template)}

	// Reuse Dora's real layout chrome from the templates package embed.
	layoutSrc, err := fs.ReadFile(doratemplates.Files, "_layout/layout.html")
	if err != nil {
		return nil, fmt.Errorf("read dora layout: %w", err)
	}
	chromeSrc, err := leanTemplates.ReadFile("templates/lean/_lean.html")
	if err != nil {
		return nil, fmt.Errorf("read lean chrome: %w", err)
	}

	pages := map[string]string{
		"dashboard":  "templates/lean/dashboard.html",
		"slots":      "templates/lean/slots.html",
		"slot":       "templates/lean/slot.html",
		"finality":   "templates/lean/finality.html",
		"validators": "templates/lean/validators.html",
		"forkchoice": "templates/lean/forkchoice.html",
	}

	for name, path := range pages {
		pageSrc, err := leanTemplates.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read page %s: %w", name, err)
		}
		t := template.New(name).Funcs(funcMap)
		// Order matters only in that all needed defines must be present; the page
		// "page"/"css"/"js" defines come from pageSrc, chrome supplies the rest.
		if _, err := t.Parse(string(layoutSrc)); err != nil {
			return nil, fmt.Errorf("parse layout for %s: %w", name, err)
		}
		if _, err := t.Parse(string(chromeSrc)); err != nil {
			return nil, fmt.Errorf("parse chrome for %s: %w", name, err)
		}
		if _, err := t.Parse(string(pageSrc)); err != nil {
			return nil, fmt.Errorf("parse page %s: %w", name, err)
		}
		r.cache[name] = t
	}
	return r, nil
}

// render executes the named page template, wrapping data in a pageRoot.
func (r *renderer) render(w http.ResponseWriter, name, title, path string, data any) {
	t, ok := r.cache[name]
	if !ok {
		http.Error(w, "unknown page: "+name, http.StatusInternalServerError)
		return
	}
	root := pageRoot{
		BuildTime:  buildTime,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
		Meta: pageMeta{
			Title:       title,
			Description: "lean consensus block explorer",
			Domain:      "lean-dora",
			Path:        path,
		},
		Data: data,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", root); err != nil {
		// Headers may already be written; log-friendly fallback.
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
