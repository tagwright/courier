// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/tagwright/courier"
)

// These tests exercise Beacon.Notify's multi-channel fan-out (task #617): the
// documented contract is that one channel failing still ATTEMPTS the others and
// every failure lands in the errors.Join result, never swallowed. beacontest.New
// wires only a single capture channel, so, like report_test.go's capSink for the
// telemetry side, these build their own multi-channel Beacon through the ordinary
// courier.New path with an independent fault knob per channel.

// capChannel is a capturing courier.Backend with a per-instance name and an
// optional delivery error, so a test can fail channel A and still observe that
// channel B received the notification. The result is recorded before err is
// returned, so a failing channel still proves it was attempted.
type capChannel struct {
	mu   sync.Mutex
	name string
	got  []courier.Notification
	err  error // returned by Send when set; the notification is still recorded
}

func (c *capChannel) Name() string { return c.name }

func (c *capChannel) Send(_ context.Context, n courier.Notification) error {
	c.mu.Lock()
	c.got = append(c.got, n)
	err := c.err
	c.mu.Unlock()
	return err
}

func (c *capChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func (c *capChannel) last() (courier.Notification, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.got) == 0 {
		return courier.Notification{}, false
	}
	return c.got[len(c.got)-1], true
}

// capChanType is the registered backend type the tests build through courier.New.
// A package-qualified name keeps it from colliding with any real channel type.
const capChanType = "beacon_test.capchan"

var (
	capChanMu   sync.Mutex
	capChannels = map[string]*capChannel{}
	capChanSeq  int
)

// init registers a backend factory that hands back the test-controlled
// capChannel named by the "id" setting, mirroring how beacontest and report_test
// route a factory-built backend back to the instance the test still holds.
func init() {
	courier.RegisterBackend(capChanType, func(settings map[string]string, _ courier.SecretResolver) (courier.Backend, error) {
		id := settings["id"]
		capChanMu.Lock()
		c := capChannels[id]
		capChanMu.Unlock()
		if c == nil {
			return nil, fmt.Errorf("beacon_test: no capchan registered for id %q", id)
		}
		return c, nil
	})
}

// newNotifyBeacon builds a *courier.Beacon whose channels are exactly the given
// capChannels, in order, all at LevelInfo (so every notification is eligible),
// through the ordinary courier.New path.
func newNotifyBeacon(t *testing.T, chans ...*capChannel) *courier.Beacon {
	t.Helper()
	ccs := make([]courier.ChannelConfig, 0, len(chans))
	for _, c := range chans {
		capChanMu.Lock()
		capChanSeq++
		id := fmt.Sprintf("chan-%d", capChanSeq)
		capChannels[id] = c
		capChanMu.Unlock()
		ccs = append(ccs, courier.ChannelConfig{
			Type:     capChanType,
			MinLevel: courier.LevelInfo,
			Settings: map[string]string{"id": id},
		})
	}
	b, err := courier.New(courier.Config{Channels: ccs}, nil)
	if err != nil {
		t.Fatalf("courier.New: %v", err)
	}
	return b
}

