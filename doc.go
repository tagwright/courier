// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

// Package courier is a small library for sending notifications and pushing
// telemetry: alerts to the channels people watch, and health and status to a
// monitor. It imports nothing application-specific, and secret resolution is
// injected by the host program.
//
// This library was formerly named beacon. The name "beacon" now belongs to a
// forthcoming label-driven notification service, and courier is the delivery
// library that service will use to send messages.
//
// The API is under construction.
package courier
