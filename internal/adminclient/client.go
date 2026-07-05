// Package adminclient talks to a running shadow-mcp daemon's loopback admin
// API, auto-starting one if none is reachable yet.
package adminclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/shadow-code/shadow-mcp/internal/config"
	"github.com/shadow-code/shadow-mcp/internal/daemon"
)

// Client is an authenticated handle to one running daemon.
type Client struct {
	BaseURL string
	Token   string
	http    *http.Client
}

// New wraps an already-known daemon base URL and token.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, http: &http.Client{Timeout: 10 * time.Second}}
}

// AuthenticatedHTTPClient returns an *http.Client that injects this client's
// bearer token on every request, suitable for handing to an
// mcp.StreamableClientTransport that talks to the daemon's MCP relay endpoints.
func (c *Client) AuthenticatedHTTPClient() *http.Client {
	return &http.Client{Transport: &authRoundTripper{token: c.Token, base: http.DefaultTransport}}
}

// MCPEndpoint returns the full URL of the daemon's MCP relay endpoint for a
// profile (mounted at its configured http_path, or /mcp/<name> by default).
// An empty profileName resolves to the reserved unfiltered path.
func (c *Client) MCPEndpoint(path string) string {
	return c.BaseURL + path
}

func (c *Client) Status() (daemon.StatusResponse, error) {
	var v daemon.StatusResponse
	err := c.get("/status", &v)
	return v, err
}

func (c *Client) Config() (daemon.ConfigSnapshot, error) {
	var v daemon.ConfigSnapshot
	err := c.get("/config", &v)
	return v, err
}

func (c *Client) RecentCalls(limit int) ([]daemon.CallRecord, error) {
	var v []daemon.CallRecord
	err := c.get(fmt.Sprintf("/calls/recent?limit=%d", limit), &v)
	return v, err
}

func (c *Client) Reload() error {
	return c.post("/reload", nil)
}

// Server/Profile/Rule CRUD - see internal/daemon's registerCRUD for the
// matching server-side routes. Get* returns the raw (pre-interpolation)
// entity, suitable for pre-filling an edit form with its literal "${VAR}"
// values rather than a resolved secret. originalName in Update* is the
// entity's current name (its new value may rename it); Create* fails if the
// name already exists.
func (c *Client) GetServer(name string) (config.DownstreamServer, error) {
	var v config.DownstreamServer
	err := c.get("/config/servers/"+url.PathEscape(name), &v)
	return v, err
}
func (c *Client) CreateServer(s config.DownstreamServer) error {
	return c.postJSON("/config/servers", s, nil)
}
func (c *Client) UpdateServer(originalName string, s config.DownstreamServer) error {
	return c.putJSON("/config/servers/"+url.PathEscape(originalName), s, nil)
}
func (c *Client) DeleteServer(name string) error {
	return c.deleteReq("/config/servers/" + url.PathEscape(name))
}

func (c *Client) GetProfile(name string) (config.Profile, error) {
	var v config.Profile
	err := c.get("/config/profiles/"+url.PathEscape(name), &v)
	return v, err
}
func (c *Client) CreateProfile(p config.Profile) error {
	return c.postJSON("/config/profiles", p, nil)
}
func (c *Client) UpdateProfile(originalName string, p config.Profile) error {
	return c.putJSON("/config/profiles/"+url.PathEscape(originalName), p, nil)
}
func (c *Client) DeleteProfile(name string) error {
	return c.deleteReq("/config/profiles/" + url.PathEscape(name))
}

func (c *Client) GetRule(name string) (config.Rule, error) {
	var v config.Rule
	err := c.get("/config/rules/"+url.PathEscape(name), &v)
	return v, err
}
func (c *Client) CreateRule(r config.Rule) error {
	return c.postJSON("/config/rules", r, nil)
}
func (c *Client) UpdateRule(originalName string, r config.Rule) error {
	return c.putJSON("/config/rules/"+url.PathEscape(originalName), r, nil)
}
func (c *Client) DeleteRule(name string) error {
	return c.deleteReq("/config/rules/" + url.PathEscape(name))
}

func (c *Client) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) post(path string, out any) error {
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) postJSON(path string, body, out any) error {
	return c.bodyRequest(http.MethodPost, path, body, out)
}

func (c *Client) putJSON(path string, body, out any) error {
	return c.bodyRequest(http.MethodPut, path, body, out)
}

func (c *Client) deleteReq(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *Client) bodyRequest(method, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin api %s: %s: %s", req.URL.Path, resp.Status, string(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

type authRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (t *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
