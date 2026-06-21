package aidosfi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"github.com/gorilla/websocket"
)

// ConnectWebSocket opens a WebSocket connection to the Aidos real-time event stream.
// It authenticates with the configured API key and returns a channel of WsEvent.
// The caller MUST read from the channel to avoid blocking the connection.
// Close the context to shut down the connection and close the channel.
func (c *AidosClient) ConnectWebSocket(ctx context.Context) (<-chan WsEvent, error) {
	u, err := url.Parse(c.config.WsURL)
	if err != nil {
		return nil, fmt.Errorf("aidosfi: parse ws url: %w", err)
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("aidosfi: dial ws: %w", err)
	}

	// Send auth message
	authMsg, _ := json.Marshal(map[string]string{
		"type":   "auth",
		"apiKey": c.config.APIKey,
	})
	if err := conn.WriteMessage(websocket.TextMessage, authMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("aidosfi: ws auth: %w", err)
	}

	ch := make(chan WsEvent, 64)

	go func() {
		defer close(ch)
		defer conn.Close()

		// Close when context is done
		go func() {
			<-ctx.Done()
			conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
		}()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				// Connection closed or errored — stop the reader
				return
			}

			var event WsEvent
			if err := json.Unmarshal(msg, &event); err != nil {
				log.Printf("aidosfi: ws unmarshal: %v", err)
				continue
			}

			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
