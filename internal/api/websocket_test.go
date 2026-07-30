package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// checkOriginResult builds a WebSocketManager with the given allowlist and
// runs a handshake request with the given Origin (empty means no header)
// through the upgrader's CheckOrigin hook — the same hook gorilla/websocket
// calls during a real upgrade, without needing to perform one.
func checkOriginResult(t *testing.T, allowedOrigins []string, origin string) bool {
	t.Helper()
	wsm := NewWebSocketManager(allowedOrigins)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	return wsm.upgrader.CheckOrigin(req)
}

func TestWebSocketCheckOriginAbsentOriginIsAccepted(t *testing.T) {
	// A non-browser client (a Go websocket client, a CLI) sends no Origin
	// header at all. Browsers always send one on a WS handshake, so
	// accepting an absent Origin does not weaken the browser-facing
	// protection, and rejecting it would break legitimate non-browser use.
	if !checkOriginResult(t, []string{"https://app.example.com"}, "") {
		t.Error("got false, want true — an absent Origin must be accepted")
	}
}

func TestWebSocketCheckOriginAllowedOriginIsAccepted(t *testing.T) {
	if !checkOriginResult(t, []string{"https://app.example.com"}, "https://app.example.com") {
		t.Error("got false, want true — an origin in the allowlist must be accepted")
	}
}

func TestWebSocketCheckOriginUnlistedOriginIsRejected(t *testing.T) {
	if checkOriginResult(t, []string{"https://app.example.com"}, "https://evil.example.com") {
		t.Error("got true, want false — an origin not in the allowlist must be rejected")
	}
}

func TestWebSocketCheckOriginWildcardAcceptsAnything(t *testing.T) {
	if !checkOriginResult(t, []string{"*"}, "https://anything.example.com") {
		t.Error("got false, want true — a wildcard allowlist must accept any origin")
	}
}

func TestWebSocketCheckOriginEmptyAllowlistRejectsBrowserButAcceptsAbsent(t *testing.T) {
	if checkOriginResult(t, nil, "https://app.example.com") {
		t.Error("got true, want false — an empty allowlist must reject a browser origin")
	}
	if !checkOriginResult(t, nil, "") {
		t.Error("got false, want true — an empty allowlist must still accept an absent Origin (non-browser client)")
	}
}
