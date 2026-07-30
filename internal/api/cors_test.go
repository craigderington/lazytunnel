package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func corsTestServer(t *testing.T, origins []string) *Server {
	t.Helper()
	return NewServer(context.Background(), Config{
		Addr:           ":0",
		Logger:         zerolog.Nop(),
		AllowedOrigins: origins,
	})
}

// corsResponse runs a request through the middleware and returns the headers.
func corsResponse(t *testing.T, srv *Server, method, origin string) http.Header {
	t.Helper()

	handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(method, "/api/v1/tunnels", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Header()
}

func TestCORSNoOriginHeaderEmitsNothing(t *testing.T) {
	// A same-origin request carries no Origin header. Stamping CORS headers
	// onto it is meaningless, and the old middleware did exactly that.
	h := corsResponse(t, corsTestServer(t, []string{"*"}), http.MethodGet, "")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("got Access-Control-Allow-Origin %q, want none for a request with no Origin", got)
	}
}

func TestCORSEmptyAllowlistDeniesEverything(t *testing.T) {
	// This is the new default. Nothing cross-origin gets through.
	h := corsResponse(t, corsTestServer(t, nil), http.MethodGet, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("got Access-Control-Allow-Origin %q, want none — an empty allowlist must deny", got)
	}
}

func TestCORSWildcardEchoesWildcard(t *testing.T) {
	h := corsResponse(t, corsTestServer(t, []string{"*"}), http.MethodGet, "https://app.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("got %q, want *", got)
	}
	if got := h.Get("Vary"); got != "" {
		t.Errorf("got Vary %q, want none — a wildcard response does not vary by origin", got)
	}
}

func TestCORSExactMatchEchoesOriginAndVaries(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com", "https://admin.example.com"})
	h := corsResponse(t, srv, http.MethodGet, "https://admin.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Errorf("got %q, want the matching origin echoed back", got)
	}
	if got := h.Get("Vary"); got != "Origin" {
		t.Errorf("got Vary %q, want Origin — without it a shared cache can serve one origin's response to another", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods must accompany an allow-origin header")
	}
	if got := h.Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers must accompany an allow-origin header")
	}
}

func TestCORSNonMatchingOriginIsDenied(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})
	h := corsResponse(t, srv, http.MethodGet, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("got %q, want none for an unlisted origin", got)
	}
	if got := h.Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("got Allow-Methods %q, want none — it is meaningless without an allow-origin header", got)
	}
}

func TestCORSMatchingIsCaseSensitiveAndExact(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})

	for _, origin := range []string{
		"https://APP.example.com",
		"https://app.example.com.evil.com",
		"https://evil.com?x=https://app.example.com",
		"http://app.example.com",
	} {
		h := corsResponse(t, srv, http.MethodGet, origin)
		if got := h.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %q was allowed (got %q); matching must be exact", origin, got)
		}
	}
}

func TestCORSWildcardWinsOverSpecificEntries(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com", "*"})
	h := corsResponse(t, srv, http.MethodGet, "https://app.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("got %q, want * — a wildcard in the list must not be silently narrowed", got)
	}
}

func TestCORSPreflightAllowedAndDenied(t *testing.T) {
	srv := corsTestServer(t, []string{"https://app.example.com"})

	allowed := corsResponse(t, srv, http.MethodOptions, "https://app.example.com")
	if got := allowed.Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("allowed preflight: got %q, want the origin echoed", got)
	}

	denied := corsResponse(t, srv, http.MethodOptions, "https://evil.example.com")
	if got := denied.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("denied preflight: got %q, want no allow-origin header", got)
	}
}
