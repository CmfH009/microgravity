package httputil

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// MaxErrorBodyBytes bounds how many bytes are read or drained from an HTTP error body.
const MaxErrorBodyBytes = 64 * 1024

// ClientConfig configures timeouts and connection pooling for an LLM HTTP client.
type ClientConfig struct {
	DialTimeout           time.Duration
	KeepAlive             time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	Timeout               time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
}

// NewClient constructs an *http.Client with sensible defaults for streaming inference endpoints.
func NewClient(cfg ClientConfig) *http.Client {
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 15 * time.Second
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 30 * time.Second
	}
	if cfg.IdleConnTimeout <= 0 {
		cfg.IdleConnTimeout = 90 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Minute
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 32
	}
	if cfg.MaxIdleConnsPerHost <= 0 {
		cfg.MaxIdleConnsPerHost = 8
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: cfg.KeepAlive,
		}).DialContext,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		ExpectContinueTimeout: cfg.ExpectContinueTimeout,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
	}
}

// DrainAndClose drains up to MaxErrorBodyBytes from body and closes it so the TCP connection can be reused.
func DrainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, MaxErrorBodyBytes))
	_ = body.Close()
}

// ReadBoundedErrorBody reads up to MaxErrorBodyBytes from body, drains remaining bounded bytes, closes body, and returns the trimmed string.
func ReadBoundedErrorBody(body io.ReadCloser) string {
	if body == nil {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(body, MaxErrorBodyBytes))
	_, _ = io.Copy(io.Discard, io.LimitReader(body, MaxErrorBodyBytes))
	_ = body.Close()
	return strings.TrimSpace(string(data))
}

// EstimateTokens approximates token count from text length (~4 chars/token).
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}
