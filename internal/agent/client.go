package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/serverpal/serverpal/internal/config"
)

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(cfg config.Config) (*Client, error) {
	tokenBytes, err := os.ReadFile(cfg.AgentTokenFile)
	if err != nil {
		return nil, fmt.Errorf("read agent token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 32 {
		return nil, errors.New("agent token is invalid")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", cfg.AgentSocket)
		},
		MaxIdleConns:          8,
		IdleConnTimeout:       60 * time.Second,
		DisableCompression:    true,
		ResponseHeaderTimeout: 2 * time.Minute,
	}
	return &Client{
		token: token,
		httpClient: &http.Client{
			Transport: transport,
		},
	}, nil
}

func (c *Client) Metrics(ctx context.Context) (Metrics, error) {
	var result Metrics
	err := c.JSON(ctx, http.MethodGet, "/v1/metrics", nil, &result)
	return result, err
}

func (c *Client) System(ctx context.Context) (SystemStatus, error) {
	var result SystemStatus
	err := c.JSON(ctx, http.MethodGet, "/v1/system", nil, &result)
	return result, err
}

func (c *Client) EnsureProjectRoot(ctx context.Context, path string) error {
	return c.JSON(ctx, http.MethodPost, "/v1/project/root", map[string]string{"path": path}, nil)
}

func (c *Client) Processes(ctx context.Context) ([]Process, error) {
	var result []Process
	err := c.JSON(ctx, http.MethodGet, "/v1/pm2/list", nil, &result)
	return result, err
}

func (c *Client) ProjectAction(ctx context.Context, request ProjectAction) error {
	return c.JSON(ctx, http.MethodPost, "/v1/pm2/action", request, nil)
}

func (c *Client) Logs(ctx context.Context, name string, lines int) (string, error) {
	query := url.Values{"name": {name}, "lines": {strconv.Itoa(lines)}}
	var result struct {
		Logs string `json:"logs"`
	}
	err := c.JSON(ctx, http.MethodGet, "/v1/pm2/logs?"+query.Encode(), nil, &result)
	return result.Logs, err
}

func (c *Client) ApplyDomain(ctx context.Context, domain DomainSpec) error {
	return c.JSON(ctx, http.MethodPost, "/v1/domain/apply", domain, nil)
}

func (c *Client) RemoveDomain(ctx context.Context, id string) error {
	return c.JSON(ctx, http.MethodPost, "/v1/domain/remove", map[string]string{"id": id}, nil)
}

func (c *Client) CertificateStatus(ctx context.Context, hostname string) (CertificateStatus, error) {
	var result CertificateStatus
	err := c.JSON(ctx, http.MethodGet, "/v1/domain/status?"+url.Values{"hostname": {hostname}}.Encode(), nil, &result)
	return result, err
}

func (c *Client) JSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://serverpal"+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAgentError(response)
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode agent response: %w", err)
	}
	return nil
}

func (c *Client) Do(request *http.Request) (*http.Response, error) {
	request.Header.Set("X-ServerPal-Agent-Token", c.token)
	return c.httpClient.Do(request)
}

func (c *Client) DialTerminal(ctx context.Context, cwd string) (*websocket.Conn, *http.Response, error) {
	query := url.Values{"cwd": {cwd}}
	return websocket.Dial(ctx, "ws://serverpal/v1/terminal?"+query.Encode(), &websocket.DialOptions{
		HTTPClient: c.httpClient,
		HTTPHeader: http.Header{"X-ServerPal-Agent-Token": {c.token}},
	})
}

func decodeAgentError(response *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &payload) == nil && payload.Error != "" {
		return errors.New(payload.Error)
	}
	return fmt.Errorf("agent returned HTTP %d", response.StatusCode)
}
