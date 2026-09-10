// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"net/url"
)

const pushoverEndpoint = "https://api.pushover.net/1/messages.json"

func init() {
	RegisterBackend("pushover", newPushoverBackendFromSettings)
}

// PushoverBackend sends notifications through Pushover.
type PushoverBackend struct {
	tokenSetting string
	userSetting  string
	endpoint     string
	resolve      SecretResolver
}

// newPushoverBackendFromSettings builds the registered "pushover" backend.
// The secrets "token_secret" (the application token) and "user_secret" (the
// user or group key) are both required and resolved at send time. The
// optional "endpoint" overrides Pushover's public messages API URL; it
// exists so tests can point this backend at a local catcher instead of the
// real Pushover API, not for routing production traffic elsewhere.
func newPushoverBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	if _, err := requiredSetting(settings, "token_secret"); err != nil {
		return nil, err
	}
	if _, err := requiredSetting(settings, "user_secret"); err != nil {
		return nil, err
	}
	endpoint := settings["endpoint"]
	if endpoint == "" {
		endpoint = pushoverEndpoint
	}
	return &PushoverBackend{
		tokenSetting: settings["token_secret"],
		userSetting:  settings["user_secret"],
		endpoint:     endpoint,
		resolve:      resolve,
	}, nil
}

// Name returns "pushover".
func (b *PushoverBackend) Name() string {
	return "pushover"
}

// Send posts n to the Pushover messages API. Message is required by
// Pushover, so Send falls back to Title when Body is empty. Priority is
// deliberately capped below Pushover's emergency tier (2), which requires
// additional retry/expire parameters this backend does not manage.
func (b *PushoverBackend) Send(ctx context.Context, n Notification) error {
	token, err := resolveSecretByName(b.resolve, b.tokenSetting)
	if err != nil {
		return err
	}
	user, err := resolveSecretByName(b.resolve, b.userSetting)
	if err != nil {
		return err
	}

	message := n.Body
	if message == "" {
		message = n.Title
	}

	form := url.Values{
		"token":    {token},
		"user":     {user},
		"message":  {message},
		"priority": {pushoverPriority(n.Level)},
	}
	if n.Title != "" {
		form.Set("title", n.Title)
	}

	_, err = postForm(ctx, b.endpoint, form, nil)
	return err
}

// pushoverPriority maps Level onto Pushover's -2..2 scale: info is low and
// quiet, warning and error are normal and high priority respectively.
func pushoverPriority(l Level) string {
	switch l {
	case LevelError:
		return "1"
	case LevelWarning:
		return "0"
	default:
		return "-1"
	}
}
