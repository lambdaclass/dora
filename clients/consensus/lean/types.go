// Package lean implements a consensus RPC client for the lean-consensus
// (3SF-mini) protocol served by ethlambda over its /lean/v0 HTTP API.
//
// Lean consensus differs from the Ethereum beacon chain on every axis the
// explorer cares about: 4-second slots with 5 intervals (no epochs of 32),
// XMSS post-quantum signatures (opaque bytes here, never verified), no
// execution layer, no balances. The Go types below mirror the JSON shapes
// emitted by ethlambda's axum handlers (crates/net/rpc/src/*.rs) and the lean
// wire types (crates/common/types/src/*.rs).
package lean

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

// Root is a 32-byte block/state root. It marshals to/from a 0x-prefixed hex
// string, matching ethlambda's H256 serde (primitives.rs).
type Root [32]byte

// String renders the root as a 0x-prefixed lowercase hex string.
func (r Root) String() string {
	return "0x" + hex.EncodeToString(r[:])
}

// Bytes returns the raw 32 bytes.
func (r Root) Bytes() []byte {
	b := make([]byte, 32)
	copy(b, r[:])
	return b
}

// IsZero reports whether the root is all zeroes.
func (r Root) IsZero() bool {
	for _, b := range r {
		if b != 0 {
			return false
		}
	}
	return true
}

// UnmarshalJSON parses a 0x-prefixed (or bare) 32-byte hex string.
func (r *Root) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return r.fromHex(s)
}

func (r *Root) fromHex(s string) error {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("lean.Root: invalid hex: %w", err)
	}
	if len(b) != 32 {
		return fmt.Errorf("lean.Root: expected 32 bytes, got %d", len(b))
	}
	copy(r[:], b)
	return nil
}

// MarshalJSON renders the root as a 0x-prefixed hex string.
func (r Root) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// RootFromBytes builds a Root from a byte slice (must be exactly 32 bytes).
func RootFromBytes(b []byte) (Root, error) {
	var r Root
	if len(b) != 32 {
		return r, fmt.Errorf("lean.Root: expected 32 bytes, got %d", len(b))
	}
	copy(r[:], b)
	return r, nil
}

// Checkpoint mirrors ethlambda's Checkpoint (checkpoint.rs): a (root, slot)
// pair. The node serializes slot as a number but accepts a decimal string on
// deserialize, so we tolerate both.
type Checkpoint struct {
	Root Root `json:"root"`
	Slot Slot `json:"slot"`
}

// Slot is a lean slot number. It tolerates both JSON numbers and decimal
// strings (ethlambda's Checkpoint.slot uses a string-accepting deserializer).
type Slot uint64

// UnmarshalJSON accepts either a JSON number or a decimal string.
func (s *Slot) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var str string
		if err := json.Unmarshal(data, &str); err != nil {
			return err
		}
		v, err := strconv.ParseUint(str, 10, 64)
		if err != nil {
			return fmt.Errorf("lean.Slot: invalid decimal string %q: %w", str, err)
		}
		*s = Slot(v)
		return nil
	}
	var v uint64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = Slot(v)
	return nil
}

// AttestationData is the content of an attestation (attestation.rs):
// the attested slot plus head/target/source checkpoints.
type AttestationData struct {
	Slot   Slot       `json:"slot"`
	Head   Checkpoint `json:"head"`
	Target Checkpoint `json:"target"`
	Source Checkpoint `json:"source"`
}

// AggregatedAttestation is one entry in a block body (block.rs / attestation.rs).
// aggregation_bits is the SSZ-encoded bitlist as a 0x-hex string.
type AggregatedAttestation struct {
	AggregationBits HexBytes        `json:"aggregation_bits"`
	Data            AttestationData `json:"data"`
}

// BlockBody carries the attestations packed into a block.
type BlockBody struct {
	Attestations []AggregatedAttestation `json:"attestations"`
}

