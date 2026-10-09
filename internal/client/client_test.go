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
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL+"/", "jdo_test")
	c.sleep = func(context.Context, time.Duration) error { return nil }
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

func TestDoRetriesOn429WithRetryAfter(t *testing.T) {
	calls := 0
	var waits []time.Duration
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != `{"a":1}` {
			t.Errorf("body on retry = %q", b)
		}
		w.Write([]byte(`{}`))
	})
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	var notified int
	c.OnRetry = func(time.Duration) { notified++ }

	if _, err := c.Do(context.Background(), "POST", "/v1/x", nil, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(waits) != 2 || waits[0] != 7*time.Second || notified != 2 {
		t.Errorf("calls=%d waits=%v notified=%d", calls, waits, notified)
	}
}

func TestDoGivesUpAfterMaxRetries(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	})

	_, err := c.Do(context.Background(), "GET", "/v1/apps", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Type != "rate_limit_error" {
		t.Fatalf("err = %v", err)
	}
	if calls != maxRetries+1 {
		t.Errorf("calls = %d", calls)
	}
}

func TestDoRetryAbortsOnContextCancel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.sleep = sleep
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := c.Do(ctx, "GET", "/v1/apps", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestDoRejectsRelativePath(t *testing.T) {
	c := New("http://example.test", "t")
	if _, err := c.Do(context.Background(), "GET", "v1/apps", nil, nil); err == nil || !strings.Contains(err.Error(), "must start with /") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetAllFollowsCursor(t *testing.T) {
	var cursors []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		cursors = append(cursors, r.URL.Query().Get("cursor"))
		if r.URL.Query().Get("from") != "2026-01-01" {
			t.Errorf("from not repeated on %s", r.URL)
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			w.Write([]byte(`{"scale_events":[{"id":1},{"id":2}],"next_cursor":"c2"}`))
		case "c2":
			w.Write([]byte(`{"scale_events":[{"id":3}],"next_cursor":"c3"}`))
		default:
			w.Write([]byte(`{"scale_events":[{"id":4}]}`))
		}
	})

	body, err := c.GetAll(context.Background(), "/v1/processes/1/scale_events", url.Values{"from": {"2026-01-01"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"scale_events":[{"id":1},{"id":2},{"id":3},{"id":4}]}` {
		t.Errorf("body = %s", body)
	}
	if strings.Join(cursors, ",") != ",c2,c3" {
		t.Errorf("cursors = %v", cursors)
	}
}

func TestGetAllSinglePage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"restart_events":[]}`))
	})

	body, err := c.GetAll(context.Background(), "/v1/x", nil)
	if err != nil || string(body) != `{"restart_events":[]}` {
		t.Fatalf("body=%s err=%v", body, err)
	}
}

func TestGetAllStopsOnRepeatedCursor(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[1],"next_cursor":"same"}`))
	})

	if _, err := c.GetAll(context.Background(), "/v1/x", nil); err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	hits := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/docs", http.StatusFound)
	})

	_, err := c.Do(context.Background(), "POST", "/v1/apps", nil, []byte(`{}`))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 302 || !strings.Contains(apiErr.Message, "redirected to /docs") {
		t.Fatalf("err = %v", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d", hits)
	}
}

func TestDoRejectsNonJSONSuccess(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<!doctype html>"))
	})

	_, err := c.Do(context.Background(), "GET", "/v1/apps", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "expected JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoAcceptsJSONVariantsAndEmptyBodies(t *testing.T) {
	for _, ct := range []string{"application/json; charset=utf-8", "application/problem+json"} {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.Write([]byte(`{}`))
		})
		if _, err := c.Do(context.Background(), "GET", "/v1/x", nil, nil); err != nil {
			t.Errorf("%s: %v", ct, err)
		}
	}

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Del("Content-Type")
		w.WriteHeader(http.StatusNoContent)
	})
	if _, err := c.Do(context.Background(), "DELETE", "/v1/x", nil, nil); err != nil {
		t.Errorf("204: %v", err)
	}
}
