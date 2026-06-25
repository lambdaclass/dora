package webui

import "time"

// ClientsCLPageData mirrors Dora's consensus-clients model
// (types/models.ClientsCLPageData), reduced to the lean-local subset that
// templates/clients/clients_cl.html actually reads. Field names/types match the
// real template so it executes unchanged.
//
// Lean reality folds onto the eth model as follows:
//   - There is exactly one consensus client: the connected ethlambda node. So
//     Clients holds a single row and ClientCount is 1.
//   - No peer graph / PeerDAS: ShowPeerDASInfos and the DAS-guardian flags stay
//     false and Nodes is empty, so the peer-graph accordion stays empty and the
//     entire PeerDAS custody block (which dereferences PeerDASInfos) is skipped.
//   - Peer counters are unknown (the lean read API exposes no peer info), so the
//     inbound/outbound/total peer counts render as 0.
//   - Sorting is fixed to the default "index"; the sort links render but are
//     inert (a single row needs no sorting).
type ClientsCLPageData struct {
	Clients     []*ClientsCLPageDataClient `json:"clients"`
	ClientCount uint64                     `json:"client_count"`

	// PeerDAS / peer-graph machinery: all off for lean. Nodes is non-nil but
	// empty so `range $root.Nodes` and `len $root.Nodes` are safe; PeerDASInfos
	// is only dereferenced inside `{{ if $root.ShowPeerDASInfos }}`, which is
	// false, so it may stay nil.
	Nodes                     map[string]*clientsCLNode `json:"nodes"`
	ShowPeerDASInfos          bool                      `json:"show_peer_das_infos"`
	PeerDASInfos              *clientsCLPeerDAS         `json:"peer_das"`
	DisableDasGuardianCheck   bool                      `json:"disable_das_guardian_check"`
	EnableDasGuardianMassScan bool                      `json:"enable_das_guardian_mass_scan"`

	Sorting          string `json:"sorting"`
	IsDefaultSorting bool   `json:"is_default_sorting"`

	// Fields read only by the page's <script> block (the cytoscape peer graph and
	// the spec-comparison modal). All zero/nil in lean: PeerMap nil -> the graph
	// has no data; the Expected* spec fields nil -> the spec-mismatch checker has
	// nothing to compare; ShowSensitivePeerInfos false; CurrentForkDigest/Fulu
	// stay zero.
	PeerMap                  *clientsCLPeerMap      `json:"peer_map"`
	ShowSensitivePeerInfos   bool                   `json:"show_sensitive_peer_infos"`
	CurrentForkDigest        []byte                 `json:"current_fork_digest"`
	FuluActivationEpoch      uint64                 `json:"fulu_activation_epoch"`
	ExpectedChainSpec        map[string]interface{} `json:"expected_chain_spec"`
	ExpectedConfigFields     []string               `json:"expected_config_fields"`
	ExpectedPresetFields     []string               `json:"expected_preset_fields"`
	ExpectedDomainTypeFields []string               `json:"expected_domain_type_fields"`
}

// clientsCLPeerMap is the never-populated peer-graph payload (cytoscape nodes +
// edges). PeerMap stays nil in lean, emitted to JS as the literal `null`.
type clientsCLPeerMap struct {
	Nodes []interface{} `json:"nodes"`
	Edges []interface{} `json:"edges"`
}

// ClientsCLPageDataClient is one row in the consensus-clients table. The
// peer-count and ENR/DAS fields are present for template compatibility but stay
// zero/empty in lean.
type ClientsCLPageDataClient struct {
	Index                int       `json:"index"`
	Name                 string    `json:"name"`
	Version              string    `json:"version"`
	HeadSlot             uint64    `json:"head_slot"`
	HeadRoot             []byte    `json:"head_root"`
	Status               string    `json:"status"`
	LastRefresh          time.Time `json:"refresh"`
	LastError            string    `json:"error"`
	PeerID               string    `json:"peer_id"`
	NodeENR              string    `json:"node_enr"`
	PeerCount            uint32    `json:"peer_count"`
	PeersInboundCounter  uint32    `json:"peers_inbound_counter"`
	PeersOutboundCounter uint32    `json:"peers_outbound_counter"`
}

// clientsCLNode is an unused-in-lean placeholder for Dora's peer-graph node. The
// Nodes map is always empty in lean, but the template ranges over it and reads
// .PeerID / .Alias, so the type must carry those fields to type-check.
type clientsCLNode struct {
	PeerID string `json:"peer_id"`
	Alias  string `json:"alias"`
}

// clientsCLPeerDAS is the never-populated PeerDAS payload. It is only
// dereferenced behind `{{ if $root.ShowPeerDASInfos }}` (always false in lean),
// so it stays nil; the fields exist solely to keep the template parseable.
type clientsCLPeerDAS struct {
	NumberOfColumns              uint64                  `json:"number_of_columns"`
	CustodyRequirement           uint64                  `json:"custody_requirement"`
	DataColumnSidecarSubnetCount uint64                  `json:"data_column_sidecar_subnet_count"`
	TotalRows                    int                     `json:"total_rows"`
	ColumnDistribution           map[uint64][]string     `json:"column_distribution"`
	Warnings                     clientsCLPeerDASWarning `json:"warnings"`
}

type clientsCLPeerDASWarning struct {
	HasWarnings            bool     `json:"has_warnings"`
	MissingENRsPeers       []string `json:"missing_enrs_peers"`
	MissingCGCFromENRPeers []string `json:"missing_cgc_from_enr_peers"`
	MissingSpecValues      bool     `json:"missing_spec_values"`
	EmptyColumns           []uint64 `json:"missing_peers_on_column"`
}
