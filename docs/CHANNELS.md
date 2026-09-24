# Channels

Every notification channel and telemetry sink courier ships, with the settings
each one reads. The code is the source of truth: the keys below are the exact
strings each backend looks up in its `Settings` map, so a config that uses a key
not listed here has no effect.

## How a channel is configured

A notification channel is one `ChannelConfig`:

    Type      selects the backend by its registered name (the headings below).
    MinLevel  the lowest severity this channel receives. A notification below
              MinLevel is skipped for this channel rather than sent.
    Settings  a map[string]string of the backend-specific keys documented here.

A telemetry sink is a `TelemetryConfig` with a `Type` and the same kind of
`Settings` map, and no `MinLevel`.

Any key whose name ends in `_secret` holds the *name* of a secret, not the value.
The backend resolves that name through the host's `SecretResolver` at send time,
so a rotated credential takes effect without a rebuild. A name that resolves to
an empty string is treated as an error, never as an empty credential.

A notification carries a `Title`, a `Body`, a `Level` (`info`, `warning`, or
`error`), optional `Tags`, and optional `Fields` of string key/value pairs. Each
backend renders those into whatever its target accepts, called out per channel
where it changes the wire shape.

## Notification channels

### `log`

The built-in floor. It is always registered, depends on nothing outside the
standard library, and never returns an error, so a notification is never lost for
want of a working channel. Output is either plain lines to a stream or structured
records through `log/slog`.

| Key | Required | Meaning |
| --- | --- | --- |
| `output` | no | `stdout` or `stderr` writes plain lines to that stream. Any other value, or none, logs through `slog.Default()`. |

### `smtp`

Email through an SMTP relay, built on `github.com/wneessen/go-mail`. Any
credentials are resolved fresh on every send. A titleless notification is given
the subject `Notification`, since a blank subject reads as broken in most clients.

| Key | Required | Meaning |
| --- | --- | --- |
| `host` | yes | relay hostname. |
| `from` | yes | the From address. |
| `to` | yes | comma-separated recipient list. At least one address must survive trimming. |
| `port` | no | relay port. Defaults to go-mail's own default for the chosen encryption. |
| `encryption` | no | `starttls` (default), `tls` (implicit TLS), or `none`. |
| `auth` | no | `auto` (default, username/password) or `xoauth2`. |
| `username_secret` | conditional | account name. Optional under `auto`, required under `xoauth2`. |
| `password_secret` | conditional | password. Under `auto` it must be set together with `username_secret` or both left absent. Not used under `xoauth2`. |
| `token_secret` | conditional | OAuth2 access token. Required under `xoauth2`, rejected under `auto`. courier uses the token as handed and never runs the OAuth flow. |

### `ntfy`

An HTTP POST to an ntfy topic, on ntfy.sh or a self-hosted server. `Title`,
`Tags`, and the level-derived priority travel as ntfy headers. The request body
is `Body`, falling back to `Title` when `Body` is empty.

| Key | Required | Meaning |
| --- | --- | --- |
| `topic` | yes | the topic to publish to. |
| `server` | no | base URL of the ntfy server. Defaults to `https://ntfy.sh`. |
| `token_secret` | no | bearer token for a protected topic. Omit for a public one. |

### `gotify`

An HTTP POST to a self-hosted Gotify `/message`, authenticated with the
`X-Gotify-Key` header. The 1-5 level priority sits inside Gotify's 0-10 range.

| Key | Required | Meaning |
| --- | --- | --- |
| `server` | yes | base URL of the Gotify instance. |
| `token_secret` | yes | Gotify application token. |

### `discord`

An incoming-webhook POST. A notification with a title becomes a coloured embed
(red for error, orange for warning, blue otherwise). A titleless one is sent as
plain content instead, since there is nothing to title the embed with.

| Key | Required | Meaning |
| --- | --- | --- |
| `webhook_secret` | yes | names the full Discord incoming-webhook URL. |

### `slack`

An incoming-webhook POST. Title and Body are joined into the message text, with a
colour bar keyed off level using Slack's `good`, `warning`, and `danger` names.

| Key | Required | Meaning |
| --- | --- | --- |
| `webhook_secret` | yes | names the Slack incoming-webhook URL. |

### `mattermost`

