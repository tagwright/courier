# courier

A small Go library for sending notifications and pushing telemetry. courier gives a
program one place to tell the outside world what is happening: fire an alert when
something needs attention, and push health and status to a monitor on a schedule.

> Naming note: this library used to be called beacon. The name "beacon" is being
> handed to a forthcoming label-driven notification service, and courier is the
> delivery library that service will use to actually send messages. If you are
> looking for the service, this is not it; this is the library it builds on.

It is deliberately narrow. courier knows about notification channels and telemetry
sinks and nothing else, so it drops into any project without dragging along
unrelated dependencies. Secret resolution is injected, so the host program supplies
tokens and passwords its own way.

Status: early, and built first for Ballast, the label-driven backup tool. The API
will move until it settles.

## Two axes

- Notifications: alerts to the channels people actually watch, including email,
  ntfy, Gotify, Telegram, Discord, Slack, Mattermost, Pushover, and Matrix, with a
  generic webhook or a templatable HTTP channel for anything else.
- Telemetry: health and status push to a monitor, starting with Gatus external
  endpoints.

## Design

courier is configured through a plain struct and returns a small interface you call
when something happens. It imports nothing from any particular application. A host
wires its own events into it and provides a function that resolves secret names to
values, so courier never assumes where secrets live.

## Testing

`test/integration/` holds courier's delivery tests: real end-to-end sends against
throwaway ntfy, Gotify, and mailpit containers for the channels with a
self-hostable server, and outbound-request-shape checks against an in-process
catcher for the rest. See [docs/TESTING.md](docs/TESTING.md) for the coverage
matrix and how to run it.

## License

GPL-3.0-or-later. See [LICENSE](LICENSE). Each source file carries an
`SPDX-License-Identifier: GPL-3.0-or-later` header.
