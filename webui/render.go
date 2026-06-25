package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	doratemplates "github.com/ethpandaops/dora/templates"
	"github.com/ethpandaops/dora/types"
	"github.com/ethpandaops/dora/utils"
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

// version is surfaced in the footer ("Powered by ... | {{ .Version }}").
const version = "lean-dora"

// pageMeta carries the per-page metadata the Dora layout expects.
type pageMeta struct {
	Title       string
	Description string
	Domain      string
	Path        string
}

// pageRoot wraps a per-page data struct with the fields Dora's real layout,
// header and footer templates read off the root (".") — not off .Data. The
// header reads ExplorerLogo/Title/Subtitle, IsReady, MainMenuItems, ApiEnabled
// and ExecutionIndexerEnabled; the footer reads Version. Populating these lets
// Dora's real chrome execute with no nil-map/nil-pointer panics.
type pageRoot struct {
	BuildTime  string
	ServerTime string
	Meta       pageMeta
	Data       any

	// Header/footer fields (root-scoped).
	Version                 string
	ExplorerLogo            string
	ExplorerTitle           string
	ExplorerSubtitle        string
	IsReady                 bool
	ApiEnabled              bool
	ExecutionIndexerEnabled bool
	MainMenuItems           []mainMenuItem
}

// mainMenuItem / navigationGroup / navigationLink mirror Dora's navbar model
// (types/models.MainMenuItem et al.) with the exact field names the real
// header template reads.
type mainMenuItem struct {
	Label    string
	Path     string
	IsActive bool
	Groups   []navigationGroup
}

type navigationGroup struct {
	Label string
	Links []navigationLink
}

type navigationLink struct {
	Label         string
	Path          string
	CustomIcon    string
	Icon          string
	IsHidden      bool
	IsHighlighted bool
}

// leanMainMenu returns the navbar layout for the lean explorer. It mirrors the
// shape of Dora's eth navbar (Blockchain / Validators / Clients dropdowns) so
// the chrome looks identical, but the links point at lean routes. active marks
// the dropdown that contains the current page so it highlights.
func leanMainMenu() []mainMenuItem {
	return []mainMenuItem{
		{
			Label: "Blockchain",
			Groups: []navigationGroup{{
				Links: []navigationLink{
					{Label: "Epochs", Path: "/epochs", Icon: "fa-history"},
					{Label: "Slots", Path: "/slots", Icon: "fa-cube"},
					{Label: "Finality", Path: "/finality", Icon: "fa-check-double"},
				},
			}},
		},
		{
			Label: "Validators",
			Groups: []navigationGroup{{
				Links: []navigationLink{
					{Label: "Validators", Path: "/validators", Icon: "fa-users"},
				},
			}},
		},
		{
			Label: "Network",
			Groups: []navigationGroup{{
				Links: []navigationLink{
					{Label: "Clients", Path: "/clients", Icon: "fa-server"},
					{Label: "Forks", Path: "/forks", Icon: "fa-code-fork"},
					{Label: "Chain Forks", Path: "/chain_forks", Icon: "fa-project-diagram"},
					{Label: "Fork Choice", Path: "/forkchoice", Icon: "fa-code-branch"},
				},
			}},
		},
	}
}

// renderer compiles and caches the explorer page templates. The homepage
// ("dashboard") is assembled from Dora's real layout + header + footer + svg
// chrome plus the real templates/index/*.html files, so it is pixel-identical
// to Dora. The remaining lean pages keep the hand-rolled lean chrome
// (_lean.html) for now; both are valid "layout" templates in separate cache
// entries.
type renderer struct {
	cache map[string]*template.Template
}