The same JSON shape as Slack (it reuses the Slack payload builder), posted to a
Mattermost incoming webhook.

| Key | Required | Meaning |
| --- | --- | --- |
| `webhook_secret` | yes | names the Mattermost incoming-webhook URL. |

### `telegram`

A POST to the bot `sendMessage` endpoint. Telegram has no separate subject, so
Title and Body are combined into one text field.

| Key | Required | Meaning |
| --- | --- | --- |
| `chat_id` | yes | target chat id. |
| `token_secret` | yes | bot token. |
| `api_base` | no | overrides the Bot API host (default `https://api.telegram.org`). It exists so tests can point at a local catcher, not for routing production traffic elsewhere. |

### `pushover`

A form POST to the Pushover messages API. `Message` is required by Pushover, so
Body falls back to Title when empty. Priority stays below Pushover's emergency
tier, which needs retry and expire parameters this backend does not manage.

| Key | Required | Meaning |
| --- | --- | --- |
| `token_secret` | yes | Pushover application token. |
| `user_secret` | yes | user or group key. |
| `endpoint` | no | overrides the messages API URL (default `https://api.pushover.net/1/messages.json`). A test-only override. |

### `matrix`

A PUT to the client-server room-send endpoint as a bot user. The bot account must
already be joined to the target room, because this backend cannot accept an
invite. Every send generates a fresh transaction id so a homeserver never
deduplicates two notifications into one.

| Key | Required | Meaning |
| --- | --- | --- |
| `homeserver` | yes | base URL, for example `https://matrix.org`. |
| `room_id` | yes | target room, for example `!abc:matrix.org`. |
| `token_secret` | yes | the bot's access token. |
| `msgtype` | no | Matrix message type, default `m.text`. Use `m.notice` for bot traffic clients should not push loudly. |

### `webhook`

courier's own fixed JSON payload (`title`, `body`, `level`, `tags`, `fields`,
`timestamp`) POSTed to an arbitrary URL, optionally HMAC-signed. Reach for this
when the receiver is happy to accept courier's shape.

| Key | Required | Meaning |
| --- | --- | --- |
| `url_secret` | yes | names the target URL. |
| `sign_secret` | no | names an HMAC-SHA256 key. When set, each request carries an `X-Beacon-Signature` header computed over the exact bytes sent. |
| `bearer_secret` | no | names an access token, sent as `Authorization: Bearer`. |

### `http`

The generic templatable channel. The caller defines the exact request body with a
Go `text/template`, so any service that accepts an HTTP POST is reachable by
configuration alone. Reach for this when the receiver dictates its own payload
shape. A `tojson` template function is available for embedding text into a JSON
body without hand-rolled escaping. The template sees `.Message` (Title and Body
joined), `.Title`, `.Body`, `.Level`, `.Recipient`, `.Tags`, and `.Fields`.

| Key | Required | Meaning |
| --- | --- | --- |
| `url` | one of | literal POST target. Provide this or `url_secret`, never both. |
| `url_secret` | one of | names a secret that resolves to the POST target, for a URL that must not sit in config in the clear. |
| `body_template` | yes | the `text/template` rendered into the request body. |
| `content_type` | no | request Content-Type (default `application/json`). |
| `recipient` | no | a value exposed to the template as `.Recipient`. |
| `bearer_secret` | no | names an access token, sent as `Authorization: Bearer`. |
| `success_field` | no | a top-level field of the JSON response that must be present for the send to count as delivered. |
| `success_equals` | no | alongside `success_field`, the value that field must equal, compared as text. Without it, the field need only be truthy. |

## Telemetry sinks

### `gatus`

Pushes a health result to a Gatus external endpoint, its push-based heartbeat
API. Call `Report` on a dead-man's-switch schedule even when healthy, because
Gatus marks the endpoint unhealthy if no push lands inside its configured
interval. `success`, `duration`, and (only when the result is not OK) `error` go
as URL query parameters, since the external-endpoint API takes no request body.

| Key | Required | Meaning |
| --- | --- | --- |
| `url` | yes | the Gatus base URL. |
| `endpoint_key` | yes | the external endpoint's key, the `group_endpoint-name` path segment configured for it in Gatus. |
| `token_secret` | no | bearer token, sent as an Authorization header when set. |
