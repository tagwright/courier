// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"fmt"
	"sync"
)

// Backend is one notification channel: email, ntfy, Discord, a generic
// webhook, and so on. A Backend is constructed by the BackendFactory
// registered under its Type and lives for the lifetime of a Beacon.
type Backend interface {
	// Name identifies this backend instance, typically its registered type
	// (e.g. "ntfy"). Used in error messages and logs.
	Name() string

	// Send delivers a notification through this channel. Implementations
	// should resolve any secrets they need at send time, not at
	// construction time, so credential rotation does not require a
	// rebuild.
	Send(ctx context.Context, n Notification) error
}

// TelemetrySink is one telemetry target: a monitor that wants health and
// status pushed to it on a schedule or on change, such as a Gatus external
// endpoint.
type TelemetrySink interface {
	// Name identifies this sink instance, typically its registered type.
	Name() string

	// Report pushes a health result to this sink.
	Report(ctx context.Context, h Health) error
}

// BackendFactory builds a Backend from its configured settings. resolve is
// never nil; a host with no secret store still provides a resolver that
// returns an error for every name, so factories can call it unconditionally.
type BackendFactory func(settings map[string]string, resolve SecretResolver) (Backend, error)

// SinkFactory builds a TelemetrySink from its configured settings, with the
// same resolve contract as BackendFactory.
type SinkFactory func(settings map[string]string, resolve SecretResolver) (TelemetrySink, error)

var (
	registryMu   sync.RWMutex
	backendTypes = map[string]BackendFactory{}
	sinkTypes    = map[string]SinkFactory{}
)

// RegisterBackend makes a notification backend type available to Config,
// keyed by typ (e.g. "ntfy", "discord"). It is meant to be called from an
// init function in the package implementing that backend. Registering the
// same type twice panics, since it almost always indicates two packages
// fighting over the same name.
func RegisterBackend(typ string, ctor BackendFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := backendTypes[typ]; exists {
		panic(fmt.Sprintf("beacon: backend type %q already registered", typ))
	}
	backendTypes[typ] = ctor
}

// RegisterSink makes a telemetry sink type available to Config, keyed by
// typ (e.g. "gatus"). It is meant to be called from an init function in the
// package implementing that sink. Registering the same type twice panics.
func RegisterSink(typ string, ctor SinkFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := sinkTypes[typ]; exists {
		panic(fmt.Sprintf("beacon: sink type %q already registered", typ))
	}
	sinkTypes[typ] = ctor
}

func lookupBackend(typ string) (BackendFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ctor, ok := backendTypes[typ]
	return ctor, ok
}

func lookupSink(typ string) (SinkFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ctor, ok := sinkTypes[typ]
	return ctor, ok
}
