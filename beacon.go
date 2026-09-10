// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Level is the severity of a Notification, ordered from least to most
// urgent.
type Level int

const (
	// LevelInfo is routine, non-urgent information.
	LevelInfo Level = iota
	// LevelWarning is something worth a look but not on fire.
	LevelWarning
	// LevelError is something that needs attention now.
	LevelError
)

// String returns the lowercase name of the level, e.g. "warning".
func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "info"
	case LevelWarning:
		return "warning"
	case LevelError:
		return "error"
	default:
		return fmt.Sprintf("level(%d)", int(l))
	}
}

// Notification is one alert to send to the channels people watch.
type Notification struct {
	// Title is a short summary line.
	Title string
	// Body is the longer message, if any.
	Body string
	// Level is the severity, used both for display and for routing: a
	// channel only receives notifications at or above its MinLevel.
	Level Level
	// Tags are free-form labels a backend may use for routing or display
	// (e.g. topic names for ntfy).
	Tags []string
	// Fields are structured key/value details a backend may render
	// alongside the body (e.g. a service name, a duration, an exit code).
	Fields map[string]string
}

// Health is one telemetry result to push to a monitor.
type Health struct {
	// Name identifies what this health result is about, e.g. a service or
	// job name.
	Name string
	// OK reports whether the thing being monitored is healthy.
	OK bool
	// Message is a short human-readable detail, especially useful when
	// OK is false.
	Message string
	// Duration is how long the underlying operation took, if applicable.
	Duration time.Duration
}

// SecretResolver resolves a secret by name to its value. The host program
// supplies one so beacon never assumes where secrets live. A nil resolver
// is treated as "no secrets available": New replaces it with a resolver
// that returns an error for every name, so backend and sink factories can
// call resolve unconditionally.
type SecretResolver func(name string) (string, error)

func noSecrets(name string) (string, error) {
	return "", fmt.Errorf("beacon: no secret resolver configured, cannot resolve %q", name)
}

// ChannelConfig configures one notification backend.
type ChannelConfig struct {
	// Type selects the backend, matching a name passed to RegisterBackend
	// (e.g. "log", "ntfy", "discord").
	Type string
	// MinLevel is the lowest severity this channel receives. A
	// Notification below MinLevel is not sent to this channel.
	MinLevel Level
	// Settings is backend-specific configuration. Any value that names a
	// secret rather than holding it directly is resolved through the
	// SecretResolver by the backend itself, at send time, so credential
	// rotation takes effect without a rebuild or restart.
	Settings map[string]string
}

// TelemetryConfig configures one telemetry sink.
type TelemetryConfig struct {
	// Type selects the sink, matching a name passed to RegisterSink
	// (e.g. "gatus").
	Type string
	// Settings is sink-specific configuration, with the same
	// secret-naming and send-time resolution convention as
	// ChannelConfig.Settings.
	Settings map[string]string
}

// Config is the full beacon configuration: the notification channels and
// telemetry sinks to build.
type Config struct {
	Channels  []ChannelConfig
	Telemetry []TelemetryConfig
}

// channel pairs a built Backend with the MinLevel it was configured with.
type channel struct {
	backend  Backend
	minLevel Level
}

// Beacon fans a notification out to configured channels and a health
// result out to configured telemetry sinks.
type Beacon struct {
	channels []channel
	sinks    []TelemetrySink
}

// New builds a Beacon from cfg, constructing each configured channel and
// sink from the registry populated by RegisterBackend and RegisterSink.
// resolve may be nil if the host has no secrets to offer; New then uses a
// resolver that errors on every lookup.
//
// New fails if a Channels or Telemetry entry names a Type that has not
// been registered, or if a factory returns an error.
func New(cfg Config, resolve SecretResolver) (*Beacon, error) {
	if resolve == nil {
		resolve = noSecrets
	}

	b := &Beacon{
		channels: make([]channel, 0, len(cfg.Channels)),
		sinks:    make([]TelemetrySink, 0, len(cfg.Telemetry)),
	}

	for i, cc := range cfg.Channels {
		ctor, ok := lookupBackend(cc.Type)
		if !ok {
			return nil, fmt.Errorf("beacon: channel %d: unknown backend type %q", i, cc.Type)
		}
		backend, err := ctor(cc.Settings, resolve)
		if err != nil {
			return nil, fmt.Errorf("beacon: channel %d: building backend %q: %w", i, cc.Type, err)
		}
		b.channels = append(b.channels, channel{backend: backend, minLevel: cc.MinLevel})
	}

	for i, tc := range cfg.Telemetry {
		ctor, ok := lookupSink(tc.Type)
		if !ok {
			return nil, fmt.Errorf("beacon: telemetry %d: unknown sink type %q", i, tc.Type)
		}
		sink, err := ctor(tc.Settings, resolve)
		if err != nil {
			return nil, fmt.Errorf("beacon: telemetry %d: building sink %q: %w", i, tc.Type, err)
		}
		b.sinks = append(b.sinks, sink)
	}

	return b, nil
}

// Notify fans n out to every channel whose MinLevel is at or below
// n.Level. It attempts every eligible channel even if one fails, and
// returns the combined error via errors.Join (nil if all succeeded, or if
// no channel was eligible).
func (b *Beacon) Notify(ctx context.Context, n Notification) error {
	var errs []error
	for _, c := range b.channels {
		if n.Level < c.minLevel {
			continue
		}
		if err := c.backend.Send(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("beacon: %s: %w", c.backend.Name(), err))
		}
	}
	return errors.Join(errs...)
}

// Report fans h out to every configured telemetry sink. It attempts every
// sink even if one fails, and returns the combined error via errors.Join
// (nil if all succeeded, or if there are no sinks).
func (b *Beacon) Report(ctx context.Context, h Health) error {
	var errs []error
	for _, s := range b.sinks {
		if err := s.Report(ctx, h); err != nil {
			errs = append(errs, fmt.Errorf("beacon: %s: %w", s.Name(), err))
		}
	}
	return errors.Join(errs...)
}
