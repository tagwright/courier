#!/usr/bin/env bash
# beacon integration test harness: the first real delivery tests for every
# beacon channel. It starts three throwaway, self-hostable servers (ntfy,
# gotify, mailpit), runs `go test -tags=integration` against them and
# against an in-process HTTP catcher, and tears everything down after.
#
# Tier 1 (delivery-proven): ntfy, gotify, and smtp send a real Notification
# through beacon and then read it back via that server's own API.
#
# Tier 2 (request-shape-verified): discord, slack, mattermost, webhook,
# telegram, pushover, matrix, and the gatus sink point at an in-process HTTP
# catcher the test binary itself runs, since those channels need a real
# third-party account or instance this harness cannot obtain. These tests
# only prove the outbound request is shaped correctly. See ../../docs/TESTING.md.
#
# Every Docker object this script creates is named with the prefix
# "beacon-itest-". It never touches any other container or network. Cleanup
# runs on exit (success, failure, or interrupt) via the trap below, so a
# failed run does not leave test objects behind.
#
# Usage: test/integration/run.sh [--keep] [--go-image IMAGE]
#   --keep             skip cleanup at the end (for debugging a failure)
#   --go-image IMAGE   golang image to run the tests in (default golang:1.25.14)

set -euo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HARNESS_DIR/../.." && pwd)"

KEEP=0
# golang:1.25.14 matches go.mod's floor (go 1.25.0); an older image would force a
# toolchain download at test time under GOTOOLCHAIN=auto.
GO_IMAGE="golang:1.25.14"
while [ $# -gt 0 ]; do
  case "$1" in
    --keep) KEEP=1; shift ;;
    --go-image) GO_IMAGE="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

NET=beacon-itest-net
NTFY=beacon-itest-ntfy
GOTIFY=beacon-itest-gotify
MAILPIT=beacon-itest-mailpit
RUNNER=beacon-itest-runner

log() { printf '\n=== %s ===\n' "$1"; }

cleanup() {
  if [ "$KEEP" -eq 1 ]; then
    log "skipping cleanup (--keep); remove beacon-itest-* by hand when done"
    return
  fi
  log "cleanup"
  docker rm -f "$RUNNER" >/dev/null 2>&1 || true
  docker rm -f "$NTFY" "$GOTIFY" "$MAILPIT" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# --- start clean, in case a previous run was interrupted --------------------

log "clearing any leftover beacon-itest-* objects"
docker rm -f "$RUNNER" "$NTFY" "$GOTIFY" "$MAILPIT" >/dev/null 2>&1 || true
docker network rm "$NET" >/dev/null 2>&1 || true

# --- network ------------------------------------------------------------

log "creating $NET"
docker network create "$NET" >/dev/null

# --- tier 1 servers ----------------------------------------------------

log "starting $NTFY (binwiederhier/ntfy)"
docker run -d --name "$NTFY" --network "$NET" \
  binwiederhier/ntfy serve >/dev/null

log "starting $GOTIFY (gotify/server)"
docker run -d --name "$GOTIFY" --network "$NET" \
  gotify/server >/dev/null

log "starting $MAILPIT (axllent/mailpit)"
docker run -d --name "$MAILPIT" --network "$NET" \
  axllent/mailpit >/dev/null

wait_ready() {
  local name="$1" url="$2"
  log "waiting for $name to answer $url"
  docker run --rm --name "beacon-itest-wait-$name" --network "$NET" \
    curlimages/curl:latest \
    -sf --retry 30 --retry-delay 1 --retry-connrefused --retry-all-errors "$url" >/dev/null
  echo "$name is ready"
}

wait_ready ntfy "http://$NTFY:80/v1/health"
wait_ready gotify "http://$GOTIFY:80/version"
wait_ready mailpit "http://$MAILPIT:8025/api/v1/info"

# --- run the integration tests ------------------------------------------

log "running go test -tags=integration against $NET (image $GO_IMAGE)"
set +e
docker run --rm --name "$RUNNER" --network "$NET" \
  -v "$REPO_ROOT":/src:ro \
  -w /src \
  -e NTFY_URL="http://$NTFY:80" \
  -e GOTIFY_URL="http://$GOTIFY:80" \
  -e MAILPIT_SMTP_HOST="$MAILPIT" \
  -e MAILPIT_SMTP_PORT="1025" \
  -e MAILPIT_HTTP_URL="http://$MAILPIT:8025" \
  -e GOCACHE=/tmp/gocache \
  "$GO_IMAGE" \
  go test -tags=integration -count=1 -v ./test/integration/...
STATUS=$?
set -e

if [ $STATUS -ne 0 ]; then
  log "FAIL: integration tests exited $STATUS"
  echo "--- $NTFY logs (tail) ---"
  docker logs --tail 40 "$NTFY" 2>&1 || true
  echo "--- $GOTIFY logs (tail) ---"
  docker logs --tail 40 "$GOTIFY" 2>&1 || true
  echo "--- $MAILPIT logs (tail) ---"
  docker logs --tail 40 "$MAILPIT" 2>&1 || true
  exit $STATUS
fi

log "PASS"
