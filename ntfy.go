// SPDX-License-Identifier: GPL-3.0-or-later

package beacon

import (
	"context"
	"strings"
)

func init() {
	RegisterBackend("ntfy", newNtfyBackendFromSettings)
}

// NtfyBackend sends notifications to an ntfy server (ntfy.sh or self-hosted)
// as a plain HTTP POST to a topic.
type NtfyBackend struct {
	server       string
	topic        string
	tokenSetting string
	resolve      SecretResolver
}

// newNtfyBackendFromSettings builds the registered "ntfy" backend. Settings
// accepts "server" (default "https://ntfy.sh") and requires "topic". The
// optional secret "token_secret" names a bearer token for protected topics,
// resolved at send time.
func newNtfyBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	topic, err := requiredSetting(settings, "topic")
	if err != nil {
		return nil, err
	}
	server := settings["server"]
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &NtfyBackend{
		server:       strings.TrimRight(server, "/"),
		topic:        topic,
		tokenSetting: settings["token_secret"],
		resolve:      resolve,
	}, nil
}

// Name returns "ntfy".
func (b *NtfyBackend) Name() string {
	return "ntfy"
}

// Send POSTs n to the configured ntfy topic. The request body is n.Body,
// falling back to n.Title if there is no body. Title, Tags, and Priority are
// sent as ntfy headers.
func (b *NtfyBackend) Send(ctx context.Context, n Notification) error {
	message := n.Body
	if message == "" {
		message = n.Title
	}

	headers := map[string]string{
		"Priority": ntfyPriority(levelPriority(n.Level)),
	}
	if n.Title != "" {
		headers["Title"] = n.Title
	}
	if len(n.Tags) > 0 {
		headers["Tags"] = strings.Join(n.Tags, ",")
	}

	if b.tokenSetting != "" {
		token, err := resolveSecretByName(b.resolve, b.tokenSetting)
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + token
	}

	_, err := postText(ctx, b.server+"/"+b.topic, message, headers)
	return err
}

// ntfyPriority converts the shared 1-5 priority scale into the words ntfy
// expects: default is 3, high is 4, urgent is 5.
func ntfyPriority(p int) string {
	switch {
	case p >= 5:
		return "urgent"
	case p == 4:
		return "high"
	default:
		return "default"
	}
}
