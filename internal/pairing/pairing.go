// Package pairing implements the browser-approved device flow that mints a
// 2ba API key: request a device code, open the verification page in the
// browser, and poll until the user approves it. It talks to the public 2ba
// backend endpoints /api/auth/device/code and /api/auth/device/token.
package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

// Defaults matching the historical install.sh.
const (
	defaultOrigin = "https://2ba.ai"
	pollInterval  = 3 * time.Second
	maxAttempts   = 200 // ~10 minutes
)

// CodeResp is the response to POST /api/auth/device/code.
type CodeResp struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	VerifyURL  string `json:"verification_url"`
	ExpiresIn  int    `json:"expires_in"`
}

// TokenResp is the success response to POST /api/auth/device/token.
type TokenResp struct {
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	BaseURL string `json:"base_url"`
}

// errResp is the failure/pending response shape.
type errResp struct {
	Error string `json:"error"`
}

// Client performs the device flow.
type Client struct {
	HTTP *http.Client
	// Origin is the website/API origin (POST base for /api/auth/...). When
	// empty it defaults to https://2ba.ai. It is re-derived from the returned
	// verification_url so polling always hits the issuing instance.
	Origin string

	OpenBrowser func(u string) error // default: open/xdg-open
	ErrOut      io.Writer            // for "." progress; default os.Stdout
	// Test seams.
	Now   func() time.Time
	Sleep func(d time.Duration)
}

// New returns a Client with sensible defaults.
func New() *Client {
	return &Client{
		HTTP:        http.DefaultClient,
		OpenBrowser: OpenBrowser,
		ErrOut:      nil,
		Now:         time.Now,
		Sleep:       time.Sleep,
	}
}

func (c *Client) origin() string {
	if c.Origin != "" {
		return c.Origin
	}
	return defaultOrigin
}

// RequestCode starts a pairing session and returns the codes + verification
// URL. When the origin was not set explicitly, it pins c.Origin to the host
// that issued the code so polling hits the issuing instance; when the caller
// pinned an origin (--api-origin) it is kept as-is.
func (c *Client) RequestCode(ctx context.Context) (CodeResp, error) {
	wasEmpty := c.Origin == ""
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin()+"/api/auth/device/code", nil)
	if err != nil {
		return CodeResp{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return CodeResp{}, fmt.Errorf("could not reach %s: %w", c.origin(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return CodeResp{}, fmt.Errorf("device/code returned %s", resp.Status)
	}
	var cr CodeResp
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return CodeResp{}, fmt.Errorf("bad device/code response: %w", err)
	}
	// Pin origin to the issuing host so the token poll hits the same instance
	// (only when the caller didn't already pin one via --api-origin).
	if wasEmpty {
		if u, err := url.Parse(cr.VerifyURL); err == nil && u.Host != "" {
			c.Origin = u.Scheme + "://" + u.Host
		}
	}
	if cr.DeviceCode == "" || cr.UserCode == "" {
		return CodeResp{}, fmt.Errorf("unexpected pairing response")
	}
	return cr, nil
}

// PollToken polls POST /api/auth/device/token until approved or the attempt
// budget is exhausted. It returns TokenResp on success.
func (c *Client) PollToken(ctx context.Context, deviceCode string) (TokenResp, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return TokenResp{}, err
		}
		if c.Sleep != nil {
			c.Sleep(pollInterval)
		}

		body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.origin()+"/api/auth/device/token", bytes.NewReader(body))
		if err != nil {
			return TokenResp{}, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			// Transport hiccup: stay quiet and retry (matches install.sh).
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusBadRequest {
			var er errResp
			if json.Unmarshal(data, &er) == nil && er.Error == "authorization_pending" {
				if c.ErrOut != nil {
					fmt.Fprint(c.ErrOut, ".")
				}
				continue
			}
			return TokenResp{}, fmt.Errorf("pairing failed — run the installer again")
		}
		if resp.StatusCode != http.StatusOK {
			return TokenResp{}, fmt.Errorf("device/token returned %s", resp.Status)
		}

		var tr TokenResp
		if err := json.Unmarshal(data, &tr); err != nil {
			return TokenResp{}, fmt.Errorf("bad device/token response: %w", err)
		}
		if tr.APIKey == "" {
			return TokenResp{}, fmt.Errorf("pairing failed — run the installer again")
		}
		if c.ErrOut != nil {
			fmt.Fprintln(c.ErrOut)
		}
		return tr, nil
	}
	return TokenResp{}, fmt.Errorf("pairing timed out")
}

// Pair runs the whole flow: request code, print the code, open the browser,
// and poll for the key.
func (c *Client) Pair(ctx context.Context, out io.Writer) (TokenResp, error) {
	cr, err := c.RequestCode(ctx)
	if err != nil {
		return TokenResp{}, err
	}
	fmt.Fprintf(out, "\n  ┌─────────────────────────────────────────────────────┐\n")
	fmt.Fprintf(out, "  │  Pairing code:  %s\n", cr.UserCode)
	fmt.Fprintf(out, "  └─────────────────────────────────────────────────────┘\n")
	fmt.Fprintf(out, "  opening your browser — sign in and confirm the code matches\n")
	if c.OpenBrowser != nil {
		_ = c.OpenBrowser(cr.VerifyURL) // best effort; the URL is printed above
	}
	return c.PollToken(ctx, cr.DeviceCode)
}

// OpenBrowser opens u in the default browser on darwin/linux. Failures are
// swallowed: the verification URL is also printed so the user can open it
// manually.
func OpenBrowser(u string) error {
	var cmd string
	if runtime.GOOS == "darwin" {
		cmd = "open"
	} else {
		cmd = "xdg-open"
	}
	_, err := exec.Command(cmd, u).Output() //nolint:errcheck // best-effort
	_ = err
	if err != nil {
		return err
	}
	return nil
}
