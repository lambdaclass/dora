package webui

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func hexToBytes(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	return hex.DecodeString(s)
}

// sub clamps a-b at zero (uint64 subtraction without underflow).
func sub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}

func minVal(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
