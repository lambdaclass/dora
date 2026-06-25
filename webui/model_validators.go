package webui

import "time"

// ValidatorsPageData mirrors Dora's validators-list model
// (types/models.ValidatorsPageData), reduced to a lean-local struct. Field
// names/types match what Dora's templates/validators/validators.html reads so
// the real template executes against it unchanged.
//
// Lean reality folds onto the eth model as follows:
//   - Index + PublicKey are real (PublicKey is the validator's XMSS attestation
//     pubkey).
//   - No validator economics: Balance/EffectiveBalance are always 0, so the
//     Balance column renders "0 (0 ETH)" via Dora's gwei helpers.
//   - No epochs: there are no activation/exit epochs, so ShowActivation and
//     ShowExit stay false and those columns render "-".
//   - No withdrawal credentials: ShowWithdrawAddress stays false, so the
//     W/address column renders "-".
//   - No liveness up-check: ShowUpcheck stays false.
//   - State is fixed to "Active": every validator in the registry is active for
//     the whole life of the lean chain (no activation queue, no exits).
//
// The status/credential filter widgets and sort links render but are inert in
// lean: the registry is small and static, so we serve the whole set on one
// page and ignore the filter/sort query params.
type ValidatorsPageData struct {
	FilterPubKey     string                           `json:"filter_pubkey"`
	FilterIndex      string                           `json:"filter_index"`
	FilterName       string                           `json:"filter_name"`
	FilterStatus     string                           `json:"filter_status"`
	FilterWithdrawal string                           `json:"filter_withdrawal"`
	FilterCredTypes  map[uint8]bool                   `json:"filter_cred_types"`
	FilterStatusOpts []ValidatorsPageDataStatusOption `json:"filter_status_opts"`

	Validators       []*ValidatorsPageDataValidator `json:"validators"`
	ValidatorCount   uint64                         `json:"validator_count"`
	FirstValidator   uint64                         `json:"first_validx"`
	LastValidator    uint64                         `json:"last_validx"`
	Sorting          string                         `json:"sorting"`
	IsDefaultSorting bool                           `json:"default_sorting"`
	IsDefaultPage    bool                           `json:"default_page"`
	TotalPages       uint64                         `json:"total_pages"`
	PageSize         uint64                         `json:"page_size"`
	CurrentPageIndex uint64                         `json:"page_index"`
	PrevPageIndex    uint64                         `json:"prev_page_index"`
	NextPageIndex    uint64                         `json:"next_page_index"`
	LastPageIndex    uint64                         `json:"last_page_index"`
	FilteredPageLink string                         `json:"filtered_page_link"`

	UrlParams []urlParam `json:"url_params"`
}

// ValidatorsPageDataStatusOption is one entry in the status filter dropdown
// (status name + count). Lean reports a single "Active" option.
type ValidatorsPageDataStatusOption struct {
	Status string `json:"index"`
	Count  uint64 `json:"count"`
}

// ValidatorsPageDataValidator is one row in the validators list. The eth-only
// economics/epoch/withdrawal fields are present for template compatibility but
// always zero/false in lean; the Show* flags gate their columns to "-".
type ValidatorsPageDataValidator struct {
	Index               uint64    `json:"index"`
	ProjectedIndex      bool      `json:"projected_index"`
	Name                string    `json:"name"`
	PublicKey           []byte    `json:"pubkey"`
	Balance             uint64    `json:"balance"`
	EffectiveBalance    uint64    `json:"eff_balance"`
	State               string    `json:"state"`
	ShowUpcheck         bool      `json:"show_upcheck"`
	UpcheckActivity     uint8     `json:"upcheck_act"`
	UpcheckMaximum      uint8     `json:"upcheck_max"`
	ShowActivation      bool      `json:"show_activation"`
	ActivationTs        time.Time `json:"activation_ts"`
	ActivationEpoch     uint64    `json:"activation_epoch"`
	ShowExit            bool      `json:"show_exit"`
	ExitTs              time.Time `json:"exit_ts"`
	ExitEpoch           uint64    `json:"exit_epoch"`
	ShowWithdrawAddress bool      `json:"show_withdraw_address"`
	WithdrawAddress     []byte    `json:"withdraw_address"`
}
