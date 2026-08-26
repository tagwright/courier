// SPDX-License-Identifier: GPL-3.0-or-later

package beacon

import "context"

func init() {
	RegisterBackend("mattermost", newMattermostBackendFromSettings)
}

// MattermostBackend sends notifications to a Mattermost incoming webhook.
// Mattermost's incoming webhooks accept the same JSON shape as Slack's, so
// this backend reuses buildSlackPayload from slack.go rather than
// duplicating it.
type MattermostBackend struct {
	webhookSetting string
	resolve        SecretResolver
}

// newMattermostBackendFromSettings builds the registered "mattermost"
// backend. The secret "webhook_secret" names the incoming-webhook URL,
// required and resolved at send time.
func newMattermostBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	if _, err := requiredSetting(settings, "webhook_secret"); err != nil {
		return nil, err
	}
	return &MattermostBackend{
		webhookSetting: settings["webhook_secret"],
		resolve:        resolve,
	}, nil
}

// Name returns "mattermost".
func (b *MattermostBackend) Name() string {
	return "mattermost"
}

// Send POSTs n to the configured Mattermost incoming webhook.
func (b *MattermostBackend) Send(ctx context.Context, n Notification) error {
	webhook, err := resolveSecretByName(b.resolve, b.webhookSetting)
	if err != nil {
		return err
	}
	_, err = postJSON(ctx, webhook, buildSlackPayload(n), nil)
	return err
}
