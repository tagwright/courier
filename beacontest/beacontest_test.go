// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacontest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tagwright/courier"
	"github.com/tagwright/courier/beacontest"
)

func TestCaptureRecordsNotifications(t *testing.T) {
	ctx := context.Background()
	b, cap := beacontest.New(courier.LevelInfo)

	if err := b.Notify(ctx, courier.Notification{Title: "Backup failed", Body: "service kimai", Level: courier.LevelError}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if err := b.Notify(ctx, courier.Notification{Title: "routine", Level: courier.LevelInfo}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if cap.Count() != 2 {
		t.Fatalf("want 2 captured, got %d", cap.Count())
	}
	if !cap.FiredAtLevel(courier.LevelError) {
		t.Fatal("expected an Error-level notification to have fired")
	}
	if !cap.Contains(courier.LevelError, "kimai") {
		t.Fatal("expected an Error-level alert mentioning kimai")
	}
	if lvl, ok := cap.HighestLevel(); !ok || lvl != courier.LevelError {
		t.Fatalf("HighestLevel: want error, got %v (ok=%v)", lvl, ok)
	}
}

func TestMinLevelFilters(t *testing.T) {
	ctx := context.Background()
	// Capture only warnings and above: an Info notification must not be routed
	// to the capture channel (courier.Notify filters by the channel MinLevel).
	b, cap := beacontest.New(courier.LevelWarning)

	_ = b.Notify(ctx, courier.Notification{Title: "info", Level: courier.LevelInfo})
	if cap.Count() != 0 {
		t.Fatalf("info below MinLevel should not be captured, got %d", cap.Count())
	}
	_ = b.Notify(ctx, courier.Notification{Title: "warn", Level: courier.LevelWarning})
	if cap.Count() != 1 {
		t.Fatalf("warning should be captured, got %d", cap.Count())
	}
}

func TestSendErrSurfaces(t *testing.T) {
	ctx := context.Background()
	b, cap := beacontest.New(courier.LevelInfo)
	cap.SendErr = errors.New("channel down")

	err := b.Notify(ctx, courier.Notification{Title: "x", Level: courier.LevelError})
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
	b1, c1 := beacontest.New(courier.LevelInfo)
	_, c2 := beacontest.New(courier.LevelInfo)

	_ = b1.Notify(ctx, courier.Notification{Title: "only b1", Level: courier.LevelError})
	if c1.Count() != 1 || c2.Count() != 0 {
		t.Fatalf("captures leaked into each other: c1=%d c2=%d", c1.Count(), c2.Count())
	}
}
