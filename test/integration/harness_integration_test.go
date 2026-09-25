// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

//go:build integration

// These helpers serve the tier 1 delivery tests only (ntfy, gotify, smtp),
// which are themselves //go:build integration. They live behind the same tag
// as their callers rather than in the untagged harness_test.go for two
// reasons the Testing Standard's enforceability layer makes concrete:
//
//   - The dead-code gate roots a library at its test binaries (deadcode -test
//     ./...) and runs untagged. A helper called only by an integration-tagged
//     test is unreachable in the untagged build, so leaving it in the untagged
//     harness file makes the gate flag it as dead scaffolding. Co-locating it
//     with its callers is the honest fix: it lives in the same build partition
//     that actually exercises it (run.sh runs `go test -tags=integration`).
//   - mustEnv's t.Skip belongs to the integration leg's skip budget, not the
//     always-run leg (which is budget 0). A resource-gated skip in an untagged
//     file would be counted against the always-run leg and fail closed.
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
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
