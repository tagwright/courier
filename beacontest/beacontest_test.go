// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacontest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tagwright/beacon"
	"github.com/tagwright/beacon/beacontest"
)

func TestCaptureRecordsNotifications(t *testing.T) {
	ctx := context.Background()
	b, cap := beacontest.New(beacon.LevelInfo)

	if err := b.Notify(ctx, beacon.Notification{Title: "Backup failed", Body: "service kimai", Level: beacon.LevelError}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if err := b.Notify(ctx, beacon.Notification{Title: "routine", Level: beacon.LevelInfo}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if cap.Count() != 2 {
		t.Fatalf("want 2 captured, got %d", cap.Count())
	}
	if !cap.FiredAtLevel(beacon.LevelError) {
		t.Fatal("expected an Error-level notification to have fired")
	}
	if !cap.Contains(beacon.LevelError, "kimai") {
		t.Fatal("expected an Error-level alert mentioning kimai")
	}
	if lvl, ok := cap.HighestLevel(); !ok || lvl != beacon.LevelError {
		t.Fatalf("HighestLevel: want error, got %v (ok=%v)", lvl, ok)
	}
}

func TestMinLevelFilters(t *testing.T) {
	ctx := context.Background()
	// Capture only warnings and above: an Info notification must not be routed
	// to the capture channel (beacon.Notify filters by the channel MinLevel).
	b, cap := beacontest.New(beacon.LevelWarning)

	_ = b.Notify(ctx, beacon.Notification{Title: "info", Level: beacon.LevelInfo})
	if cap.Count() != 0 {
		t.Fatalf("info below MinLevel should not be captured, got %d", cap.Count())
	}
	_ = b.Notify(ctx, beacon.Notification{Title: "warn", Level: beacon.LevelWarning})
	if cap.Count() != 1 {
		t.Fatalf("warning should be captured, got %d", cap.Count())
	}
}

func TestSendErrSurfaces(t *testing.T) {
	ctx := context.Background()
	b, cap := beacontest.New(beacon.LevelInfo)
	cap.SendErr = errors.New("channel down")

	err := b.Notify(ctx, beacon.Notification{Title: "x", Level: beacon.LevelError})
	if err == nil {
		t.Fatal("expected Notify to surface the channel error")
	}
	// The notification is still recorded even when delivery fails.
	if cap.Count() != 1 {
		t.Fatalf("want the notification recorded despite SendErr, got %d", cap.Count())
	}
}

func TestIndependentCaptures(t *testing.T) {
	ctx := context.Background()
	b1, c1 := beacontest.New(beacon.LevelInfo)
	_, c2 := beacontest.New(beacon.LevelInfo)

	_ = b1.Notify(ctx, beacon.Notification{Title: "only b1", Level: beacon.LevelError})
	if c1.Count() != 1 || c2.Count() != 0 {
		t.Fatalf("captures leaked into each other: c1=%d c2=%d", c1.Count(), c2.Count())
	}
}
