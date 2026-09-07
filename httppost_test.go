// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A body template used only as a test fixture: an arbitrary JSON shape that
// exercises the tojson escaping and the .Recipient / .Message variables. No
// particular target's contract lives in the library.
const fixtureTemplate = `{"channel":{{.Recipient | tojson}},"text":{{.Message | tojson}}}`

// TestHTTPBackendBodyRender proves the rendered POST body is exactly what the
// template dictates, byte-for-byte, including a raw em dash (U+2014) passed
// through tojson without being escaped, and a quote that is escaped.
func TestHTTPBackendBodyRender(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json (default)", ct)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	b, err := newHTTPBackendFromSettings(map[string]string{
		"url":           srv.URL,
		"recipient":     "team",
		"body_template": fixtureTemplate,
		"success_field": "ok",
	}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// The message carries an em dash (must survive verbatim) and a double quote
	// (must be JSON-escaped), so the render exercises both paths.
	err = b.Send(context.Background(), Notification{Title: `alpha — say "hi"`})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	want := `{"channel":"team","text":"alpha — say \"hi\""}`
	if string(gotBody) != want {
		t.Errorf("POST body\n got: %s\nwant: %s", gotBody, want)
	}
}

// TestHTTPBackendSuccessCheck proves the response-body success check: a 200
// that reports failure in its body is treated as a failed delivery.
func TestHTTPBackendSuccessCheck(t *testing.T) {
	newBackend := func(t *testing.T, url string) Backend {
		t.Helper()
		b, err := newHTTPBackendFromSettings(map[string]string{
			"url":            url,
			"body_template":  fixtureTemplate,
			"success_field":  "ok",
			"success_equals": "true",
		}, nil)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return b
	}

	cases := []struct {
		name    string
		reply   string
		status  int
		wantErr bool
	}{
		{"ok-true", `{"ok":true}`, 200, false},
		{"ok-false", `{"ok":false}`, 200, true},
		{"field-missing", `{"other":1}`, 200, true},
		{"non-2xx", `boom`, 500, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.reply))
			}))
			defer srv.Close()

			err := newBackend(t, srv.URL).Send(context.Background(), Notification{Title: "x"})
			if tc.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestHTTPBackendTruthyCheck covers success_field with no success_equals: the
// field just has to be truthy.
func TestHTTPBackendTruthyCheck(t *testing.T) {
	for _, tc := range []struct {
		reply   string
		wantErr bool
	}{
		{`{"ok":true}`, false},
		{`{"ok":false}`, true},
		{`{"ok":1}`, false},
		{`{"ok":0}`, true},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(tc.reply))
		}))
		b, err := newHTTPBackendFromSettings(map[string]string{
			"url": srv.URL, "body_template": `{}`, "success_field": "ok",
		}, nil)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		err = b.Send(context.Background(), Notification{Title: "x"})
		srv.Close()
		if tc.wantErr && err == nil {
			t.Errorf("%s: expected error", tc.reply)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.reply, err)
		}
	}
}

// TestHTTPBackendNoSuccessCheck pins the default: with no success_field
// configured, the response body is never parsed and only the HTTP status
// decides. Any 2xx is a success (as for a plain webhook), and a non-2xx is a
// failure even without a body check. The mock replies with a body that is NOT
// valid JSON to prove it is never inspected in the no-check case.
func TestHTTPBackendNoSuccessCheck(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{"200 is success", 200, false},
		{"204 is success", 204, false},
		{"non-2xx is failure", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`not json`))
			}))
			defer srv.Close()

			b, err := newHTTPBackendFromSettings(map[string]string{
				"url": srv.URL, "body_template": `{}`,
			}, nil)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			err = b.Send(context.Background(), Notification{Title: "x"})
			if tc.wantErr && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestHTTPBackendUnreachable proves a transport failure surfaces.
func TestHTTPBackendUnreachable(t *testing.T) {
	b, err := newHTTPBackendFromSettings(map[string]string{
		"url": "http://127.0.0.1:1", "body_template": `{}`,
	}, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := b.Send(context.Background(), Notification{Title: "x"}); err == nil {
		t.Error("expected an error when the target is unreachable")
	}
}

// TestHTTPBackendConfigErrors covers construction-time validation.
func TestHTTPBackendConfigErrors(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]string
	}{
		{"no url", map[string]string{"body_template": `{}`}},
		{"both urls", map[string]string{"url": "http://x", "url_secret": "s", "body_template": `{}`}},
		{"no template", map[string]string{"url": "http://x"}},
		{"bad template", map[string]string{"url": "http://x", "body_template": `{{ .Nope`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newHTTPBackendFromSettings(tc.settings, nil); err == nil {
				t.Error("expected a construction error, got nil")
			}
		})
	}
}

// TestHTTPBackendURLSecret proves the URL can come from a resolved secret.
func TestHTTPBackendURLSecret(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(name string) (string, error) {
		if name == "hook_url" {
			return srv.URL, nil
		}
		return "", errUnknownSecret
	}
	b, err := newHTTPBackendFromSettings(map[string]string{
		"url_secret": "hook_url", "body_template": `{}`, "success_field": "ok",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := b.Send(context.Background(), Notification{Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !hit {
		t.Error("target resolved from url_secret was not called")
	}
}

var errUnknownSecret = errorString("unknown secret")

type errorString string

func (e errorString) Error() string { return string(e) }
