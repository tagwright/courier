// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

// Package integration holds beacon's first real delivery tests: they build
// each backend the same way a host program would, send a real
// Notification or Health through it, and check what actually arrived.
//
// Tier 1 (delivery-proven): ntfy, gotify, and smtp run against real,
// self-hostable servers started by run.sh (ntfy/ntfy, gotify/server,
// axllent/mailpit), and each test polls that server's own API to confirm
// the message landed with the right title, body, and priority.
//
// Tier 2 (request-shape-verified): discord, slack, mattermost, webhook,
// telegram, pushover, matrix, and the gatus telemetry sink point at an
// in-process HTTP catcher (see catcher below) instead of a real account or
// instance, since those need credentials this harness cannot obtain on its
// own. These tests assert the outbound request beacon builds -- method,
// path, headers, and JSON/form body -- is correct, but do not prove a real
// third party ever received or rendered it. See docs/TESTING.md for the
// full breakdown.
//
// Run via test/integration/run.sh, which starts the tier 1 containers,
// runs `go test -tags=integration ./test/integration/...` in a golang:1.23
// container on the same Docker network, and tears everything down after.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/tagwright/courier"
)

// mustEnv reads an environment variable set by run.sh, skipping the test
// with a clear message if it is absent so a stray `go test -tags=integration`
// run outside the harness fails obviously instead of with a confusing
// connection-refused error.
func mustEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("%s not set; run via test/integration/run.sh", name)
	}
	return v
}

// literalResolver returns a courier.SecretResolver that treats the secret
// "name" as the literal value: settings in these tests name a value
// directly (e.g. "topic-abc123") rather than indirecting through a real
// secret store, since resolveByName just needs to prove backends resolve
// at send time, not exercise a particular secret store.
func literalResolver(values map[string]string) courier.SecretResolver {
	return func(name string) (string, error) {
		return values[name], nil
	}
}

// sendNotification builds a Beacon with a single channel of cfg.Type and
// sends n through it via the real public API (courier.New then Notify), the
// same path a host program goes through. It never reaches into Beacon's
// internals, since none are exported for this purpose.
func sendNotification(t *testing.T, cfg courier.ChannelConfig, resolve courier.SecretResolver, n courier.Notification) error {
	t.Helper()
	b, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{cfg}}, resolve)
	if err != nil {
		t.Fatalf("building %s backend: %v", cfg.Type, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return b.Notify(ctx, n)
}

// sendHealth builds a Beacon with a single telemetry sink of cfg.Type and
// reports h through it via the real public API (courier.New then Report).
func sendHealth(t *testing.T, cfg courier.TelemetryConfig, resolve courier.SecretResolver, h courier.Health) error {
	t.Helper()
	b, err := courier.New(courier.Config{Telemetry: []courier.TelemetryConfig{cfg}}, resolve)
	if err != nil {
		t.Fatalf("building %s sink: %v", cfg.Type, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return b.Report(ctx, h)
}

// --- in-process HTTP catcher, used by tier 2 request-shape tests -----------

// capturedRequest is one request the catcher recorded.
type capturedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// catcher is a tiny HTTP server that records every request it receives and
// answers 200 OK, standing in for the real third-party service a tier 2
// backend would otherwise need a live account to reach. It binds to
// 127.0.0.1 and needs no Docker network of its own: the beacon backends
// under test run in the same process and can reach it directly.
type catcher struct {
	mu   sync.Mutex
	reqs []capturedRequest
	srv  *httptest.Server
}

func newCatcher(t *testing.T) *catcher {
	t.Helper()
	c := &catcher{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.mu.Lock()
		c.reqs = append(c.reqs, capturedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   body,
		})
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// URL returns the catcher's base URL, e.g. "http://127.0.0.1:54321".
func (c *catcher) URL() string {
	return c.srv.URL
}

// last returns the most recently captured request, failing the test if
// none arrived.
func (c *catcher) last(t *testing.T) capturedRequest {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reqs) == 0 {
		t.Fatal("catcher: expected a request, got none")
	}
	return c.reqs[len(c.reqs)-1]
}

// count returns how many requests the catcher has recorded so far.
func (c *catcher) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

// decodeJSON unmarshals req.Body into v, failing the test on error.
func decodeJSON(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decoding JSON body %q: %v", body, err)
	}
}

// defaultWait bounds how long a tier 1 test polls a real server's API for a
// message to arrive, before failing.
const defaultWait = 15 * time.Second

// httpJSON does an HTTP request against target, optionally with basic auth
// and a JSON request body, and decodes a JSON response into out (which may
// be nil to discard the body). It fails the test on any transport error or
// non-2xx status, since every caller in this harness needs a successful
// call to proceed.
func httpJSON(t *testing.T, method, target, user, pass string, reqBody, out any) {
	t.Helper()
	var body io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("encoding request body: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("building request to %s: %v", target, err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response from %s: %v", target, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s: status %d: %s", method, target, resp.StatusCode, respBody)
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			t.Fatalf("decoding response from %s: %v (body: %s)", target, err, respBody)
		}
	}
}

// waitFor polls check every 200ms until it returns true or timeout elapses,
// failing the test with msg on timeout. Used against real tier 1 servers
// (ntfy, gotify, mailpit) whose ingestion is asynchronous relative to the
// HTTP response beacon's Send already waited for.
func waitFor(t *testing.T, timeout time.Duration, msg string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for: %s", msg)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
