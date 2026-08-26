// SPDX-License-Identifier: GPL-3.0-or-later

package beacon

import "context"

func init() {
	RegisterBackend("discord", newDiscordBackendFromSettings)
}

// Discord embed colors, chosen to read clearly against Discord's dark
// theme: red for an error, orange for a warning, blue for routine
// information.
const (
	discordColorRed    = 0xE74C3C
	discordColorOrange = 0xE67E22
	discordColorBlue   = 0x3498DB
)

// discordEmbed is one embed in a Discord webhook payload.
type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color"`
}

// discordPayload is the JSON body a Discord webhook expects.
type discordPayload struct {
	Content string         `json:"content,omitempty"`
	Embeds  []discordEmbed `json:"embeds,omitempty"`
}

// DiscordBackend sends notifications to a Discord incoming webhook.
type DiscordBackend struct {
	webhookSetting string
	resolve        SecretResolver
}

// newDiscordBackendFromSettings builds the registered "discord" backend.
// The secret "webhook_secret" names the full webhook URL, required and
// resolved at send time.
func newDiscordBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	if _, err := requiredSetting(settings, "webhook_secret"); err != nil {
		return nil, err
	}
	return &DiscordBackend{
		webhookSetting: settings["webhook_secret"],
		resolve:        resolve,
	}, nil
}

// Name returns "discord".
func (b *DiscordBackend) Name() string {
	return "discord"
}

// Send POSTs n to the configured webhook as an embed: title, description,
// and a color keyed off Level. If n.Title is empty there is nothing
// meaningful to title an embed with, so Send falls back to a plain content
// message instead.
func (b *DiscordBackend) Send(ctx context.Context, n Notification) error {
	webhook, err := resolveSecretByName(b.resolve, b.webhookSetting)
	if err != nil {
		return err
	}

	var payload discordPayload
	if n.Title == "" {
		payload.Content = n.Body
	} else {
		payload.Embeds = []discordEmbed{{
			Title:       n.Title,
			Description: n.Body,
			Color:       discordColorForLevel(n.Level),
		}}
	}

	_, err = postJSON(ctx, webhook, payload, nil)
	return err
}

func discordColorForLevel(l Level) int {
	switch l {
	case LevelError:
		return discordColorRed
	case LevelWarning:
		return discordColorOrange
	default:
		return discordColorBlue
	}
}
