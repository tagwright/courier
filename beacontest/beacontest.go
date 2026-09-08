// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

// Package beacontest provides Capture, a capturing beacon.Backend, and New, a
// *beacon.Beacon wired to it. It is the notifier-side counterpart to
// core/runtime/runtimetest: where runtimetest lets a wiring test inject a
// runtime failure, beacontest lets that same test assert the failure ALSO
// fired the operator alert it is contracted to fire.
//
// Use it only on failure paths a tool is contracted to alert on (a backup
// failure, an enforcement failure, and the like); the run record and exit code
// stay the primary surface, and a notify assertion is additive, not a blanket
// rule on every wiring test.
//
// It leaves beacon's exported API untouched: a capture registers as an ordinary
// backend type and New builds an ordinary *beacon.Beacon through beacon.New, so
// the code under test receives exactly the *beacon.Beacon it would in
// production, with no test-only construction path bolted onto the library.
package beacontest

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tagwright/beacon"
)

// backendType is the registered beacon backend type New wires up. It carries a
// package-qualified name so it can never collide with a real channel type.
const backendType = "beacontest.capture"

var (
	mu       sync.Mutex
	captures = map[string]*Capture{}
	seq      int
)

func init() {
	beacon.RegisterBackend(backendType, func(settings map[string]string, _ beacon.SecretResolver) (beacon.Backend, error) {
		id := settings["id"]
		mu.Lock()
		c := captures[id]
		mu.Unlock()
		if c == nil {
			return nil, fmt.Errorf("beacontest: no capture registered for id %q", id)
		}
		return c, nil
	})
}

// Capture is a beacon.Backend that records every Notification it is sent, in
// order, so a test can assert which alerts fired and at what level. Set SendErr
// to make the channel itself fail, exercising how a caller handles a notify
// error (a failure to alert is its own bug class).
type Capture struct {
	mu  sync.Mutex
	got []beacon.Notification

	// SendErr, when set, is returned by every Send, simulating a channel that
	// cannot deliver. The notification is still recorded first.
	SendErr error
}

// Name identifies the backend, as beacon.Backend requires.
func (c *Capture) Name() string { return backendType }

// Send records n, then returns SendErr (nil unless the test set it).
func (c *Capture) Send(_ context.Context, n beacon.Notification) error {
	c.mu.Lock()
	c.got = append(c.got, n)
	err := c.SendErr
	c.mu.Unlock()
	return err
}

// Notifications returns a copy of every notification captured so far, in order.
func (c *Capture) Notifications() []beacon.Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]beacon.Notification(nil), c.got...)
}

// Count returns how many notifications have been captured.
func (c *Capture) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

// HighestLevel returns the most severe level among captured notifications, and
// ok=false if none were captured.
func (c *Capture) HighestLevel() (level beacon.Level, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.got) == 0 {
		return 0, false
	}
	h := c.got[0].Level
	for _, n := range c.got[1:] {
		if n.Level > h {
			h = n.Level
		}
	}
	return h, true
}

// FiredAtLevel reports whether any captured notification is at exactly level,
// the "an Error-level alert fired" assertion in its simplest form.
func (c *Capture) FiredAtLevel(level beacon.Level) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.got {
		if n.Level == level {
			return true
		}
	}
	return false
}

// Contains reports whether any captured notification at or above level has
// substr in its Title or Body, for "an error alert mentioning X fired".
func (c *Capture) Contains(level beacon.Level, substr string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.got {
		if n.Level >= level && (strings.Contains(n.Title, substr) || strings.Contains(n.Body, substr)) {
			return true
		}
	}
	return false
}

// New builds a *beacon.Beacon that captures every notification at or above
// minLevel into the returned Capture. Pass beacon.LevelInfo to capture all
// levels. The returned Beacon is an ordinary one built through beacon.New, so
// the code under test cannot tell it from production.
func New(minLevel beacon.Level) (*beacon.Beacon, *Capture) {
	c := &Capture{}

	mu.Lock()
	seq++
	id := fmt.Sprintf("cap-%d", seq)
	captures[id] = c
	mu.Unlock()

	b, err := beacon.New(beacon.Config{Channels: []beacon.ChannelConfig{{
		Type:     backendType,
		MinLevel: minLevel,
		Settings: map[string]string{"id": id},
	}}}, nil)
	if err != nil {
		// The only way beacon.New fails here is an unregistered backend type,
		// but this package's init registers it, so a failure is a bug in
		// beacontest itself, not something a caller can cause.
		panic("beacontest: building capture beacon: " + err.Error())
	}
	return b, c
}
