package lean

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ConsensusRPCClient is the read-only surface the lean explorer consumes from a
// consensus node. ethlambda's /lean/v0 API is the only implementation today;
// the interface keeps the indexer decoupled from the transport so it can be
// mocked in tests.
type ConsensusRPCClient interface {
	GetGenesis(ctx context.Context) (*Genesis, error)
	GetSpec(ctx context.Context) (*Spec, error)
	GetSyncState(ctx context.Context) (*SyncState, error)
	GetNodeIdentity(ctx context.Context) (*NodeIdentity, error)

	GetBlockByID(ctx context.Context, id string) (*Block, error)
	GetBlockHeaderByID(ctx context.Context, id string) (*BlockHeader, error)
	GetBlocksByRange(ctx context.Context, startSlot, count uint64) ([]*Block, error)

	GetAttestations(ctx context.Context, slot *uint64) ([]*Attestation, error)
	GetForkChoice(ctx context.Context) (*ForkChoice, error)
	GetJustifiedCheckpoint(ctx context.Context) (*JustifiedCheckpoint, error)

	// StreamEvents opens the SSE stream and delivers parsed events on the
	// returned channel until ctx is cancelled or the stream errors. The channel
	// is closed when the stream ends.
	StreamEvents(ctx context.Context) (<-chan StreamEvent, error)
}

// Client is the HTTP implementation of ConsensusRPCClient against ethlambda.
type Client struct {
	endpoint   string
	httpClient *http.Client
	headers    map[string]string
}

// NewClient builds a lean RPC client for the given base endpoint
// (e.g. "http://127.0.0.1:5052"). headers may be nil.
func NewClient(endpoint string, headers map[string]string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		headers:  headers,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("GET %s: decode: %w", path, err)
	}
	return nil
}

func (c *Client) GetGenesis(ctx context.Context) (*Genesis, error) {
	var g Genesis
	if err := c.get(ctx, "/lean/v0/genesis", &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func (c *Client) GetSpec(ctx context.Context) (*Spec, error) {
	var s Spec
	if err := c.get(ctx, "/lean/v0/config/spec", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetSyncState(ctx context.Context) (*SyncState, error) {
	var s SyncState
	if err := c.get(ctx, "/lean/v0/node/syncing", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (c *Client) GetNodeIdentity(ctx context.Context) (*NodeIdentity, error) {
	var n NodeIdentity
	if err := c.get(ctx, "/lean/v0/node/identity", &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// GetBlockByID resolves a block by id, which is either a 0x-prefixed root or a
// decimal slot, matching the node's /blocks/{block_id} semantics.
func (c *Client) GetBlockByID(ctx context.Context, id string) (*Block, error) {
	var b Block
	if err := c.get(ctx, "/lean/v0/blocks/"+url.PathEscape(id), &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Client) GetBlockHeaderByID(ctx context.Context, id string) (*BlockHeader, error) {
	var h BlockHeader
	if err := c.get(ctx, "/lean/v0/blocks/"+url.PathEscape(id)+"/header", &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func (c *Client) GetBlocksByRange(ctx context.Context, startSlot, count uint64) ([]*Block, error) {
	path := fmt.Sprintf("/lean/v0/blocks?start_slot=%d&count=%d", startSlot, count)
	var blocks []*Block
	if err := c.get(ctx, path, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func (c *Client) GetAttestations(ctx context.Context, slot *uint64) ([]*Attestation, error) {
	path := "/lean/v0/attestations"
	if slot != nil {
		path += "?slot=" + strconv.FormatUint(*slot, 10)
	}
	var atts []*Attestation
	if err := c.get(ctx, path, &atts); err != nil {
		return nil, err
	}
	return atts, nil
}

func (c *Client) GetForkChoice(ctx context.Context) (*ForkChoice, error) {
	var f ForkChoice
	if err := c.get(ctx, "/lean/v0/fork_choice", &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) GetJustifiedCheckpoint(ctx context.Context) (*JustifiedCheckpoint, error) {
	var cp JustifiedCheckpoint
	if err := c.get(ctx, "/lean/v0/checkpoints/justified", &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

var _ ConsensusRPCClient = (*Client)(nil)
