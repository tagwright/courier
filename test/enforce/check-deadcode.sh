#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Copyright (C) 2026 techgaud
#
# Dead-code gate (Testing Standard enforceability layer, task #552).
#
# Runs golang.org/x/tools/cmd/deadcode (pinned) and fails on any function the
# analyzer reports as unreachable. This machine-checks the built-but-not-wired
# class directly (the Billet lesson). Zero tolerance, no allowlist.
#
# The root is AUTO-DETECTED, never a per-repo flag (a flag is something someone
# flips to get green): the script asks `go list` whether a `main` package
# exists, exactly as the standard specifies.
#
#   - Daemon module (a main exists): root at the production main, `deadcode
#     ./...`. Never -test here; it would hide the built-but-not-wired class the
#     daemon gate exists to catch.
#   - Library module (no main: core, courier): root at the test binaries,
#     `deadcode -test ./...`. A library's only attestable entrypoint is its
#     test suite, so rooting there is the honest analogue of rooting at main
#     ("a library owes its own coverage"). Plain `deadcode ./...` on a library
#     fails closed on "no main packages", so -test is mandatory, not optional.
#     courier is a library (module github.com/tagwright/courier, no main), so
#     this gate roots at -test here.
#
# Needs the Go toolchain and network for the pinned tool; runs in golang:1.25.
set -euo pipefail

# Pin the analyzer version. Bump deliberately, never float, so a toolchain or
# analyzer change is a reviewed edit and not a silent behavior shift. One
# analyzer pin suite-wide (v0.38.0 needs Go >= 1.24; courier's go.mod is 1.25).
DEADCODE_VERSION="v0.38.0"

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
cd "$repo"

# Auto-detect the root. A non-empty result means a main package exists (daemon);
# empty means a library. 2>/dev/null so a cold-cache "go: downloading" line on
# stderr never contaminates the emptiness test.
mains="$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./... 2>/dev/null || true)"
if [ -n "$mains" ]; then
  root_args="./..."
  root_desc="production main (daemon module)"
else
  root_args="-test ./..."
  root_desc="test binaries (library module, no main)"
fi
echo "deadcode: rooting at ${root_desc}"

# Capture findings (stdout) separately from download noise and real errors
# (stderr). On a cold module cache the analyzer prints "go: downloading ..." to
# stderr, so folding stderr into the findings would false-fail a clean tree.
# A nonzero exit is a real analyzer failure, distinct from a clean run.
errf="$(mktemp)"
# shellcheck disable=SC2086
if out="$(go run "golang.org/x/tools/cmd/deadcode@${DEADCODE_VERSION}" $root_args 2>"$errf")"; then rc=0; else rc=$?; fi
if [ "$rc" -ne 0 ]; then
  echo "FAIL: deadcode analyzer failed to run (exit $rc):" >&2
  cat "$errf" >&2
  rm -f "$errf"
  exit 1
fi
rm -f "$errf"
if [ -n "$out" ]; then
  echo "FAIL: deadcode found unreachable code:" >&2
  printf '%s\n' "$out" >&2
  echo "      Every library finding is actionable. An unexported hit is a dead" >&2
  echo "      helper (delete or wire); an exported hit is untested API surface" >&2
  echo "      owed a test; a _test.go hit is dead scaffolding (delete it, or if a" >&2
  echo "      helper serves only a build-tagged test, move it behind that tag so" >&2
  echo "      it lives in the same partition as its callers)." >&2
  exit 1
fi
echo "deadcode: OK (no unreachable code)"
