// SPDX-License-Identifier: GPL-3.0-or-later

package beacon

import "context"

func init() {
	RegisterBackend("telegram", newTelegramBackendFromSettings)
}

// telegramMessage is the JSON body Telegram's sendMessage endpoint expects.
type telegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// TelegramBackend sends notifications through a Telegram bot to a chat.
type TelegramBackend struct {
	chatID       string
	tokenSetting string
	resolve      SecretResolver
}

// newTelegramBackendFromSettings builds the registered "telegram" backend.
// Settings requires "chat_id". The secret "token_secret" names the bot
// token, required and resolved at send time.
func newTelegramBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	chatID, err := requiredSetting(settings, "chat_id")
	if err != nil {
		return nil, err
	}
	if _, err := requiredSetting(settings, "token_secret"); err != nil {
		return nil, err
	}
	return &TelegramBackend{
		chatID:       chatID,
		tokenSetting: settings["token_secret"],
		resolve:      resolve,
	}, nil
}

// Name returns "telegram".
func (b *TelegramBackend) Name() string {
	return "telegram"
}

// Send POSTs n to the bot's sendMessage endpoint. Title and Body are
// combined into a single text field, since Telegram has no separate
// subject.
func (b *TelegramBackend) Send(ctx context.Context, n Notification) error {
	token, err := resolveSecretByName(b.resolve, b.tokenSetting)
	if err != nil {
		return err
	}

	msg := telegramMessage{
		ChatID: b.chatID,
		Text:   combineTitleBody(n),
	}

	target := "https://api.telegram.org/bot" + token + "/sendMessage"
	_, err = postJSON(ctx, target, msg, nil)
	return err
}
