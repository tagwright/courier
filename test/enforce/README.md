# Enforceability layer

These are the CI-enforceable checks the [tagwright Testing Standard][std] calls
its enforceability layer (task #552). They turn the standard's rules from prose
into gates that re-run on every CI, so a rule fails the build the moment it stops
being true rather than living in someone's memory.

All of them run on the **hosted** CI leg (`.github/workflows/ci.yml`). None needs
a Docker socket or kernel privilege: they are static analysis and `go test`-level,
so they belong beside `go build` / `go vet` / `go test`. courier's own live
delivery harness (`test/integration/run.sh`) needs a Docker daemon to stand up
throwaway servers but no privilege, so it too runs on the hosted leg, not the
self-hosted runner (see `docs/TESTING.md`).

Run them all locally the same way CI does, inside the repo's `golang:1.25`
container (there is no host Go):

    docker run --rm -v "$PWD":/w -v courier-gomodcache:/go/pkg/mod -w /w \
      -e GOPRIVATE=github.com/tagwright/* -e GOFLAGS=-buildvcs=false golang:1.25 \
      sh -c 'test/enforce/run-all.sh'

## courier is a library

courier is a Go **library** module (`github.com/tagwright/courier`, no `main`
package, flat root package plus `beacontest`). Two of the checks below are
adapted for that, and the adaptations are the point of this README:

- **Dead-code rooting.** The standard roots the dead-code analyzer differently
  for a library than a daemon, and `check-deadcode.sh` here AUTO-DETECTS which,
  never a per-repo flag. It asks `go list` whether a `main` package exists:
  - a **daemon** (a `main` exists) roots at the production main, `deadcode
    ./...`, never `-test`;
  - a **library** (no `main`, courier's case) roots at its own test binaries,
    `deadcode -test ./...`. A library's only attestable entrypoint is its test
    suite, so rooting there is the honest analogue of rooting at `main` -- "a
    library owes its own coverage." Plain `deadcode ./...` on a library fails
    closed on `no main packages`, so `-test` is mandatory, not optional.

  Every library finding is actionable and was resolved the standard's way, not
  waived: an unexported hit is a dead helper (deleted or wired), an exported hit
  is untested API surface owed a test, a `_test.go` hit is dead scaffolding
  (deleted, or if it serves only a build-tagged test, moved behind that tag so
  it lives in the same partition as its callers).

- **Coverage floor.** With no daemon wiring to scope to, the floor covers the
  library's own meaningful packages (`.` the root package, and `beacontest`) at
  measured coverage, per "a library owes its own coverage." See
  `coverage-floor.txt`. It is still not a global percentage: each package is
  named and floored individually, and test-only scaffolding is excluded.

## The checks

### `check-skip-budget.sh` — the skip budget

A `t.Skip` is how a test goes green without proving anything, so the standard
caps them. This check splits skips into two buckets by build tag:

- **Always-run leg** (test files with no `//go:build integration` tag, the ones
  `go test ./...` compiles and runs on every CI): skips are banned outright,
  budget **0**, no ratchet. That leg runs in plain CI where every resource a
  test needs is present, so a skip there can only be skip-to-get-green.
- **Integration leg** (`//go:build integration` files, run by
  `test/integration/run.sh` against the throwaway servers): skips are the
  *allowed* resource-absent kind -- they skip cleanly when the servers are
  absent and run for real where they are present. Their count is capped at the
  committed budget in `skip-budget.txt`, a **ratchet**: removing a skip never
  breaks the build, and raising the budget is a reviewed change that must name
  the new resource-absent skip it admits.

### `check-wiring-coverage.sh` — the coverage floor

A per-package statement-coverage floor, committed in `coverage-floor.txt`. For a
library this covers the library's own meaningful packages (see above), not
daemon wiring. It is a ratchet: it may only rise. Edit a number upward when
coverage climbs; never downward to make a regression pass.

### `check-deadcode.sh` — the built-but-not-wired analyzer

Runs `golang.org/x/tools/cmd/deadcode` (pinned), auto-detecting the root
(library `-test ./...` for courier; see the library section above). Any
unreachable function is reported and fails the build. Zero tolerance, no
allowlist.

### `check-testing-doc.sh` — the docs/TESTING.md gate-check

Asserts every test name and file path `docs/TESTING.md` cites actually exists in
the tree, so the honest-reporting doc cannot rot into citing tests that were
renamed or files that moved. Citations are the backtick-quoted code spans in the
doc. See the script header for the exact citation grammar and the cross-module
rule.

### `check-last-run.sh` — the live-harness LAST-RUN attestation

Asserts `test/integration/LAST-RUN` exists and is well-formed, and that any
operator-run scenarios it names carry a real (non-placeholder) attestation sha.
The release job would invoke it with `--release <sha>` to additionally require
the attestation match the tag being published exactly. courier's `operator_run`
is `none` (every delivery scenario drives a throwaway self-hostable server, so
nothing needs a real external account), so there is nothing that can go stale,
but the gate stays in place for the day a real external-service scenario is
added.

## The committed data files

- `skip-budget.txt` — the integration-leg skip ceiling.
- `coverage-floor.txt` — per-package floors, `<package> <percent>` (`.` is the
  root package).

Bumping either is a normal reviewed edit. Lowering a floor, or raising the skip
budget without naming the skip it admits, is the move the standard's "never
weaken a gate to make code pass" rule forbids.

[std]: the tagwright Testing Standard (wiki, tagwright/operating/Testing Standard)
