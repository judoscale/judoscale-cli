package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/", "jdo_test")
	return c
}

func TestDoSendsAuthAndQuery(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer jdo_test" {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/v1/apps" || r.URL.Query().Get("a") != "1" || r.URL.Query().Get("b") != "2" {
			t.Errorf("url = %s", r.URL)
		}
		w.Write([]byte(`{"apps":[]}`))
	})

	body, err := c.Do(context.Background(), "GET", "/v1/apps?a=1", url.Values{"b": {"2"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"apps":[]}` {
		t.Errorf("body = %s", body)
	}
}

func TestDoSendsJSONBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || string(b) != `{"x":1}` || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s %s %q", r.Method, b, r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if _, err := c.Do(context.Background(), "PATCH", "/v1/team", nil, []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
}

func TestDoParsesErrorEnvelope(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad id","param":"id"}}`))
	})

	_, err := c.Do(context.Background(), "GET", "/v1/apps/x", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Status != 422 || apiErr.Type != "invalid_request_error" || apiErr.Message != "bad id" || apiErr.Param != "id" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if got := apiErr.Error(); got != "invalid_request_error: bad id (param: id)" {
		t.Errorf("Error() = %q", got)
	}
}

func TestDoNonEnvelopeError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>bad gateway</html>"))
	})

	_, err := c.Do(context.Background(), "GET", "/v1/apps", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 502 || apiErr.Type != "http_error" {
		t.Fatalf("err = %v", err)
	}
}

func TestDoRejectsRelativePath(t *testing.T) {
	c := New("http://example.test", "t")
	if _, err := c.Do(context.Background(), "GET", "v1/apps", nil, nil); err == nil || !strings.Contains(err.Error(), "must start with /") {
		t.Fatalf("err = %v", err)
	}
}
