<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Security

courier is a library, not a service, so its security story is smaller than a
daemon's and worth stating exactly. It touches two sensitive things: the
credentials a notification backend needs to authenticate, and the message
content a host asks it to deliver. Both are in courier's hands only for the
length of a single send, and both leave the process over the network to a
destination the operator's config chose. That last part is the whole shape of
the threat model, so it comes first below. Every claim here is checkable against
the source in this repository.

## What courier holds

courier does not store credentials, and it does not decrypt or fetch them. A
config never carries a secret value. Any setting whose name ends in `_secret`
holds the *name* of a secret, and courier keeps only that name. The value is
resolved through a `SecretResolver` the host supplies, at send time, on every
send. Where the secret actually lives (a file, an env var, SOPS, a vault) is the
host's concern and courier makes no assumption about it. If the host wires no
resolver, `New` substitutes one that errors on every lookup rather than handing
back an empty credential.

Two honest consequences follow:

- A resolved value is an ordinary Go `string` for the duration of the send. It
  sits on the heap, courier does not lock it into non-swappable memory, and a
  Go string cannot be zeroized after use. courier makes no at-rest or
  in-memory-hardening claim about credential material. Narrowing that window is
  the secret store's job upstream, not the library's.
- A credential that resolves to an empty string is rejected as an error, never
  sent as a blank password or token. A backend does not silently transmit a
  missing credential.

## Where courier sends, and who owns that

This is the part that matters most. Every backend POSTs to a target the config
names: a `url_secret` or `webhook_secret` for the generic channels, a `server`
or `homeserver` or SMTP `host` for the named ones. courier sends the
notification's content, and where a channel is configured for it, the resolved
bearer token or API key, to exactly that target and asks no questions about it.

So a wrong or hostile endpoint is an exfiltration path, and it is one the
operator owns. Point `webhook_secret` at an attacker's collector and courier
will faithfully deliver your alert bodies, and any `bearer_secret` you attached,
straight to it. The `api_base`, `endpoint`, and `server` overrides that exist
for pointing tests at a local catcher can redirect production traffic the same
way if they are set in a live config. courier cannot tell a legitimate receiver
from a malicious one, because from inside the library they are the same HTTP
POST. Treat the endpoint values in a courier config as security-relevant, review
them the way you would review where a firewall lets traffic out, and keep them
under the same change control as the rest of the deployment.

## Signing and bearer auth

Two mechanisms let a receiver trust that a request came from your courier, and
it is worth being precise about which channels have which.

The `webhook` channel supports an HMAC-SHA256 signature. Set `sign_secret` and
every request carries an `X-Beacon-Signature` header computed over the exact
bytes on the wire. The body is marshaled once and signed and sent as that single
byte slice, so the signature always matches what the receiver reads. This is the
only channel with request signing. It proves origin to a receiver that checks
it, and it does nothing on its own if the receiver ignores the header.

Bearer-token auth is broader. The `webhook`, `http`, `ntfy`, `matrix`, and
`gatus` targets can send an `Authorization: Bearer <token>` header from a
resolved secret, Gotify uses its `X-Gotify-Key` header, and Telegram carries its
bot token in the URL path. In all of these courier uses the token exactly as the
resolver hands it over. It never runs an OAuth flow and never acquires,
refreshes, or caches a token. A bearer header authenticates courier to the
receiver over the transport, which is a different thing from the HMAC signature
above and not a substitute for TLS.

## What it logs, and what it never logs

No path in courier writes a resolved secret value to a log, an error, or a
returned string. This is worth being exact about, because it is achieved by
never putting the value there in the first place, not by a redaction pass that
scrubs it afterward. There is no redaction layer, and none is claimed.

- Error messages that involve a secret name it by its *name* only. A failed
  resolve reads `resolving secret "smtp_password"`, never the value behind it.
- An HTTP error names the destination by host, not by full URL. A `url_secret`
  that resolves to a URL with a token or key embedded in its path or query is
  reported as the host alone, so that credential does not spill into error text
  or a caller's logs.
- A non-2xx response body is truncated to 512 bytes before it is echoed into an
  error, so a misbehaving or hostile receiver cannot balloon or flood an error
  message through the response it returns.

One thing courier does not control: the `log` channel writes the notification
itself, its title, body, tags, and fields, to a stream or through `slog`. That
is the point of the channel. courier does not inspect that content, so if a host
places a secret into a notification body or field, the log channel will write
it. Keeping credentials out of notification content is the host's call, not
something the library can enforce.

## Transport

HTTP channels use Go's default `net/http` client with a 10-second timeout per
request. TLS verification is Go's default: the system trust store, standard
certificate validation, no pinning and no custom TLS config. The built-in
defaults for the hosted services are `https` endpoints.

courier does not force TLS. A `url_secret` or `server` that names an `http://`
target is honored as written, and the SMTP channel's `encryption` setting
accepts `none` to disable TLS outright alongside its `starttls` default and
implicit-`tls` mode. Those are operator choices, and courier carries them out
rather than overriding them. If a channel must be encrypted, the config has to
say so, because the library will not insist.

## What courier does not defend against

Named plainly, so nobody deploys expecting a guarantee that is not here.

- A malicious or careless config author. Whoever writes the courier config
  chooses every destination and can point delivery anywhere. courier has no
  allowlist of endpoints and no notion of a trusted receiver. The endpoint
  review above is the mitigation, and it lives with the operator.
- A compromised host process. The host owns the `SecretResolver` and can already
  read its own secrets. A process that runs courier is inside courier's trust
  boundary by construction.
- Secret material at rest or a hardened memory lifetime. courier holds a
  resolved value transiently as a Go string and makes no claim beyond that.
  Encryption at rest, access scoping, and rotation belong to the secret store
  the resolver reads from.
- A TLS downgrade the operator selected. An `http://` endpoint or
  `encryption: none` is honored as configured, and courier will not warn or
  refuse.
- What the receiver does with a delivered message. Once a notification reaches
  the endpoint the config named, its handling, retention, and onward exposure
  are the receiver's, outside anything the library can reach.

## Reporting a vulnerability

Report a suspected vulnerability through GitHub's private vulnerability reporting
on this repository: open the Security tab and choose "Report a vulnerability".
The report stays private to the maintainers while it is triaged.

Disclosure is coordinated. A fix is prepared and released before the details are
made public, and you are kept in the loop on timing.
