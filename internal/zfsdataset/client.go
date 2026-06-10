package zfsdataset

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/pvginkel/HomelabTerraformProvider/internal/httplog"
)

const httpTimeout = 30 * time.Second

// Client talks to the per-node iac-provisioner agents. Unlike the other
// resource clients it has no single base URL: a dataset's pool resolves to a
// node hostname via the pools map, and each call is issued against that node's
// agent on a shared port.
type Client struct {
	pools      map[string]string
	token      string
	port       int
	userAgent  string
	httpClient *http.Client
}

func NewClient(pools map[string]string, token string, port int, version string) *Client {
	cp := make(map[string]string, len(pools))
	for k, v := range pools {
		cp[k] = v
	}
	return &Client{
		pools:      cp,
		token:      token,
		port:       port,
		userAgent:  "terraform-provider-homelab/" + version,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

func (c *Client) Put(ctx context.Context, pool, name string, spec Spec) (*Dataset, error) {
	target, err := c.datasetURL(pool, name)
	if err != nil {
		return nil, err
	}
	var out Dataset
	if err := c.do(ctx, http.MethodPut, target, spec, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Get(ctx context.Context, pool, name string) (*Dataset, error) {
	target, err := c.datasetURL(pool, name)
	if err != nil {
		return nil, err
	}
	var out Dataset
	if err := c.do(ctx, http.MethodGet, target, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Delete(ctx context.Context, pool, name string) error {
	target, err := c.datasetURL(pool, name)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, target, nil, nil)
}

// datasetURL resolves pool -> node hostname and builds the agent URL. The full
// dataset path (pool/name) is URL-encoded into a single segment, so the inner
// slashes become %2F.
func (c *Client) datasetURL(pool, name string) (string, error) {
	host, ok := c.pools[pool]
	if !ok {
		return "", &UnmappedPoolError{Pool: pool}
	}
	full := pool + "/" + name
	return fmt.Sprintf("http://%s:%d/zfs/datasets/%s", host, c.port, url.PathEscape(full)), nil
}

func (c *Client) do(ctx context.Context, method, target string, body any, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	tflog.SubsystemDebug(ctx, httplog.Subsystem, "zfs dataset request", map[string]any{
		"method": method,
		"url":    target,
		"body":   string(bodyBytes),
	})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	tflog.SubsystemDebug(ctx, httplog.Subsystem, "zfs dataset response", map[string]any{
		"status": resp.StatusCode,
		"body":   string(respBytes),
	})

	if resp.StatusCode >= 400 {
		apiErr := &APIError{Status: resp.StatusCode}
		var env errorEnvelope
		if len(respBytes) > 0 && json.Unmarshal(respBytes, &env) == nil && (env.Error != "" || env.Message != "") {
			apiErr.Code = env.Error
			apiErr.Message = env.Message
		} else {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		return apiErr
	}

	if out == nil || len(respBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBytes, out); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}
