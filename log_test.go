// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// LogBackend is the always-on fallback floor: it depends on nothing outside the
// standard library and is what a Config falls back to when no other channel is
// configured or reachable (task #618). Its contract is that Send NEVER errors,
// because the floor is not allowed to be the reason a notification is lost.
// These are internal (package courier) tests because formatLine, slogLevel, and
// slogAttrs are unexported, matching webhook_test.go's package.

// TestLogBackendPlainLineNeverErrors proves the writer-backed backend formats the
// notification onto its io.Writer and returns nil, at every level.
func TestLogBackendPlainLineNeverErrors(t *testing.T) {
	for _, level := range []Level{LevelInfo, LevelWarning, LevelError} {
		var buf bytes.Buffer
		b := NewLogBackend(&buf)
		if b.Name() != "log" {
			t.Errorf("Name() = %q, want \"log\"", b.Name())
		}
		n := Notification{Title: "backup failed", Body: "restic exited 1", Level: level}
		if err := b.Send(context.Background(), n); err != nil {
			t.Fatalf("Send at level %v returned error, the floor must never error: %v", level, err)
		}
		out := buf.String()
		if !strings.Contains(out, strings.ToUpper(level.String())) {
			t.Errorf("output %q missing uppercased level %q", out, strings.ToUpper(level.String()))
		}
		if !strings.Contains(out, "backup failed") || !strings.Contains(out, "restic exited 1") {
			t.Errorf("output %q missing title or body", out)
		}
	}
}

// TestFormatLine pins the plain-line format across the optional-field
// combinations. A single Field keeps the output deterministic (map iteration
// order is unspecified with more than one).
func TestFormatLine(t *testing.T) {
	tests := []struct {
		name string
		n    Notification
		want string
	}{
		{
			name: "title only",
			n:    Notification{Title: "hello", Level: LevelInfo},
			want: "[INFO] hello",
		},
		{
			name: "title and body",
			n:    Notification{Title: "hello", Body: "world", Level: LevelWarning},
			want: "[WARNING] hello: world",
		},
		{
			name: "tags",
			n:    Notification{Title: "hi", Level: LevelError, Tags: []string{"a", "b"}},
			want: "[ERROR] hi (tags=a,b)",
		},
		{
			name: "single field",
			n:    Notification{Title: "hi", Level: LevelInfo, Fields: map[string]string{"svc": "kimai"}},
			want: "[INFO] hi svc=kimai",
		},
		{
			name: "body tags and field",
			n:    Notification{Title: "t", Body: "b", Level: LevelError, Tags: []string{"x"}, Fields: map[string]string{"k": "v"}},
			want: "[ERROR] t: b (tags=x) k=v",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatLine(tc.n); got != tc.want {
				t.Errorf("formatLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSlogLevel proves the Level-to-slog.Level mapping, including that an
// out-of-range Level falls through to Info rather than panicking or dropping.
func TestSlogLevel(t *testing.T) {
	tests := []struct {
		in   Level
		want slog.Level
	}{
		{LevelInfo, slog.LevelInfo},
		{LevelWarning, slog.LevelWarn},
		{LevelError, slog.LevelError},
		{Level(99), slog.LevelInfo}, // unknown falls through to Info
	}
	for _, tc := range tests {
		if got := slogLevel(tc.in); got != tc.want {
			t.Errorf("slogLevel(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestSlogAttrs proves body and tags become attributes when present and are
// omitted when empty, and that a Field is carried through as its own attribute.
func TestSlogAttrs(t *testing.T) {
	// Empty notification: no body, no tags, no fields => no attrs.
	if got := slogAttrs(Notification{Title: "t"}); len(got) != 0 {
		t.Errorf("slogAttrs of a bare notification = %v, want none", got)
	}

	n := Notification{
		Title:  "t",
		Body:   "the body",
		Tags:   []string{"one", "two"},
		Fields: map[string]string{"svc": "kimai"},
	}
	attrs := slogAttrs(n)
	byKey := map[string]string{}
	for _, a := range attrs {
		byKey[a.Key] = a.Value.String()
	}
	if byKey["body"] != "the body" {
		t.Errorf("body attr = %q, want %q", byKey["body"], "the body")
	}
	if byKey["tags"] != "one,two" {
		t.Errorf("tags attr = %q, want %q", byKey["tags"], "one,two")
	}
	if byKey["svc"] != "kimai" {
		t.Errorf("field attr svc = %q, want %q", byKey["svc"], "kimai")
	}
}

// TestSlogBackendEmitsRecord proves the slog-backed backend emits a structured
// record at the mapped level, carrying the title as the message, and never
// errors. A JSON handler over a buffer lets the test read the emitted record
// back.
func TestSlogBackendEmitsRecord(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	b := NewSlogBackend(logger)
	if b.Name() != "log" {
		t.Errorf("Name() = %q, want \"log\"", b.Name())
	}

	n := Notification{Title: "enforcement failed", Body: "drop applied", Level: LevelError, Fields: map[string]string{"svc": "berm"}}
	if err := b.Send(context.Background(), n); err != nil {
		t.Fatalf("Send returned error, the floor must never error: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("emitted record is not valid JSON (%v): %s", err, buf.String())
	}
	if rec["msg"] != "enforcement failed" {
		t.Errorf("record msg = %v, want title \"enforcement failed\"", rec["msg"])
	}
	if rec["level"] != "ERROR" {
		t.Errorf("record level = %v, want ERROR", rec["level"])
	}
	if rec["body"] != "drop applied" {
		t.Errorf("record body = %v, want \"drop applied\"", rec["body"])
	}
	if rec["svc"] != "berm" {
		t.Errorf("record field svc = %v, want \"berm\"", rec["svc"])
	}
}

// TestNewLogBackendFromSettings proves the registered "log" factory builds a
// usable, non-erroring backend for each output selector, including the default
// (slog) branch for an absent or unrecognized value. It exercises the switch
// that decides which floor a Config falls back to.
func TestNewLogBackendFromSettings(t *testing.T) {
	for _, out := range []string{"stdout", "stderr", "", "bogus"} {
		b, err := newLogBackendFromSettings(map[string]string{"output": out}, nil)
		if err != nil {
			t.Fatalf("output=%q: factory returned error: %v", out, err)
		}
		if b == nil {
			t.Fatalf("output=%q: factory returned nil backend", out)
		}
		if b.Name() != "log" {
			t.Errorf("output=%q: Name() = %q, want \"log\"", out, b.Name())
		}
		if err := b.Send(context.Background(), Notification{Title: "x", Level: LevelInfo}); err != nil {
			t.Errorf("output=%q: Send errored, the floor must never error: %v", out, err)
		}
	}
}
