package webui

import (
	"encoding/hex"
	"html/template"

	"github.com/ethpandaops/dora/dbtypes"
)

// shortHex renders the first 4 bytes of a root as 0x-prefixed hex (8 chars).
func shortHex(b []byte) string {
	if len(b) == 0 {
		return "—"
	}
	n := 4
	if len(b) < n {
		n = len(b)
	}
	return "0x" + hex.EncodeToString(b[:n])
}

// fullHex renders a byte slice as a 0x-prefixed hex string.
func fullHex(b []byte) string {
	if len(b) == 0 {
		return "—"
	}
	return "0x" + hex.EncodeToString(b)
}

// statusLabel maps a slot status (0=missed,1=proposed,2=orphaned) to a label.
func statusLabel(s dbtypes.SlotStatus) string {
	switch s {
	case dbtypes.Missing:
		return "missed"
	case dbtypes.Canonical:
		return "proposed"
	case dbtypes.Orphaned:
		return "orphaned"
	default:
		return "unknown"
	}
}

// statusClass maps a slot status to a coarse css class (ok/warn/muted) that the
// page templates translate into Bootstrap badge variants.
func statusClass(s dbtypes.SlotStatus) string {
	switch s {
	case dbtypes.Canonical:
		return "ok"
	case dbtypes.Orphaned:
		return "warn"
	default:
		return "muted"
	}
}

// funcMap holds the template helpers shared by every lean page.
var funcMap = template.FuncMap{
	"shortHex":    shortHex,
	"fullHex":     fullHex,
	"statusLabel": statusLabel,
	"statusClass": statusClass,
	// sub is used by Dora's shared layout/header markup.
	"sub": func(i, j int) int { return i - j },
}
