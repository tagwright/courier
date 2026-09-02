// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

func init() {
	RegisterBackend("log", newLogBackendFromSettings)
}

// newLogBackendFromSettings builds the registered "log" backend. Settings
// accepts an optional "output" of "stdout" or "stderr" to write plain
// lines directly to that stream; any other value, including an absent
// one, uses slog.Default() instead.
func newLogBackendFromSettings(settings map[string]string, _ SecretResolver) (Backend, error) {
	switch settings["output"] {
	case "stdout":
		return NewLogBackend(os.Stdout), nil
	case "stderr":
		return NewLogBackend(os.Stderr), nil
	default:
		return NewSlogBackend(slog.Default()), nil
	}
}

// LogBackend is the built-in, always-available notification backend. It
// writes notifications either as plain lines to an io.Writer or as
// structured records through log/slog, and depends on nothing outside the
// standard library. It is registered under the type "log", and is the
// floor every beacon.Config can fall back to when no other channel is
// configured or reachable.
type LogBackend struct {
	out    io.Writer
	logger *slog.Logger
}

// NewLogBackend returns a LogBackend that writes one formatted line per
// notification to w.
func NewLogBackend(w io.Writer) *LogBackend {
	return &LogBackend{out: w}
}

// NewSlogBackend returns a LogBackend that emits each notification as a
// structured record through logger, at a level matching Notification.Level.
func NewSlogBackend(logger *slog.Logger) *LogBackend {
	return &LogBackend{logger: logger}
}

// Name returns "log".
func (l *LogBackend) Name() string {
	return "log"
}

// Send writes n to the configured writer or slog logger. It never returns
// an error: the log backend is the always-on floor and is not allowed to
// be the reason a notification is lost.
func (l *LogBackend) Send(ctx context.Context, n Notification) error {
	if l.logger != nil {
		l.logger.LogAttrs(ctx, slogLevel(n.Level), n.Title, slogAttrs(n)...)
		return nil
	}
	fmt.Fprintln(l.out, formatLine(n))
	return nil
}

func slogLevel(l Level) slog.Level {
	switch l {
	case LevelWarning:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func slogAttrs(n Notification) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(n.Fields)+2)
	if n.Body != "" {
		attrs = append(attrs, slog.String("body", n.Body))
	}
	if len(n.Tags) > 0 {
		attrs = append(attrs, slog.String("tags", strings.Join(n.Tags, ",")))
	}
	for k, v := range n.Fields {
		attrs = append(attrs, slog.String(k, v))
	}
	return attrs
}

func formatLine(n Notification) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", strings.ToUpper(n.Level.String()), n.Title)
	if n.Body != "" {
		fmt.Fprintf(&b, ": %s", n.Body)
	}
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, " (tags=%s)", strings.Join(n.Tags, ","))
	}
	for k, v := range n.Fields {
		fmt.Fprintf(&b, " %s=%s", k, v)
	}
	return b.String()
}
