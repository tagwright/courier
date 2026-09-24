# Testing

Before this harness existed, none of courier's channels had ever sent a message: the
package had unit-free confidence only. This document describes the integration
harness under `test/integration/` that actually proves delivery, and is blunt about
where it can't.

## Two tiers

**Tier 1, delivery-proven.** For channels with a real, self-hostable open-source
server, the harness runs that server in a throwaway container, sends a
`Notification` through courier's real backend, and then reads the message back out
through the server's own API. This proves an end-to-end round trip: courier's
request was accepted, parsed, and stored the way the receiving service expects.

**Tier 2, request-shape-verified.** The remaining channels either reach a hosted,
account-gated API (the Discord, Slack, and Mattermost webhooks, or the Telegram,
Pushover, and Matrix bot accounts) or exist purely to talk to a specific external
product (a generic webhook receiver, or Gatus). This harness has no real account
or instance for any of
these, so it cannot prove a human would ever see the message. Instead it points
each backend at an in-process HTTP catcher the test binary itself runs (see
`catcher` in `test/integration/harness_test.go`) and asserts the outbound request
courier builds is correct: method, path, headers, and the JSON or form body,
including the webhook backend's HMAC `X-Beacon-Signature`.

**Be clear about what tier 2 does not prove.** A request that is shaped exactly
right can still be rejected by the real service for reasons this harness cannot
see: an expired token, a renamed field, a webhook platform quietly changing its
accepted payload. Tier 2 is confidence that courier assembled the request it meant
to, not confidence a person receives it. Closing that gap for any of these channels
means testing against the real service with a real account, which is out of scope
for an automated, self-cleaning harness.

## Coverage matrix

| Channel      | Tier                     | Proven against                              |
|--------------|--------------------------|----------------------------------------------|
| ntfy         | Delivery-proven          | `binwiederhier/ntfy`, read back via its JSON poll API |
| gotify       | Delivery-proven          | `gotify/server`, read back via `GET /message` with a client token |
| smtp         | Delivery-proven          | `axllent/mailpit`, read back via its HTTP API |
| discord      | Request-shape-verified   | in-process HTTP catcher |
| slack        | Request-shape-verified   | in-process HTTP catcher |
| mattermost   | Request-shape-verified   | in-process HTTP catcher |
| webhook      | Request-shape-verified   | in-process HTTP catcher (body + HMAC signature) |
| telegram     | Request-shape-verified   | in-process HTTP catcher (via `api_base` override, see below) |
| pushover     | Request-shape-verified   | in-process HTTP catcher (via `endpoint` override, see below) |
| matrix       | Request-shape-verified   | in-process HTTP catcher |
| gatus (telemetry) | Request-shape-verified | in-process HTTP catcher |

The built-in `log` backend is not covered here: it writes to a local `io.Writer` or
`slog.Logger` and never leaves the process, so there is no external delivery to
verify. It has no failure mode this harness could catch that a normal unit test
wouldn't already.

## What testing found

Writing these tests surfaced two real bugs, both fixed as part of this work:

- **telegram** and **pushover** built their target URL from a hardcoded constant
  (`https://api.telegram.org`, `https://api.pushover.net/...`) with no way to
  override it. Every other backend that talks to a single service (ntfy, gotify,
  matrix, the generic webhook, gatus) already exposes its target through a setting.
  Telegram and Pushover didn't, which meant nothing could ever verify the shape of
  the request they build without hitting the real, credentialed API. Fixed by
  adding optional settings (`api_base` for telegram, `endpoint` for pushover) that
  override the default when set and change nothing for existing configuration when
  left unset. They exist for testability, not for routing production traffic
  elsewhere.

No other backend needed a code change to be testable or to deliver correctly.

## Running it

```sh
test/integration/run.sh
```

This starts `beacon-itest-ntfy`, `beacon-itest-gotify`, and `beacon-itest-mailpit`
on a `beacon-itest-net` Docker network, waits for each to answer its own health
endpoint, then runs `go test -tags=integration -v ./test/integration/...` inside a
`golang:1.23` container attached to that same network. Every object the script
creates is named with the `beacon-itest-` prefix, and nothing else is touched.
Cleanup runs on exit, whether the run succeeds, fails, or is interrupted, so a
failed run doesn't leave containers or the network behind.

Flags:

- `--keep` skips cleanup at the end, so you can poke at a failure by hand
  (`docker logs beacon-itest-ntfy`, etc.). Clean up manually afterward with
  `docker rm -f beacon-itest-ntfy beacon-itest-gotify beacon-itest-mailpit && docker network rm beacon-itest-net`.
- `--go-image IMAGE` runs the tests in a different Go image (default
  `golang:1.23`).

The tier 1 delivery tests live behind the `integration` build tag, because they
need the throwaway containers `run.sh` starts, so `go build ./...`, `go vet ./...`,
and `go test ./...` (what CI runs on every push) never touch them. Running
`go test -tags=integration ./test/integration/...` directly, outside `run.sh`,
will skip every tier 1 test with a clear message, since the environment variables
`run.sh` sets (`NTFY_URL`, `GOTIFY_URL`, `MAILPIT_SMTP_HOST`, `MAILPIT_SMTP_PORT`,
`MAILPIT_HTTP_URL`) won't be set.

The tier 2 request-shape tests are hermetic: they need no container, no network,
and no credentials, only the in-process catcher the test binary runs itself. They
carry no build tag, so plain `go test ./...` and CI run them on every push. The
`-tags=integration` run includes them too, alongside the tier 1 tests.
