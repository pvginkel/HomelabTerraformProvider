package dnsreservation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/pvginkel/HomelabTerraformProvider/internal/httplog"
)

const httpTimeout = 30 * time.Second

type Client struct {
	baseURL    string
	token      string
	userAgent  string
	httpClient *http.Client
}

func NewClient(baseURL, token, version string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		userAgent:  "terraform-provider-homelab/" + version,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

func (c *Client) Put(ctx context.Context, hostname, mac string) (*Reservation, error) {
	var out Reservation
	if err := c.do(ctx, http.MethodPut, c.reservationPath(hostname), putRequest{MAC: mac}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Get(ctx context.Context, hostname string) (*Reservation, error) {
	var out Reservation
	if err := c.do(ctx, http.MethodGet, c.reservationPath(hostname), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Delete(ctx context.Context, hostname string) error {
	return c.do(ctx, http.MethodDelete, c.reservationPath(hostname), nil, nil)
}

func (c *Client) reservationPath(hostname string) string {
	return c.baseURL + "/reservations/" + url.PathEscape(hostname)
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

	tflog.SubsystemDebug(ctx, httplog.Subsystem, "dns reservation request", map[string]any{
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

	tflog.SubsystemDebug(ctx, httplog.Subsystem, "dns reservation response", map[string]any{
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
