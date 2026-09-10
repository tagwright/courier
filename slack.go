// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import "context"

func init() {
	RegisterBackend("slack", newSlackBackendFromSettings)
}

// slackAttachment is one legacy-style Slack attachment, used here only to
// carry a color bar since Slack and Mattermost both still render it.
type slackAttachment struct {
	Color string `json:"color,omitempty"`
}

// slackPayload is the JSON body a Slack (or Slack-compatible) incoming
// webhook expects. Mattermost accepts the same shape, so this builder is
// shared with MattermostBackend.
type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// buildSlackPayload combines Title and Body into text and attaches a color
// bar keyed off Level, using the "good"/"warning"/"danger" names Slack and
// Mattermost both recognize.
func buildSlackPayload(n Notification) slackPayload {
	return slackPayload{
		Text:        combineTitleBody(n),
		Attachments: []slackAttachment{{Color: slackColorForLevel(n.Level)}},
	}
}

func slackColorForLevel(l Level) string {
	switch l {
	case LevelError:
		return "danger"
	case LevelWarning:
		return "warning"
	default:
		return "good"
	}
}

// SlackBackend sends notifications to a Slack incoming webhook.
type SlackBackend struct {
	webhookSetting string
	resolve        SecretResolver
}

// newSlackBackendFromSettings builds the registered "slack" backend. The
// secret "webhook_secret" names the incoming-webhook URL, required and
// resolved at send time.
func newSlackBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	if _, err := requiredSetting(settings, "webhook_secret"); err != nil {
		return nil, err
	}
	return &SlackBackend{
		webhookSetting: settings["webhook_secret"],
		resolve:        resolve,
	}, nil
}

// Name returns "slack".
func (b *SlackBackend) Name() string {
	return "slack"
}

// Send POSTs n to the configured Slack incoming webhook.
func (b *SlackBackend) Send(ctx context.Context, n Notification) error {
	webhook, err := resolveSecretByName(b.resolve, b.webhookSetting)
	if err != nil {
		return err
	}
	_, err = postJSON(ctx, webhook, buildSlackPayload(n), nil)
	return err
}
