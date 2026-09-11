// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func init() {
	RegisterBackend("webhook", newWebhookBackendFromSettings)
}

// webhookPayload is the documented JSON body a generic webhook receives.
type webhookPayload struct {
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Level     string            `json:"level"`
	Tags      []string          `json:"tags,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	Timestamp string            `json:"timestamp"`
}

// WebhookBackend sends notifications as a JSON POST to an arbitrary URL,
// for services with no dedicated backend of their own. If a signing secret
// is configured, the request carries an X-Beacon-Signature header so the
// receiver can verify it came from this courier. If a bearer secret is
// configured, the request carries an Authorization: Bearer header, for a
// receiver behind OAuth2 or another bearer-token scheme.
type WebhookBackend struct {
	urlSetting    string
	signSetting   string
	bearerSetting string
	resolve       SecretResolver
}

// newWebhookBackendFromSettings builds the registered "webhook" backend.
// The secret "url_secret" names the target URL and is required. The
// optional secret "sign_secret" names an HMAC-SHA256 key; when present,
// every request is signed. The optional secret "bearer_secret" names an
// access token; when present, every request carries an
// "Authorization: Bearer <token>" header. The token is resolved through the
// injected resolver at send time. courier only uses a token it is handed; it
// never runs the OAuth flow, and never acquires, refreshes, or stores one.
func newWebhookBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	if _, err := requiredSetting(settings, "url_secret"); err != nil {
		return nil, err
	}
	return &WebhookBackend{
		urlSetting:    settings["url_secret"],
		signSetting:   settings["sign_secret"],
		bearerSetting: settings["bearer_secret"],
		resolve:       resolve,
	}, nil
}

// Name returns "webhook".
func (b *WebhookBackend) Name() string {
	return "webhook"
}

// Send POSTs n to the configured URL as a webhookPayload. The body is
// marshaled once, so a configured signature is guaranteed to match the
// exact bytes transmitted.
func (b *WebhookBackend) Send(ctx context.Context, n Notification) error {
	target, err := resolveSecretByName(b.resolve, b.urlSetting)
	if err != nil {
		return err
	}

	payload := webhookPayload{
		Title:     n.Title,
		Body:      n.Body,
		Level:     n.Level.String(),
		Tags:      n.Tags,
		Fields:    n.Fields,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding JSON body: %w", err)
	}

	headers := map[string]string{}
	if b.signSetting != "" {
		key, err := resolveSecretByName(b.resolve, b.signSetting)
		if err != nil {
			return err
		}
		mac := hmac.New(sha256.New, []byte(key))
		mac.Write(encoded)
		headers["X-Beacon-Signature"] = hex.EncodeToString(mac.Sum(nil))
	}
	if b.bearerSetting != "" {
		token, err := resolveSecretByName(b.resolve, b.bearerSetting)
		if err != nil {
			return err
		}
		headers["Authorization"] = "Bearer " + token
	}

	_, err = postJSONBytes(ctx, target, encoded, headers)
	return err
}