// Block mirrors ethlambda's Block (block.rs). There is no signature here: the
// JSON handler serializes the inner Block, not SignedBlock. XMSS proofs are
// opaque and not exposed by the read API.
type Block struct {
	Slot          Slot      `json:"slot"`
	ProposerIndex uint64    `json:"proposer_index"`
	ParentRoot    Root      `json:"parent_root"`
	StateRoot     Root      `json:"state_root"`
	Body          BlockBody `json:"body"`
}

// BlockHeader mirrors ethlambda's BlockHeader (block.rs).
type BlockHeader struct {
	Slot          Slot   `json:"slot"`
	ProposerIndex uint64 `json:"proposer_index"`
	ParentRoot    Root   `json:"parent_root"`
	StateRoot     Root   `json:"state_root"`
	BodyRoot      Root   `json:"body_root"`
}

// Validator is a registry entry. Both XMSS pubkeys are opaque hex (52 bytes
// each in the lean spec); attestation/proposal serialize as bare hex (no 0x).
type Validator struct {
	AttestationPubkey HexBytes `json:"attestation_pubkey"`
	ProposalPubkey    HexBytes `json:"proposal_pubkey"`
	Index             uint64   `json:"index"`
}

// Genesis is the response of GET /lean/v0/genesis.
type Genesis struct {
	GenesisTime    uint64 `json:"genesis_time"`
	ValidatorCount uint64 `json:"validator_count"`
}

// Spec is the response of GET /lean/v0/config/spec.
type Spec struct {
	MillisecondsPerSlot     uint64 `json:"MILLISECONDS_PER_SLOT"`
	IntervalsPerSlot        uint64 `json:"INTERVALS_PER_SLOT"`
	MillisecondsPerInterval uint64 `json:"MILLISECONDS_PER_INTERVAL"`
	HistoricalRootsLimit    uint64 `json:"HISTORICAL_ROOTS_LIMIT"`
	ForkDigest              string `json:"FORK_DIGEST"`
}

// SyncState is the response of GET /lean/v0/node/syncing.
type SyncState struct {
	IsSyncing    bool   `json:"is_syncing"`
	HeadSlot     uint64 `json:"head_slot"`
	SyncDistance uint64 `json:"sync_distance"`
}

// NodeIdentity is the response of GET /lean/v0/node/identity.
type NodeIdentity struct {
	Version string `json:"version"`
}

// Attestation is one entry of GET /lean/v0/attestations.
type Attestation struct {
	ValidatorIndex uint64 `json:"validator_index"`
	Slot           uint64 `json:"slot"`
	SourceSlot     uint64 `json:"source_slot"`
	TargetSlot     uint64 `json:"target_slot"`
}

// ForkChoiceNode is one node in the fork-choice tree (fork_choice.rs).
type ForkChoiceNode struct {
	Root          Root   `json:"root"`
	Slot          Slot   `json:"slot"`
	ParentRoot    Root   `json:"parent_root"`
	ProposerIndex uint64 `json:"proposer_index"`
	Weight        uint64 `json:"weight"`
}

// ForkChoice is the response of GET /lean/v0/fork_choice.
type ForkChoice struct {
	Nodes          []ForkChoiceNode `json:"nodes"`
	Head           Root             `json:"head"`
	Justified      Checkpoint       `json:"justified"`
	Finalized      Checkpoint       `json:"finalized"`
	SafeTarget     Root             `json:"safe_target"`
	ValidatorCount uint64           `json:"validator_count"`
}

// JustifiedCheckpoint is the response of GET /lean/v0/checkpoints/justified.
type JustifiedCheckpoint struct {
	Root Root `json:"root"`
	Slot Slot `json:"slot"`
}

// HexBytes is a byte slice that (un)marshals as a hex string. It accepts an
// optional 0x prefix on decode and emits a bare hex string on encode (matching
// ethlambda's validator pubkey serializer; aggregation_bits carry a 0x prefix,
// which is also accepted).
type HexBytes []byte

// UnmarshalJSON decodes a hex string (with or without 0x prefix).
func (h *HexBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	if s == "" {
		*h = HexBytes{}
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("lean.HexBytes: invalid hex: %w", err)
	}
	*h = b
	return nil
}

// MarshalJSON emits a bare lowercase hex string.
func (h HexBytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(h))
}
