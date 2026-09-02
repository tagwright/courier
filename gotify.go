// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"context"
	"strings"
)

func init() {
	RegisterBackend("gotify", newGotifyBackendFromSettings)
}

// gotifyMessage is the JSON body Gotify's /message endpoint expects.
type gotifyMessage struct {
	Title    string `json:"title,omitempty"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// GotifyBackend sends notifications to a self-hosted Gotify server.
type GotifyBackend struct {
	server       string
	tokenSetting string
	resolve      SecretResolver
}

// newGotifyBackendFromSettings builds the registered "gotify" backend.
// Settings requires "server", the base URL of the Gotify instance. The
// secret "token_secret" names the application token, required and resolved
// at send time.
func newGotifyBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	server, err := requiredSetting(settings, "server")
	if err != nil {
		return nil, err
	}
	if _, err := requiredSetting(settings, "token_secret"); err != nil {
		return nil, err
	}
	return &GotifyBackend{
		server:       strings.TrimRight(server, "/"),
		tokenSetting: settings["token_secret"],
		resolve:      resolve,
	}, nil
}

// Name returns "gotify".
func (b *GotifyBackend) Name() string {
	return "gotify"
}

// Send POSTs n to the Gotify /message endpoint, authenticated with the app
// token via the X-Gotify-Key header. Priority uses the same 1-5 scale
// shared with ntfy, which sits comfortably inside Gotify's 0-10 range.
func (b *GotifyBackend) Send(ctx context.Context, n Notification) error {
	token, err := resolveSecretByName(b.resolve, b.tokenSetting)
	if err != nil {
		return err
	}

	msg := gotifyMessage{
		Title:    n.Title,
		Message:  n.Body,
		Priority: levelPriority(n.Level),
	}
	if msg.Message == "" {
		msg.Message = n.Title
	}

	headers := map[string]string{"X-Gotify-Key": token}
	_, err = postJSON(ctx, b.server+"/message", msg, headers)
	return err
}
