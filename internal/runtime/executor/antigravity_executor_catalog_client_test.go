package executor

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestNewAntigravityCatalogHTTPClientOmitsALPN(t *testing.T) {
	cfg := &config.Config{}
	tests := []struct {
		name     string
		proxyURL string
	}{
		{name: "direct", proxyURL: ""},
		{name: "http proxy", proxyURL: "http://127.0.0.1:18090"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewAntigravityCatalogHTTPClient(tc.proxyURL, cfg)
			if client == nil {
				t.Fatal("client is nil")
			}
			transport, ok := client.Transport.(*http.Transport)
			if !ok || transport == nil {
				t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
			}
			if transport.ForceAttemptHTTP2 || len(transport.TLSNextProto) != 0 {
				t.Fatal("catalog client must not enable HTTP/2")
			}
			if transport.TLSClientConfig == nil || len(transport.TLSClientConfig.NextProtos) != 0 {
				t.Fatal("catalog client must omit ALPN like the native client")
			}
		})
	}
}

func TestNewAntigravityCatalogHTTPClientRejectsInvalidProxy(t *testing.T) {
	client := NewAntigravityCatalogHTTPClient("ftp://bad-proxy", &config.Config{})
	if client != nil {
		t.Fatal("invalid proxy must not produce a client that bypasses the proxy")
	}
}
