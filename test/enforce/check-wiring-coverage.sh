#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Copyright (C) 2026 techgaud
#
# Coverage floor (Testing Standard enforceability layer, #552).
#
# For each "<package> <floor-percent>" line in coverage-floor.txt, runs the
# package's unit tests with coverage and fails if statement coverage is below
# the floor. The floor is a ratchet: raise it as coverage climbs, never lower
# it to pass.
#
# On a DAEMON this is scoped to the wiring packages (internal/daemon and the
# like), never a global percentage. courier is a LIBRARY with no daemon wiring:
# the standard's "a library owes its own coverage" rule applies, so the floor
# covers the library's own meaningful packages (the root courier package and
# beacontest) at their measured coverage. This is still not a global percentage:
# each package is named and floored individually, test-only scaffolding
# (test/integration) is excluded, and a floor only ever rises.
#
# Package-name convention: a package path is repo-relative. A subpackage is a
# path like "beacontest" and is tested as "./beacontest/...". The flat root
# package is written as "." and is tested as "." exactly -- NOT "././..." --
# because appending /... would sweep the whole module (every package) and the
# first coverage line grabbed would not be the root's. This "." special-case is
# the one library adaptation over the daemon exemplar's script.
#
# Needs the Go toolchain and the module cache; runs in the repo golang:1.25.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
floor_file="$here/coverage-floor.txt"

cd "$repo"
fail=0
checked=0

while IFS= read -r line; do
  # Strip comments and skip blank lines.
  line="${line%%#*}"
  pkg="$(printf '%s' "$line" | awk '{print $1}')"
  floor="$(printf '%s' "$line" | awk '{print $2}')"
  [ -z "$pkg" ] && continue
  if ! printf '%s' "$floor" | grep -qE '^[0-9]+$'; then
    echo "coverage: bad floor for '$pkg' in $floor_file: '$floor'" >&2
    exit 2
  fi
  checked=$((checked + 1))

  # The root package is tested as "." exactly; a subpackage recurses with /...
  if [ "$pkg" = "." ]; then
    target="."
  else
    target="./$pkg/..."
  fi

  # -count=1 forces a fresh run so a cached result never suppresses the
  # coverage line, keeping the measured number deterministic run to run.
  out="$(go test -count=1 -cover "$target" 2>&1)" || { echo "$out" >&2; echo "FAIL: tests failed for $pkg" >&2; fail=1; continue; }
  # Extract "coverage: NN.N% of statements".
  pct="$(printf '%s\n' "$out" | grep -oE 'coverage: [0-9]+\.[0-9]+%' | head -n1 | grep -oE '[0-9]+\.[0-9]+')"
  if [ -z "$pct" ]; then
    echo "$out" >&2
    echo "FAIL: could not read a coverage percentage for $pkg (no coverable tests?)" >&2
    fail=1
    continue
  fi

  if awk -v p="$pct" -v f="$floor" 'BEGIN{exit !(p < f)}'; then
    echo "FAIL: $pkg coverage ${pct}% is below floor ${floor}%" >&2
    fail=1
  else
    echo "coverage: $pkg ${pct}% >= ${floor}%  OK"
  fi
done < "$floor_file"

if [ "$checked" -eq 0 ]; then
  echo "coverage: no packages listed in $floor_file" >&2
  exit 2
fi
[ "$fail" -eq 0 ] && echo "coverage: OK"
exit "$fail"
