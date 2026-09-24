# courier

A small Go library for sending notifications and pushing telemetry. courier gives a
program one place to tell the outside world what is happening: fire an alert when
something needs attention, and push health and status to a monitor on a schedule.

> Naming note: this library used to be called beacon. The name "beacon" is being
> handed to a forthcoming label-driven notification service, and courier is the
> delivery library that service will use to actually send messages. If you are
> looking for the service, this is not it. This is the library it builds on.

It is deliberately narrow. courier knows about notification channels and telemetry
sinks and nothing else, so it drops into any project without dragging along
unrelated dependencies. Secret resolution is injected, so the host program supplies
tokens and passwords its own way.

Status: early, and built first for Ballast, the label-driven backup tool. The API
will move until it settles.

## Two axes

- Notifications: alerts to the channels people actually watch, including email,
  [ntfy](https://ntfy.sh), [Gotify](https://gotify.net),
  [Telegram](https://core.telegram.org/bots), [Discord](https://discord.com),
  [Slack](https://slack.com), [Mattermost](https://mattermost.com),
  [Pushover](https://pushover.net), and [Matrix](https://matrix.org), with a
  generic webhook or a templatable HTTP channel for anything else.
- Telemetry: health and status push to a monitor, starting with
  [Gatus](https://github.com/TwiN/gatus) external endpoints.

## Install

    go get github.com/tagwright/courier

## Design

courier is configured through a plain struct and returns a small interface you call
when something happens. It imports nothing from any particular application. A host
wires its own events into it and provides a function that resolves secret names to
values, so courier never assumes where secrets live.

Every channel and telemetry sink, with the settings and config keys it reads, is
listed in [docs/CHANNELS.md](docs/CHANNELS.md).

## OAuth access tokens

Some targets need an OAuth2 access token rather than a password. The SMTP channel
speaks XOAUTH2, which Gmail and O365 now require since they disabled basic-auth
SMTP, and the webhook and templatable HTTP channels can send an
`Authorization: Bearer` header for a receiver behind a bearer-token scheme.

In every case the token is just another injected secret: courier resolves it
through the host's secret resolver at send time, the same way it resolves an SMTP
password, and uses it as handed. courier never runs the OAuth flow. It does not
acquire, refresh, or store a token, and it holds no background loop. Keeping a
valid token available to the resolver is the host's job.

- SMTP XOAUTH2: set `auth: xoauth2` on the smtp channel, with `username_secret`
  (the account) and `token_secret` (the access token). The default auth mode is
  unchanged, so existing username/password relays need no edits.
- Webhook / HTTP bearer: set `bearer_secret` on the webhook or http channel to the
  secret naming the token.

## Testing

`test/integration/` holds courier's delivery tests: real end-to-end sends against
throwaway ntfy, Gotify, and mailpit containers for the channels with a
self-hostable server, and outbound-request-shape checks against an in-process
catcher for the rest. See [docs/TESTING.md](docs/TESTING.md) for the coverage
matrix and how to run it.

For how courier handles credentials and where it sends them, and what it does
not defend against, see [docs/SECURITY.md](docs/SECURITY.md).

## License

GPL-3.0-or-later. See [LICENSE](LICENSE). Each source file carries an
`SPDX-License-Identifier: GPL-3.0-or-later` header.
