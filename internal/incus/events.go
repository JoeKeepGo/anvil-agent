package incus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
)

const eventsPath = "/1.0/events"

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) ListenEvents(ctx context.Context, ch chan<- Event) error {
	conn, resp, err := websocket.Dial(ctx, "ws://unix.socket"+eventsPath, &websocket.DialOptions{
		HTTPClient: c.http,
	})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("connect to incus events websocket: status %d: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("connect to incus events websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "event stream closed")

	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read incus event websocket: %w", err)
		}

		var event Event
		if err := json.Unmarshal(msg, &event); err != nil {
			return fmt.Errorf("decode event: %w", err)
		}

		select {
		case ch <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
