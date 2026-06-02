package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type WSClient struct {
	baseURL string
	token   string
	dialer  *websocket.Dialer
}

func NewWSClient(serverURL, token string) *WSClient {
	return &WSClient{
		baseURL: strings.TrimRight(serverURL, "/"),
		token:   token,
		dialer: &websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
			Subprotocols:     []string{"binary.k8s.io"},
		},
	}
}

func (c *WSClient) Dial(path string) (*websocket.Conn, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing server URL: %w", err)
	}

	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}

	wsURL := fmt.Sprintf("%s://%s%s", scheme, u.Host, path)
	if u.Path != "" && u.Path != "/" {
		wsURL = fmt.Sprintf("%s://%s%s%s", scheme, u.Host, u.Path, path)
	}

	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}

	conn, _, err := c.dialer.Dial(wsURL, header)
	if err != nil {
		return nil, fmt.Errorf(
			"[ERROR] WebSocket连接失败 (exit_code=4)\n[CONTEXT] server=%s, path=%s\n[HINT] verify server supports WebSocket and is reachable",
			c.baseURL, path,
		)
	}
	return conn, nil
}
