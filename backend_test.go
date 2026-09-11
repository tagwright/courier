// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tagwright/courier"
)

// These tests are the fail-closed guard for the backend/sink registry (task
// #616): courier.New must REJECT a Config that names a Type nobody registered,
// never silently drop the channel and hand back a Beacon that looks fine while
// alerts go nowhere. A label typo is the silent failure mode of a config-driven
// notifier, and "fail closed, allowlists over denylists" is canon. Each test
// feeds the known violation and asserts it goes red, so the proof re-runs in CI
// rather than living in memory.

// TestNewUnknownBackendTypeFailsClosed proves an unregistered channel Type is
// rejected at construction, with an error naming the offending index and type,
// and no half-built Beacon returned.
func TestNewUnknownBackendTypeFailsClosed(t *testing.T) {
	const bogus = "backend_test.definitely-not-registered"
	b, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{
		{Type: bogus},
	}}, nil)
	if err == nil {
		t.Fatal("New accepted an unregistered backend type; a channel typo would be silently dropped")
	}
	if b != nil {
		t.Errorf("New returned a non-nil Beacon alongside an error: %v", b)
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("error %q does not name the offending type %q", err, bogus)
	}
}

// TestNewUnknownSinkTypeFailsClosed is the telemetry-side counterpart: an
// unregistered sink Type is rejected the same way.
func TestNewUnknownSinkTypeFailsClosed(t *testing.T) {
	const bogus = "backend_test.no-such-sink"
	b, err := courier.New(courier.Config{Telemetry: []courier.TelemetryConfig{
		{Type: bogus},
	}}, nil)
	if err == nil {
		t.Fatal("New accepted an unregistered sink type; a telemetry typo would be silently dropped")
	}
	if b != nil {
		t.Errorf("New returned a non-nil Beacon alongside an error: %v", b)
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("error %q does not name the offending type %q", err, bogus)
	}
}

// TestNewKnownTypeAmongUnknownStillFailsClosed proves one bad Type poisons the
// whole build: a valid channel does not paper over an unregistered one, so an
// operator cannot end up with a Beacon that quietly dropped a channel.
func TestNewKnownTypeAmongUnknownStillFailsClosed(t *testing.T) {
	const bogus = "backend_test.mixed-unknown"
	_, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{
		{Type: "log"}, // the always-registered fallback backend
		{Type: bogus},
	}}, nil)
	if err == nil {
		t.Fatal("New accepted a config with one unregistered type among valid ones")
	}
	if !strings.Contains(err.Error(), bogus) {
		t.Errorf("error %q does not name the offending type %q", err, bogus)
	}
}

// TestNewFactoryErrorSurfaces proves a registered factory returning an error
// aborts New with that error wrapped, rather than being swallowed. This is the
// other half of fail-closed: a channel that cannot be built is not silently
// omitted.
func TestNewFactoryErrorSurfaces(t *testing.T) {
	const typ = "backend_test.factory-error"
	factoryErr := errors.New("cannot build backend: no url configured")
	courier.RegisterBackend(typ, func(map[string]string, courier.SecretResolver) (courier.Backend, error) {
		return nil, factoryErr
	})

	b, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{
		{Type: typ},
	}}, nil)
	if err == nil {
		t.Fatal("New swallowed a factory error")
	}
	if b != nil {
		t.Errorf("New returned a non-nil Beacon alongside a factory error: %v", b)
	}
	if !errors.Is(err, factoryErr) {
		t.Errorf("New error %q does not wrap the factory error", err)
	}
}

// TestRegisterBackendDuplicatePanics proves two packages fighting over the same
// backend name is caught loudly at init, not resolved by last-writer-wins (which
// would let a channel silently change behavior).
func TestRegisterBackendDuplicatePanics(t *testing.T) {
	const typ = "backend_test.dup-backend"
	ctor := func(map[string]string, courier.SecretResolver) (courier.Backend, error) { return nil, nil }
	courier.RegisterBackend(typ, ctor)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering the same backend type twice did not panic")
		}
		if msg, ok := r.(string); ok && !strings.Contains(msg, typ) {
			t.Errorf("panic %q does not name the duplicated type %q", msg, typ)
		}
	}()
	courier.RegisterBackend(typ, ctor)
}

// TestRegisterSinkDuplicatePanics is the sink-side counterpart.
func TestRegisterSinkDuplicatePanics(t *testing.T) {
	const typ = "backend_test.dup-sink"
	ctor := func(map[string]string, courier.SecretResolver) (courier.TelemetrySink, error) { return nil, nil }
	courier.RegisterSink(typ, ctor)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering the same sink type twice did not panic")
		}
		if msg, ok := r.(string); ok && !strings.Contains(msg, typ) {
			t.Errorf("panic %q does not name the duplicated type %q", msg, typ)
		}
	}()
	courier.RegisterSink(typ, ctor)
}