// TestNotifyFansOutToAllChannels proves a single Notify reaches every eligible
// channel, carrying the Notification through unchanged.
func TestNotifyFansOutToAllChannels(t *testing.T) {
	ctx := context.Background()
	c1 := &capChannel{name: "chan-a"}
	c2 := &capChannel{name: "chan-b"}
	b := newNotifyBeacon(t, c1, c2)

	n := courier.Notification{Title: "backup failed", Body: "restic exited 1", Level: courier.LevelError}
	if err := b.Notify(ctx, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	for _, c := range []*capChannel{c1, c2} {
		if c.count() != 1 {
			t.Fatalf("%s: want 1 notification, got %d", c.name, c.count())
		}
		got, _ := c.last()
		if got.Title != n.Title || got.Body != n.Body || got.Level != n.Level {
			t.Errorf("%s: notification = %+v, want %+v", c.name, got, n)
		}
	}
}

// TestNotifyOneChannelFailingStillAttemptsOthers is the core #617 assertion: a
// failing channel A must not stop channel B, and the failure is SURFACED as a
// non-nil error, not swallowed. A dropped alert that reports success is the exact
// silent-failure class this suite exists to catch.
func TestNotifyOneChannelFailingStillAttemptsOthers(t *testing.T) {
	ctx := context.Background()
	failing := &capChannel{name: "failing", err: errors.New("channel down")}
	healthy := &capChannel{name: "healthy"}
	b := newNotifyBeacon(t, failing, healthy)

	err := b.Notify(ctx, courier.Notification{Title: "alert", Level: courier.LevelError})
	if err == nil {
		t.Fatal("Notify swallowed a channel failure; a failed alert reported success")
	}
	if failing.count() != 1 {
		t.Errorf("failing channel should still record the attempt, got %d", failing.count())
	}
	if healthy.count() != 1 {
		t.Errorf("a failing channel must not stop the others: healthy channel got %d, want 1", healthy.count())
	}
	if !errors.Is(err, failing.err) {
		t.Errorf("surfaced error %q does not wrap the failing channel's error", err)
	}
}

// TestNotifyJoinsChannelErrors proves that when more than one channel fails,
// Notify combines their errors via errors.Join, so no single failure masks
// another (the documented contract).
func TestNotifyJoinsChannelErrors(t *testing.T) {
	ctx := context.Background()
	err1 := errors.New("first channel boom")
	err2 := errors.New("second channel boom")
	c1 := &capChannel{name: "one", err: err1}
	c2 := &capChannel{name: "two", err: err2}
	b := newNotifyBeacon(t, c1, c2)

	err := b.Notify(ctx, courier.Notification{Title: "alert", Level: courier.LevelError})
	if err == nil {
		t.Fatal("expected a combined error when both channels fail")
	}
	if !errors.Is(err, err1) {
		t.Errorf("combined error does not wrap the first channel's error: %v", err)
	}
	if !errors.Is(err, err2) {
		t.Errorf("combined error does not wrap the second channel's error: %v", err)
	}
	if c1.count() != 1 || c2.count() != 1 {
		t.Errorf("both channels should be attempted: one=%d two=%d", c1.count(), c2.count())
	}
}

// TestNotifyRespectsMinLevelDuringFanOut proves the level gate is applied per
// channel during fan-out: a below-threshold channel is skipped (never sent, so a
// failing one below threshold cannot even contribute an error), while an eligible
// channel still receives the notification.
func TestNotifyRespectsMinLevelDuringFanOut(t *testing.T) {
	ctx := context.Background()
	// belowThreshold would error if it were ever sent, so if the gate leaks we
	// would see both a spurious delivery and a spurious error.
	belowThreshold := &capChannel{name: "warn-only", err: errors.New("must not be reached")}
	eligible := &capChannel{name: "info-ok"}

	capChanMu.Lock()
	capChanSeq++
	idBelow := fmt.Sprintf("chan-%d", capChanSeq)
	capChannels[idBelow] = belowThreshold
	capChanSeq++
	idEligible := fmt.Sprintf("chan-%d", capChanSeq)
	capChannels[idEligible] = eligible
	capChanMu.Unlock()

	b, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{
		{Type: capChanType, MinLevel: courier.LevelWarning, Settings: map[string]string{"id": idBelow}},
		{Type: capChanType, MinLevel: courier.LevelInfo, Settings: map[string]string{"id": idEligible}},
	}}, nil)
	if err != nil {
		t.Fatalf("courier.New: %v", err)
	}

	if err := b.Notify(ctx, courier.Notification{Title: "fyi", Level: courier.LevelInfo}); err != nil {
		t.Fatalf("Notify surfaced an error for a skipped below-threshold channel: %v", err)
	}
	if belowThreshold.count() != 0 {
		t.Errorf("below-threshold channel received a notification it should have been gated out of: %d", belowThreshold.count())
	}
	if eligible.count() != 1 {
		t.Errorf("eligible channel should have received the notification, got %d", eligible.count())
	}
}

// TestNotifyNoEligibleChannels proves Notify returns nil when no channel is at or
// below the notification's level, matching the documented "nil if no channel was
// eligible" contract.
func TestNotifyNoEligibleChannels(t *testing.T) {
	ctx := context.Background()
	c := &capChannel{name: "error-only", err: errors.New("must not be reached")}

	capChanMu.Lock()
	capChanSeq++
	id := fmt.Sprintf("chan-%d", capChanSeq)
	capChannels[id] = c
	capChanMu.Unlock()

	b, err := courier.New(courier.Config{Channels: []courier.ChannelConfig{
		{Type: capChanType, MinLevel: courier.LevelError, Settings: map[string]string{"id": id}},
	}}, nil)
	if err != nil {
		t.Fatalf("courier.New: %v", err)
	}

	if err := b.Notify(ctx, courier.Notification{Title: "fyi", Level: courier.LevelInfo}); err != nil {
		t.Fatalf("Notify with no eligible channel should return nil, got %v", err)
	}
	if c.count() != 0 {
		t.Errorf("no channel should have been sent to, got %d", c.count())
	}
}
