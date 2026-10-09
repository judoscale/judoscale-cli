// Package client is the core HTTP client for the Judoscale Team API.
package client

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
)

const (
	DefaultBaseURL = "https://api.judoscale.com"
	maxBodyBytes   = 32 << 20
)

// APIError is the API's JSON error envelope plus the HTTP status.
type APIError struct {
	Status  int
	Type    string
	Message string
	Param   string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%s: %s", e.Type, e.Message)
	if e.Param != "" {
		msg += fmt.Sprintf(" (param: %s)", e.Param)
	}
	return msg
}

type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

func New(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     token,
		UserAgent: "judoscale-cli",
		HTTP:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Do sends a request and returns the response body. path may include a query
// string; query is merged on top. Non-2xx responses return an *APIError.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	u, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}

	respBody, status, err := c.send(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if status >= 200 && status < 300 {
		return respBody, nil
	}
	return nil, parseError(status, respBody)
}

func (c *Client) buildURL(path string, query url.Values) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must start with /: %q", path)
	}
	u, err := url.Parse(c.BaseURL + path)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, vs := range query {
		q.Del(k)
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *Client) send(ctx context.Context, method, u string, body []byte) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, 0, err
	}
	return b, resp.StatusCode, nil
}

func parseError(status int, body []byte) *APIError {
	var env struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Type != "" {
		return &APIError{Status: status, Type: env.Error.Type, Message: env.Error.Message, Param: env.Error.Param}
	}
	return &APIError{Status: status, Type: "http_error", Message: fmt.Sprintf("%d %s", status, http.StatusText(status))}
}
