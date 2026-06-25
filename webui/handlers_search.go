package webui

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

// hexRootRe matches a 0x-prefixed 32-byte (64 hex char) block root.
var hexRootRe = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// handleSearch backs the header search form (action="/search", "q" param). It
// resolves the query to a concrete destination and 302-redirects:
//   - all digits        → /slot/{q}     (slot number)
//   - 0x + 64 hex chars  → /slot/{q}     (block root; handleSlotDetail resolves it)
//   - anything else      → /slots        (no lean-searchable match for the query)
//
// The lean explorer is only searchable by slot number and block root, so other
// entity categories (epochs, validators by name, graffiti, addresses, …) have no
// resolvable target and fall back to the slots list.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	switch {
	case q == "":
		http.Redirect(w, r, "/slots", http.StatusFound)
	case isAllDigits(q):
		http.Redirect(w, r, "/slot/"+q, http.StatusFound)
	case hexRootRe.MatchString(q):
		http.Redirect(w, r, "/slot/"+strings.ToLower(q), http.StatusFound)
	default:
		http.Redirect(w, r, "/slots", http.StatusFound)
	}
}

// searchSlotResult is one entry in the /search/slots typeahead response. The
// fields match what static/js/explorer.js reads off each suggestion object:
// `slot`, `root` (the display value) and `orphaned`.
type searchSlotResult struct {
	Slot     uint64 `json:"slot"`
	Root     string `json:"root"`
	Orphaned bool   `json:"orphaned"`
}

// handleSearchSlots backs the /search/slots typeahead. It returns a JSON array
// (the shape Bloodhound's `remote` expects). A numeric query resolves to the
// matching slot when one exists; everything else returns an empty array.
func (s *Server) handleSearchSlots(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results := []searchSlotResult{}
	if n, err := strconv.ParseUint(q, 10, 64); err == nil {
		if slots, gerr := db.GetSlotsByRange(ctx, n, n, 2); gerr == nil {
			for _, sl := range slots {
				results = append(results, searchSlotResult{
					Slot:     sl.Slot,
					Root:     fullHex(sl.Root),
					Orphaned: sl.Status == dbtypes.Orphaned,
				})
			}
		} else {
			s.logger.WithError(gerr).Warn("search: failed to query slots by range")
		}
	}
	writeJSON(w, results)
}

// handleSearchEmpty returns an empty typeahead result set (HTTP 200, JSON `[]`).
// It backs the search categories that have no lean equivalent (epochs, graffiti,
// validator names, addresses, transactions, exec blocks, validators). Returning
// an empty 200 stops explorer.js from emitting 404s on every keystroke.
func (s *Server) handleSearchEmpty(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, []struct{}{})
}

// isAllDigits reports whether s is non-empty and contains only ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
