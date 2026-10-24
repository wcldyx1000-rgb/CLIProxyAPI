package executor

import (
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
)

// NewAntigravityCatalogHTTPClient returns the client for Antigravity calls made outside
// the executor, such as the model catalog probe. It follows the executor's transport
// rules: HTTP/1.1 and no ALPN extension, which is what the native client sends to
// daily-cloudcode-pa.googleapis.com on every connection. A bare http.Client would fall
// back to http.DefaultTransport and advertise h2 on this host.
//
// The transport is private to the returned client, so callers should call
// CloseIdleConnections when they are done. Probes are rare, and a private transport keeps
// them from sharing the executor's per-credential pools.
//
// A nil result means proxyURL cannot be turned into a transport, so the caller should
// treat the call as failed instead of sending it without the configured proxy.
func NewAntigravityCatalogHTTPClient(proxyURL string, cfg *config.Config) *http.Client {
	var base *http.Transport
	if strings.TrimSpace(proxyURL) != "" {
		proxied, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
		if errBuild != nil || proxied == nil {
			return nil
		}
		base = proxied
	} else {
		var ok bool
		base, ok = http.DefaultTransport.(*http.Transport)
		if !ok || base == nil {
			// A RoundTripper that is not an *http.Transport owns its own protocol behavior.
			return &http.Client{}
		}
	}
	return &http.Client{Transport: cloneTransportWithHTTP11(base, cfg)}
}
