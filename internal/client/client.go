// Package client is the core HTTP client for the Judoscale Team API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.judoscale.com"
	maxRetries     = 3
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

	// OnRetry is called before sleeping on a 429.
	OnRetry func(wait time.Duration)

	sleep func(context.Context, time.Duration) error
}

func New(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Token:     token,
		UserAgent: "judoscale-cli",
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
			// The API never redirects; following one could also turn a write into a GET.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		sleep: sleep,
	}
}

// Do sends a request and returns the response body. path may include a query
// string; query is merged on top. Non-2xx responses, redirects and non-JSON
// success responses are errors; API errors are returned as *APIError.
// A 429 is retried after the Retry-After delay.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body []byte) ([]byte, error) {
	u, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}

	for attempt := 0; ; attempt++ {
		respBody, status, header, err := c.send(ctx, method, u, body)
		if err != nil {
			return nil, err
		}
		if status == http.StatusTooManyRequests && attempt < maxRetries {
			wait := retryAfter(header, attempt)
			if c.OnRetry != nil {
				c.OnRetry(wait)
			}
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}
		if status >= 200 && status < 300 {
			if err := checkJSON(respBody, header); err != nil {
				return nil, fmt.Errorf("unexpected response from %s: %w", u, err)
			}
			return respBody, nil
		}
		if status >= 300 && status < 400 {
			return nil, redirectError(status, header)
		}
		return nil, parseError(status, respBody)
	}
}

// GetAll fetches every page of a paginated GET endpoint, following next_cursor,
// and returns one object whose array fields are concatenated across pages.
func (c *Client) GetAll(ctx context.Context, path string, query url.Values) ([]byte, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}

	var merged map[string]json.RawMessage
	for {
		body, err := c.Do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return nil, err
		}
		var page map[string]json.RawMessage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("paginated response is not a JSON object: %w", err)
		}

		var next string
		if raw, ok := page["next_cursor"]; ok {
			_ = json.Unmarshal(raw, &next)
		}
		delete(page, "next_cursor")

		if merged == nil {
			merged = page
		} else if err := appendArrays(merged, page); err != nil {
			return nil, err
		}

		if next == "" {
			break
		}
		if next == q.Get("cursor") {
			return nil, errors.New("pagination did not advance: next_cursor repeated")
		}
		q.Set("cursor", next)
	}
	return json.Marshal(merged)
}

func appendArrays(dst, src map[string]json.RawMessage) error {
	for k, raw := range src {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		var existing []json.RawMessage
		if err := json.Unmarshal(dst[k], &existing); err != nil {
			continue
		}
		b, err := json.Marshal(append(existing, items...))
		if err != nil {
			return err
		}
		dst[k] = b
	}
	return nil
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

// NewRequest builds the request Do would send, without sending it.
func (c *Client) NewRequest(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Request, error) {
	u, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}
	return c.httpRequest(ctx, method, u, body)
}

func (c *Client) httpRequest(ctx context.Context, method, u string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) send(ctx context.Context, method, u string, body []byte) ([]byte, int, http.Header, error) {
	req, err := c.httpRequest(ctx, method, u, body)
	if err != nil {
		return nil, 0, nil, err
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, 0, nil, err
	}
	return b, resp.StatusCode, resp.Header, nil
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

func redirectError(status int, h http.Header) *APIError {
	msg := fmt.Sprintf("%d %s", status, http.StatusText(status))
	if loc := h.Get("Location"); loc != "" {
		msg += ", redirected to " + loc
	}
	return &APIError{Status: status, Type: "http_error", Message: msg}
}

// checkJSON rejects a non-empty body that isn't declared as JSON, such as an
// HTML page from a wrong base URL or a proxy.
func checkJSON(body []byte, h http.Header) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	ct := h.Get("Content-Type")
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
		return fmt.Errorf("expected JSON, got content type %q", ct)
	}
	return nil
}

// retryAfter reads Retry-After seconds, falling back to a short backoff.
func retryAfter(h http.Header, attempt int) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(attempt+1) * time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
