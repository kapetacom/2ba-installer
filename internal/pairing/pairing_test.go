package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient points at s with no real sleeping and no browser launch.
func newTestClient(s string, dots *bytes.Buffer) *Client {
	c := New()
	c.Origin = s
	c.Sleep = func(time.Duration) {} // no real waiting
	c.OpenBrowser = func(string) error { return nil }
	c.ErrOut = dots
	return c
}

func deviceMux(pendingCalls int) http.Handler {
	mux := http.NewServeMux()
	var tokenCalls int
	mux.HandleFunc("/api/auth/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-123",
			"user_code":        "TEST-CODE",
			"verification_url": "http://127.0.0.1/link?code=TEST-CODE",
			"expires_in":       600,
		})
	})
	mux.HandleFunc("/api/auth/device/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		w.Header().Set("Content-Type", "application/json")
		if tokenCalls <= pendingCalls {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"api_key":  "tuba-sk-new-key",
			"model":    "amber",
			"base_url": "https://api.2ba.ai/v1",
		})
	})
	return mux
}

func TestPairPendingThenSuccess(t *testing.T) {
	srv := httptest.NewServer(deviceMux(2)) // pending twice, then success
	defer srv.Close()

	var dots, out bytes.Buffer
	c := newTestClient(srv.URL, &dots)
	tr, err := c.Pair(context.Background(), &out)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}
	if tr.APIKey != "tuba-sk-new-key" {
		t.Errorf("APIKey = %q", tr.APIKey)
	}
	if tr.Model != "amber" || tr.BaseURL != "https://api.2ba.ai/v1" {
		t.Errorf("unexpected model/base: %+v", tr)
	}
	if strings.Count(dots.String(), ".") != 2 {
		t.Errorf("want 2 pending dots, got %q", dots.String())
	}
	if !strings.Contains(out.String(), "TEST-CODE") {
		t.Errorf("verification code not printed:\n%s", out.String())
	}
}

func TestPairTimeout(t *testing.T) {
	srv := httptest.NewServer(deviceMux(1 << 30)) // never approves
	defer srv.Close()

	var dots, out bytes.Buffer
	c := newTestClient(srv.URL, &dots)
	_, err := c.Pair(context.Background(), &out)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRequestCodeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var dots bytes.Buffer
	c := newTestClient(srv.URL, &dots)
	if _, err := c.RequestCode(context.Background()); err == nil {
		t.Fatal("expected an error from device/code")
	}
}

func TestRequestCodeHonoursPinnedOrigin(t *testing.T) {
	// A pinned origin (--api-origin) must not be clobbered by the verify URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev-1",
			"user_code":        "C",
			"verification_url": "https://elsewhere.example/link?code=C",
			"expires_in":       600,
		})
	}))
	defer srv.Close()

	c := New()
	c.Origin = srv.URL
	if cr, err := c.RequestCode(context.Background()); err != nil || cr.DeviceCode != "dev-1" {
		t.Fatalf("request code: %v %+v", err, cr)
	}
	if c.Origin != srv.URL {
		t.Errorf("pinned origin was overridden: %q", c.Origin)
	}
}
