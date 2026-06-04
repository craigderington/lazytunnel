package agentclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Client talks to the lazytunnel control plane API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Register(req types.AgentRegisterRequest) (*types.AgentInfo, error) {
	var out types.AgentInfo
	if err := c.post("/agents/register", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Heartbeat(agentID string) error {
	return c.post("/agents/"+agentID+"/heartbeat", map[string]string{}, nil)
}

func (c *Client) Assignments(agentID string) ([]types.AgentAssignment, error) {
	var out []types.AgentAssignment
	if err := c.get("/agents/"+agentID+"/assignments", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) Report(agentID string, reports []types.AgentStatusReport) error {
	return c.post("/agents/"+agentID+"/report", reports, nil)
}

func (c *Client) Login(username, password string) (string, error) {
	body := types.AgentRegisterRequest{}
	_ = body
	var resp struct {
		Token string `json:"token"`
	}
	req := map[string]string{"username": username, "password": password}
	if err := c.postPublic("/auth/login", req, &resp); err != nil {
		return "", err
	}
	c.Token = resp.Token
	return resp.Token, nil
}

func (c *Client) post(path string, body, out interface{}) error {
	return c.do(http.MethodPost, path, body, out, true)
}

func (c *Client) postPublic(path string, body, out interface{}) error {
	return c.do(http.MethodPost, path, body, out, false)
}

func (c *Client) get(path string, out interface{}) error {
	return c.do(http.MethodGet, path, nil, out, true)
}

func (c *Client) do(method, path string, body, out interface{}, auth bool) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth && c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("api %s %s: %s", method, path, string(msg))
	}

	if out != nil && res.StatusCode != http.StatusNoContent {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}