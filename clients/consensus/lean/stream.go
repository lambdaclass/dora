package lean

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamEventType identifies the kind of chain event in an SSE frame.
type StreamEventType string

const (
	StreamEventHead                StreamEventType = "head"
	StreamEventBlock               StreamEventType = "block"
	StreamEventFinalizedCheckpoint StreamEventType = "finalized_checkpoint"
)

// HeadEventData is the data payload of a "head" SSE frame.
type HeadEventData struct {
	Slot       uint64 `json:"slot"`
	Root       Root   `json:"root"`
	ParentRoot Root   `json:"parent_root"`
}

// BlockEventData is the data payload of a "block" SSE frame.
type BlockEventData struct {
	Slot uint64 `json:"slot"`
	Root Root   `json:"root"`
}

// FinalizedCheckpointEventData is the data payload of a
// "finalized_checkpoint" SSE frame.
type FinalizedCheckpointEventData struct {
	Slot uint64 `json:"slot"`
	Root Root   `json:"root"`
}

// StreamEvent is a parsed SSE frame. Exactly one of the typed payloads is set
// according to Type; Err is set if a frame failed to parse (the stream
// continues).
type StreamEvent struct {
	Type      StreamEventType
	Head      *HeadEventData
	Block     *BlockEventData
	Finalized *FinalizedCheckpointEventData
	Err       error
}

// StreamEvents opens GET /lean/v0/events and parses the SSE stream, delivering
// events on the returned channel. The channel closes when ctx is cancelled or
// the connection drops; callers reconnect and backfill the gap.
func (c *Client) StreamEvents(ctx context.Context) (<-chan StreamEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/lean/v0/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Use a client without the short timeout: SSE is long-lived.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open event stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("event stream: status %d: %s", resp.StatusCode, string(body))
	}

	out := make(chan StreamEvent, 64)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		parseSSE(ctx, resp.Body, out)
	}()
	return out, nil
}

// parseSSE reads an SSE byte stream, assembling event/data frames separated by
// blank lines, and emits a StreamEvent for each complete frame.
func parseSSE(ctx context.Context, r io.Reader, out chan<- StreamEvent) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var eventName string
	var dataLines []string

	flush := func() {
		if eventName == "" && len(dataLines) == 0 {
			return
		}
		data := strings.Join(dataLines, "\n")
		ev := decodeEvent(StreamEventType(eventName), data)
		eventName = ""
		dataLines = dataLines[:0]
		if ev == nil {
			return
		}
		select {
		case out <- *ev:
		case <-ctx.Done():
		}
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			// SSE comment / keep-alive.
			continue
		}
		if name, ok := strings.CutPrefix(line, "event:"); ok {
			eventName = strings.TrimSpace(name)
			continue
		}
		if data, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(data, " "))
			continue
		}
	}
	// Emit a trailing frame if the stream ended without a final blank line.
	flush()
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		select {
		case out <- StreamEvent{Err: fmt.Errorf("event stream read: %w", err)}:
		case <-ctx.Done():
		}
	}
}

// decodeEvent parses one assembled SSE frame into a StreamEvent. Unknown event
// types are ignored (returns nil).
func decodeEvent(name StreamEventType, data string) *StreamEvent {
	switch name {
	case StreamEventHead:
		var d HeadEventData
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return &StreamEvent{Type: name, Err: err}
		}
		return &StreamEvent{Type: name, Head: &d}
	case StreamEventBlock:
		var d BlockEventData
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return &StreamEvent{Type: name, Err: err}
		}
		return &StreamEvent{Type: name, Block: &d}
	case StreamEventFinalizedCheckpoint:
		var d FinalizedCheckpointEventData
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return &StreamEvent{Type: name, Err: err}
		}
		return &StreamEvent{Type: name, Finalized: &d}
	default:
		return nil
	}
}
