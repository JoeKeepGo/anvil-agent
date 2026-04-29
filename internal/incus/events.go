package incus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (c *Client) ListenEvents(ctx context.Context, ch chan<- Event) error {
	resp, err := c.Do(ctx, "GET", "/1.0/events?type=lifecycle", nil)
	if err != nil {
		return fmt.Errorf("connect to incus events: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ReadBody(resp)
		return fmt.Errorf("incus events returned %d: %s", resp.StatusCode, string(body))
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var event Event
			if err := decoder.Decode(&event); err != nil {
				return fmt.Errorf("decode event: %w", err)
			}
			ch <- event
		}
	}
}
