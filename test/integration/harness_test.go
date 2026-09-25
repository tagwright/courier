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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/tagwright/courier"
)

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

// decodeJSON unmarshals req.Body into v, failing the test on error.
func decodeJSON(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decoding JSON body %q: %v", body, err)
	}
}