func newRenderer() (*renderer, error) {
	// Several of Dora's real template helpers (ethBlockHashLink, ethBlockLink,
	// formatEthAddressFullLink, …) dereference the package-global utils.Config.
	// lean-dora never loads a Dora config, so seed a zero-value one if unset:
	// ExecutionIndexer.Enabled == false and an empty EthExplorerLink make those
	// helpers degrade to plain (greyed) text instead of nil-panicking. This is
	// idempotent and safe to run from both the server and tests.
	if utils.Config == nil {
		utils.Config = &types.Config{}
	}

	r := &renderer{cache: make(map[string]*template.Template)}

	// Funcs: start from Dora's full helper set (every helper the index templates
	// call) and merge in the small lean-only helpers used by the lean pages.
	funcs := utils.GetTemplateFuncs()
	for name, fn := range funcMap {
		funcs[name] = fn
	}

	// Reuse Dora's real layout chrome from the templates package embed.
	layoutSrc, err := fs.ReadFile(doratemplates.Files, "_layout/layout.html")
	if err != nil {
		return nil, fmt.Errorf("read dora layout: %w", err)
	}
	chromeSrc, err := leanTemplates.ReadFile("templates/lean/_lean.html")
	if err != nil {
		return nil, fmt.Errorf("read lean chrome: %w", err)
	}

	// --- Homepage on Dora's REAL chrome + REAL index templates. ---
	if err := r.registerDora(funcs, layoutSrc, "dashboard",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"index/index.html",
		"index/networkOverview.html",
		"index/recentEpochs.html",
		"index/recentBlocks.html",
		"index/recentSlots.html",
	); err != nil {
		return nil, err
	}

	// --- Slots list + slot detail on Dora's REAL chrome + REAL templates. ---
	// The slots list pulls in _svg/professor.html for the empty-state graphic.
	if err := r.registerDora(funcs, layoutSrc, "slots",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"_svg/professor.html",
		"slots/slots.html",
	); err != nil {
		return nil, err
	}
	// The slot detail page is assembled from slot.html (shell + tab list) plus
	// the overview and attestations sub-templates. The eth-only sub-tabs are
	// gated on *Count fields that lean always leaves at 0, so their sub-templates
	// (block_transactions, block_deposits, …) are never invoked and need not be
	// parsed in.
	if err := r.registerDora(funcs, layoutSrc, "slot",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"slot/slot.html",
		"slot/overview.html",
		"slot/attestations.html",
	); err != nil {
		return nil, err
	}
	// slot.html references the eth-only block_* sub-templates inside branches
	// that lean never takes; html/template still resolves those references at
	// parse time, so parse in empty stubs for them.
	stubSrc, err := leanTemplates.ReadFile("templates/lean/_slot_stubs.html")
	if err != nil {
		return nil, fmt.Errorf("read slot stubs: %w", err)
	}
	if _, err := r.cache["slot"].Parse(string(stubSrc)); err != nil {
		return nil, fmt.Errorf("parse slot stubs: %w", err)
	}
	// The slot "not found" page redefines "page"/"js"/"css", so it cannot share a
	// cache entry with slot.html; register it on its own.
	if err := r.registerDora(funcs, layoutSrc, "slotnotfound",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"slot/notfound.html",
	); err != nil {
		return nil, err
	}

	// --- Validators list on Dora's REAL chrome + REAL template. ---
	// validators.html uses _svg/professor.html for the empty-state graphic; it
	// references no eth-only sub-templates, so no stub file is needed.
	if err := r.registerDora(funcs, layoutSrc, "validators",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"_svg/professor.html",
		"validators/validators.html",
	); err != nil {
		return nil, err
	}

	// --- Consensus clients on Dora's REAL chrome + REAL template. ---
	// clients_cl.html references no eth-only sub-templates (its js/css are
	// self-contained define blocks), so no stub file is needed.
	if err := r.registerDora(funcs, layoutSrc, "clients",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"clients/clients_cl.html",
	); err != nil {
		return nil, err
	}

	// --- Forks on Dora's REAL chrome + REAL template. ---
	// forks.html carries its own inline "fork_client_cols" sub-template and
	// self-contained js/css, so no stub file is needed.
	if err := r.registerDora(funcs, layoutSrc, "forks",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"forks/forks.html",
	); err != nil {
		return nil, err
	}
	// chain_forks.html is the cytoscape-graph page. It reads only .ChainSpecs;
	// the diagram itself is drawn by static/js/chain-forks-diagram.js which
	// AJAX-fetches a Dora epoch/slot data endpoint that lean does not serve, so
	// the diagram area stays empty. We render the real page shell for chrome
	// parity.
	if err := r.registerDora(funcs, layoutSrc, "chain_forks",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"chain_forks/chain_forks.html",
	); err != nil {
		return nil, err
	}

	// --- Epochs list + epoch detail on Dora's REAL chrome + REAL templates. ---
	// Lean has no epochs: these render with real chrome but an empty/zeroed body.
	// epochs.html uses _svg/professor.html for its empty-state graphic.
	if err := r.registerDora(funcs, layoutSrc, "epochs",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"_svg/professor.html",
		"epochs/epochs.html",
	); err != nil {
		return nil, err
	}
	if err := r.registerDora(funcs, layoutSrc, "epoch",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"epoch/epoch.html",
	); err != nil {
		return nil, err
	}
	// The epoch "not found" page redefines page/js/css, so register it on its own.
	if err := r.registerDora(funcs, layoutSrc, "epochnotfound",
		"_layout/header.html",
		"_layout/footer.html",
		"_svg/timeline.html",
		"epoch/notfound.html",
	); err != nil {
		return nil, err
	}

	// --- Remaining lean pages on the hand-rolled lean chrome. ---
	leanPages := map[string]string{
		"finality":   "templates/lean/finality.html",
		"forkchoice": "templates/lean/forkchoice.html",
	}
	for name, path := range leanPages {
		pageSrc, err := leanTemplates.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read page %s: %w", name, err)
		}
		t := template.New(name).Funcs(funcs)
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

// registerDora parses Dora's real layout plus the given Dora template files
// (paths relative to the templates package embed) into one cache entry.
func (r *renderer) registerDora(funcs template.FuncMap, layoutSrc []byte, name string, files ...string) error {
	t := template.New(name).Funcs(funcs)
	if _, err := t.Parse(string(layoutSrc)); err != nil {
		return fmt.Errorf("parse layout for %s: %w", name, err)
	}
	for _, f := range files {
		src, err := fs.ReadFile(doratemplates.Files, f)
		if err != nil {
			return fmt.Errorf("read dora template %s: %w", f, err)
		}
		if _, err := t.Parse(string(src)); err != nil {
			return fmt.Errorf("parse dora template %s for %s: %w", f, name, err)
		}
	}
	r.cache[name] = t
	return nil
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

		Version:       version,
		ExplorerTitle: "lean-dora",
		IsReady:       true,
		MainMenuItems: leanMainMenu(),
	}
	// Mark the active nav dropdown by path prefix.
	for i := range root.MainMenuItems {
		for _, g := range root.MainMenuItems[i].Groups {
			for _, l := range g.Links {
				if l.Path == path {
					root.MainMenuItems[i].IsActive = true
				}
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", root); err != nil {
		// Headers may already be written; log-friendly fallback.
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
