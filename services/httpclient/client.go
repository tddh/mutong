package httpclient

import (
	"net"
	"net/http"
	"time"
)

var defaultTransport = &http.Transport{
	DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     90 * time.Second,
	TLSHandshakeTimeout: 10 * time.Second,
	ForceAttemptHTTP2:   true,
}

func New(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: defaultTransport}
}
