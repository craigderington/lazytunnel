package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// checkOriginForRequest builds a WebSocketManager with the given allowlist and
// runs an already-constructed handshake request through the upgrader's
// CheckOrigin hook — the same hook gorilla/websocket calls during a real
// upgrade, without needing to perform one.
func checkOriginForRequest(t *testing.T, allowedOrigins []string, req *http.Request) bool {
	t.Helper()
	return NewWebSocketManager(allowedOrigins).upgrader.CheckOrigin(req)
}

// checkOriginResult is checkOriginForRequest for the common case: a handshake
// to /api/v1/ws carrying the given Origin (empty means no header at all).
func checkOriginResult(t *testing.T, allowedOrigins []string, origin string) bool {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	return checkOriginForRequest(t, allowedOrigins, req)
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

func TestWebSocketCheckOriginSameOriginIsAcceptedWithEmptyAllowlist(t *testing.T) {
	// The regression guard. Unlike fetch, which omits Origin on a same-origin
	// GET, a browser ALWAYS sends Origin on a WebSocket handshake — including a
	// same-origin one. So "absent Origin" cannot stand in for "same origin"
	// here the way it does in corsMiddleware. If the allowlist alone decided
	// this, the UI this very server hands out from web/dist could not open its
	// own WebSocket under the shipped deny-all default.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	req.Header.Set("Origin", "http://"+req.Host)

	if !checkOriginForRequest(t, nil, req) {
		t.Errorf("got false, want true — Origin %q is same-origin with Host %q and must be accepted with an empty allowlist", req.Header.Get("Origin"), req.Host)
	}
}

func TestWebSocketCheckOriginSameHostDifferentPortIsRejected(t *testing.T) {
	// http://localhost:5173 and http://localhost:8080 are genuinely different
	// origins. The Vite dev proxy case must fall through to the allowlist, not
	// be silently treated as same-origin, so the host comparison has to include
	// the port.
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/ws", nil)
	req.Header.Set("Origin", "http://localhost:5173")

	if checkOriginForRequest(t, nil, req) {
		t.Errorf("got true, want false — Origin %q differs in port from Host %q and must not count as same-origin", req.Header.Get("Origin"), req.Host)
	}
}

func TestWebSocketCheckOriginMalformedOriginIsRejected(t *testing.T) {
	// A malformed Origin is not something to be lenient about: it is denied
	// before the allowlist is consulted, so neither a wildcard nor an entry
	// that happens to match the raw bytes can let it through.
	const malformed = "http://[::1"

	for _, allowlist := range [][]string{nil, {"*"}, {malformed}} {
		if checkOriginResult(t, allowlist, malformed) {
			t.Errorf("allowlist %v: got true, want false — a malformed Origin %q must be rejected", allowlist, malformed)
		}
	}
}

// TestWebSocketHandshakeSameOriginSucceedsWithEmptyAllowlist proves the fix the
// way the regression was found: a real gorilla/websocket handshake against the
// server's own router. A unit test on checkOrigin alone did not catch the
// original break, because it never exercised an Origin equal to the serving
// host.
func TestWebSocketHandshakeSameOriginSucceedsWithEmptyAllowlist(t *testing.T) {
	srv := NewServer(context.Background(), Config{
		Addr:           ":0",
		Logger:         zerolog.Nop(),
		AllowedOrigins: nil, // the shipped default: deny all cross-origin
	})

	ts := httptest.NewServer(srv.router)
	defer func() {
		ts.Close()
		srv.wsManager.Stop()
	}()

	header := http.Header{}
	header.Set("Origin", ts.URL) // exactly what a browser loading the bundled UI sends

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("same-origin handshake to %s with Origin %q failed: status=%d err=%v — the bundled web UI cannot open its own WebSocket", wsURL, ts.URL, status, err)
	}
	conn.Close()
}
