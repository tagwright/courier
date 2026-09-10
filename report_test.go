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

// capSink is a capturing courier.TelemetrySink: it records every Health it is
// Reported and, when err is set, fails delivery (recording the result first).
// It is the telemetry-side counterpart to beacontest.Capture, kept local
// because Beacon.Report fan-out is what these tests exercise and beacontest
// wires only a notification backend, not a sink. It touches no exported API
// beyond the ones a real sink implementation already uses.
type capSink struct {
	mu   sync.Mutex
	name string
	got  []courier.Health
	err  error // returned by Report when set; the result is still recorded
}

func (s *capSink) Name() string { return s.name }

func (s *capSink) Report(_ context.Context, h courier.Health) error {
	s.mu.Lock()
	s.got = append(s.got, h)
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *capSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func (s *capSink) last() (courier.Health, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) == 0 {
		return courier.Health{}, false
	}
	return s.got[len(s.got)-1], true
}

// capSinkType is the registered sink type the tests build through courier.New.
// A package-qualified name keeps it from colliding with any real sink type.
const capSinkType = "beacon_test.capsink"

var (
	capMu    sync.Mutex
	capSinks = map[string]*capSink{}
	capSeq   int
)

// init registers a sink factory that hands back the test-controlled capSink
// named by the "id" setting, so a Config's Telemetry entry resolves to a sink
// the test still holds a reference to. This mirrors how beacontest routes a
// factory-built backend back to its Capture, and it is the only way to observe
// Report fan-out without adding a test-only construction path to beacon itself.
func init() {
	courier.RegisterSink(capSinkType, func(settings map[string]string, _ courier.SecretResolver) (courier.TelemetrySink, error) {
		id := settings["id"]
		capMu.Lock()
		s := capSinks[id]
		capMu.Unlock()
		if s == nil {
			return nil, fmt.Errorf("beacon_test: no capsink registered for id %q", id)
		}
		return s, nil
	})
}

// newReportBeacon builds a *courier.Beacon whose Telemetry sinks are exactly the
// given capSinks, in order, through the ordinary courier.New path.
func newReportBeacon(t *testing.T, sinks ...*capSink) *courier.Beacon {
	t.Helper()
	tcs := make([]courier.TelemetryConfig, 0, len(sinks))
	for _, s := range sinks {
		capMu.Lock()
		capSeq++
		id := fmt.Sprintf("sink-%d", capSeq)
		capSinks[id] = s
		capMu.Unlock()
		tcs = append(tcs, courier.TelemetryConfig{Type: capSinkType, Settings: map[string]string{"id": id}})
	}
	b, err := courier.New(courier.Config{Telemetry: tcs}, nil)
	if err != nil {
		t.Fatalf("courier.New: %v", err)
	}
	return b
}

// TestReportFansOutToAllSinks proves a single Report reaches every configured
// telemetry sink, carrying the Health through unchanged. A dropped telemetry
// push is silent by nature (no operator sees a health report that never
// arrived), so proving the fan-out reaches all sinks is the guard against it.
func TestReportFansOutToAllSinks(t *testing.T) {
	ctx := context.Background()
	s1 := &capSink{name: "sink-a"}
	s2 := &capSink{name: "sink-b"}
	b := newReportBeacon(t, s1, s2)

	h := courier.Health{Name: "kimai-backup", OK: false, Message: "restic exited 1"}
	if err := b.Report(ctx, h); err != nil {
		t.Fatalf("Report: %v", err)
	}

	for _, s := range []*capSink{s1, s2} {
		if s.count() != 1 {
			t.Fatalf("%s: want 1 health reported, got %d", s.name, s.count())
		}
		got, _ := s.last()
		if got != h {
			t.Errorf("%s: reported health = %+v, want %+v", s.name, got, h)
		}
	}
}

// TestReportOneSinkFailingStillAttemptsOthers proves Report does not stop at the
// first failing sink: a healthy sink further down the slice still receives the
// push, and the failure is surfaced as a non-nil error rather than swallowed.
func TestReportOneSinkFailingStillAttemptsOthers(t *testing.T) {
	ctx := context.Background()
	failing := &capSink{name: "failing", err: errors.New("sink down")}
	healthy := &capSink{name: "healthy"}
	b := newReportBeacon(t, failing, healthy)

	err := b.Report(ctx, courier.Health{Name: "svc", OK: true})
	if err == nil {
		t.Fatal("expected Report to surface the failing sink's error")
	}
	if failing.count() != 1 {
		t.Errorf("failing sink should still record the attempt, got %d", failing.count())
	}
	if healthy.count() != 1 {
		t.Errorf("a failing sink must not stop the others: healthy sink got %d, want 1", healthy.count())
	}
}

// TestReportJoinsSinkErrors proves that when more than one sink fails, Report
// combines their errors via errors.Join, so no single failure masks another.
func TestReportJoinsSinkErrors(t *testing.T) {
	ctx := context.Background()
	err1 := errors.New("first sink boom")
	err2 := errors.New("second sink boom")
	s1 := &capSink{name: "one", err: err1}
	s2 := &capSink{name: "two", err: err2}
	b := newReportBeacon(t, s1, s2)

	err := b.Report(ctx, courier.Health{Name: "svc"})
	if err == nil {
		t.Fatal("expected a combined error when both sinks fail")
	}
	if !errors.Is(err, err1) {
		t.Errorf("combined error does not wrap the first sink's error: %v", err)
	}
	if !errors.Is(err, err2) {
		t.Errorf("combined error does not wrap the second sink's error: %v", err)
	}
}

// TestReportNoSinks proves Report is a no-op returning nil when no telemetry
// sink is configured, matching the documented contract.
func TestReportNoSinks(t *testing.T) {
	b := newReportBeacon(t)
	if err := b.Report(context.Background(), courier.Health{Name: "svc"}); err != nil {
		t.Fatalf("Report with no sinks should return nil, got %v", err)
	}
}
