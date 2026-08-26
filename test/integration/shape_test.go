// SPDX-License-Identifier: GPL-3.0-or-later

//go:build integration

// Tier 2 tests: discord, slack, mattermost, webhook, telegram, pushover,
// matrix, and the gatus telemetry sink all need a real third-party account
// or instance to prove actual delivery, which this harness cannot obtain on
// its own. These tests instead point each backend at the in-process catcher
// (see catcher in harness_test.go) and assert the outbound request beacon
// builds is correct: method, path, headers, and JSON/form body. See
// docs/TESTING.md for why this is a narrower guarantee than tier 1.
package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tagwright/beacon"
)

func TestDiscordRequestShape(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.ChannelConfig{
		Type:     "discord",
		Settings: map[string]string{"webhook_secret": "webhook"},
	}
	resolve := literalResolver(map[string]string{"webhook": c.URL()})
	n := beacon.Notification{Title: "Disk full", Body: "/data is at 95%", Level: beacon.LevelError}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload struct {
		Content string `json:"content"`
		Embeds  []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Color       int    `json:"color"`
		} `json:"embeds"`
	}
	decodeJSON(t, req.Body, &payload)
	if len(payload.Embeds) != 1 {
		t.Fatalf("embeds = %d, want 1: %+v", len(payload.Embeds), payload)
	}
	if payload.Embeds[0].Title != n.Title {
		t.Errorf("embed title = %q, want %q", payload.Embeds[0].Title, n.Title)
	}
	if payload.Embeds[0].Description != n.Body {
		t.Errorf("embed description = %q, want %q", payload.Embeds[0].Description, n.Body)
	}
	const wantColor = 0xE74C3C // discordColorRed
	if payload.Embeds[0].Color != wantColor {
		t.Errorf("embed color = %#x, want %#x", payload.Embeds[0].Color, wantColor)
	}
}

// TestDiscordRequestShapeNoTitle covers the fallback branch in
// DiscordBackend.Send: with no title there is nothing to caption an embed
// with, so Send should post plain content instead of an empty embed.
func TestDiscordRequestShapeNoTitle(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.ChannelConfig{
		Type:     "discord",
		Settings: map[string]string{"webhook_secret": "webhook"},
	}
	resolve := literalResolver(map[string]string{"webhook": c.URL()})
	n := beacon.Notification{Body: "just a body, no title", Level: beacon.LevelInfo}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var payload struct {
		Content string `json:"content"`
		Embeds  []any  `json:"embeds"`
	}
	decodeJSON(t, c.last(t).Body, &payload)
	if payload.Content != n.Body {
		t.Errorf("content = %q, want %q", payload.Content, n.Body)
	}
	if len(payload.Embeds) != 0 {
		t.Errorf("embeds = %d, want 0 when there is no title", len(payload.Embeds))
	}
}

func TestSlackRequestShape(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.ChannelConfig{
		Type:     "slack",
		Settings: map[string]string{"webhook_secret": "webhook"},
	}
	resolve := literalResolver(map[string]string{"webhook": c.URL()})
	n := beacon.Notification{Title: "Backup finished", Body: "12.3GB in 4m", Level: beacon.LevelInfo}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload struct {
		Text        string `json:"text"`
		Attachments []struct {
			Color string `json:"color"`
		} `json:"attachments"`
	}
	decodeJSON(t, req.Body, &payload)
	wantText := n.Title + "\n\n" + n.Body
	if payload.Text != wantText {
		t.Errorf("text = %q, want %q", payload.Text, wantText)
	}
	if len(payload.Attachments) != 1 || payload.Attachments[0].Color != "good" {
		t.Errorf("attachments = %+v, want one attachment with color %q", payload.Attachments, "good")
	}
}

// TestMattermostRequestShape checks the mattermost backend, which reuses
// slack.go's payload builder, produces the same shape with mattermost's own
// webhook secret and a different Level (exercising the "warning" color).
func TestMattermostRequestShape(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.ChannelConfig{
		Type:     "mattermost",
		Settings: map[string]string{"webhook_secret": "webhook"},
	}
	resolve := literalResolver(map[string]string{"webhook": c.URL()})
	n := beacon.Notification{Title: "Disk usage high", Body: "82% on /var", Level: beacon.LevelWarning}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	var payload struct {
		Text        string `json:"text"`
		Attachments []struct {
			Color string `json:"color"`
		} `json:"attachments"`
	}
	decodeJSON(t, req.Body, &payload)
	wantText := n.Title + "\n\n" + n.Body
	if payload.Text != wantText {
		t.Errorf("text = %q, want %q", payload.Text, wantText)
	}
	if len(payload.Attachments) != 1 || payload.Attachments[0].Color != "warning" {
		t.Errorf("attachments = %+v, want one attachment with color %q", payload.Attachments, "warning")
	}
}

func TestWebhookRequestShape(t *testing.T) {
	c := newCatcher(t)
	const signingKey = "s3cr3t-signing-key"
	cfg := beacon.ChannelConfig{
		Type: "webhook",
		Settings: map[string]string{
			"url_secret":  "url",
			"sign_secret": "sign",
		},
	}
	resolve := literalResolver(map[string]string{"url": c.URL(), "sign": signingKey})
	n := beacon.Notification{
		Title: "Job failed",
		Body:  "exit code 1",
		Level: beacon.LevelError,
		Tags:  []string{"ci"},
		Fields: map[string]string{
			"job": "nightly",
		},
	}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload struct {
		Title     string            `json:"title"`
		Body      string            `json:"body"`
		Level     string            `json:"level"`
		Tags      []string          `json:"tags"`
		Fields    map[string]string `json:"fields"`
		Timestamp string            `json:"timestamp"`
	}
	decodeJSON(t, req.Body, &payload)
	if payload.Title != n.Title {
		t.Errorf("title = %q, want %q", payload.Title, n.Title)
	}
	if payload.Body != n.Body {
		t.Errorf("body = %q, want %q", payload.Body, n.Body)
	}
	if payload.Level != "error" {
		t.Errorf("level = %q, want %q", payload.Level, "error")
	}
	if len(payload.Tags) != 1 || payload.Tags[0] != "ci" {
		t.Errorf("tags = %v, want [ci]", payload.Tags)
	}
	if payload.Fields["job"] != "nightly" {
		t.Errorf("fields[job] = %q, want %q", payload.Fields["job"], "nightly")
	}
	if _, err := time.Parse(time.RFC3339, payload.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", payload.Timestamp, err)
	}

	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(req.Body)
	wantSig := hex.EncodeToString(mac.Sum(nil))
	if got := req.Header.Get("X-Beacon-Signature"); got != wantSig {
		t.Errorf("X-Beacon-Signature = %q, want %q (HMAC over the exact bytes sent)", got, wantSig)
	}
}

func TestTelegramRequestShape(t *testing.T) {
	c := newCatcher(t)
	const token = "123456:AAExampleTelegramBotToken-abcXYZ"
	cfg := beacon.ChannelConfig{
		Type: "telegram",
		Settings: map[string]string{
			"chat_id":      "-100123456789",
			"token_secret": "token",
			"api_base":     c.URL(),
		},
	}
	resolve := literalResolver(map[string]string{"token": token})
	n := beacon.Notification{Title: "Deploy done", Body: "v1.2.3 is live", Level: beacon.LevelInfo}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	wantPath := "/bot" + token + "/sendMessage"
	if req.Path != wantPath {
		t.Errorf("path = %q, want %q", req.Path, wantPath)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var payload struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	decodeJSON(t, req.Body, &payload)
	if payload.ChatID != "-100123456789" {
		t.Errorf("chat_id = %q, want %q", payload.ChatID, "-100123456789")
	}
	wantText := n.Title + "\n\n" + n.Body
	if payload.Text != wantText {
		t.Errorf("text = %q, want %q", payload.Text, wantText)
	}
}

func TestPushoverRequestShape(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.ChannelConfig{
		Type: "pushover",
		Settings: map[string]string{
			"token_secret": "token",
			"user_secret":  "user",
			"endpoint":     c.URL() + "/1/messages.json",
		},
	}
	resolve := literalResolver(map[string]string{"token": "apptok", "user": "userkey"})
	n := beacon.Notification{Title: "Low disk", Body: "5% free", Level: beacon.LevelWarning}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if req.Path != "/1/messages.json" {
		t.Errorf("path = %q, want /1/messages.json", req.Path)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
	}

	form, err := url.ParseQuery(string(req.Body))
	if err != nil {
		t.Fatalf("parsing form body %q: %v", req.Body, err)
	}
	if got := form.Get("token"); got != "apptok" {
		t.Errorf("token = %q, want apptok", got)
	}
	if got := form.Get("user"); got != "userkey" {
		t.Errorf("user = %q, want userkey", got)
	}
	if got := form.Get("message"); got != n.Body {
		t.Errorf("message = %q, want %q", got, n.Body)
	}
	if got := form.Get("title"); got != n.Title {
		t.Errorf("title = %q, want %q", got, n.Title)
	}
	const wantPriority = "0" // pushoverPriority(LevelWarning)
	if got := form.Get("priority"); got != wantPriority {
		t.Errorf("priority = %q, want %q", got, wantPriority)
	}
}

func TestMatrixRequestShape(t *testing.T) {
	c := newCatcher(t)
	const roomID = "!abc123:matrix.org"
	const token = "syt_abcdef_token"
	cfg := beacon.ChannelConfig{
		Type: "matrix",
		Settings: map[string]string{
			"homeserver":   c.URL(),
			"room_id":      roomID,
			"token_secret": "token",
		},
	}
	resolve := literalResolver(map[string]string{"token": token})
	n := beacon.Notification{Title: "Alert", Body: "something happened", Level: beacon.LevelError}
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPut {
		t.Errorf("method = %q, want PUT", req.Method)
	}
	wantPrefix := "/_matrix/client/v3/rooms/" + roomID + "/send/m.room.message/"
	if !strings.HasPrefix(req.Path, wantPrefix) {
		t.Errorf("path = %q, want prefix %q", req.Path, wantPrefix)
	}
	if auth := req.Header.Get("Authorization"); auth != "Bearer "+token {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer "+token)
	}

	var payload struct {
		MsgType string `json:"msgtype"`
		Body    string `json:"body"`
	}
	decodeJSON(t, req.Body, &payload)
	if payload.MsgType != "m.text" {
		t.Errorf("msgtype = %q, want m.text", payload.MsgType)
	}
	wantBody := n.Title + "\n\n" + n.Body
	if payload.Body != wantBody {
		t.Errorf("body = %q, want %q", payload.Body, wantBody)
	}

	// A second send must use a different transaction ID: Matrix
	// homeservers deduplicate room-send requests by (token, txn ID), so a
	// repeated ID would silently drop every notification after the first.
	firstPath := req.Path
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify (second): %v", err)
	}
	second := c.last(t)
	if second.Path == firstPath {
		t.Errorf("second send reused the transaction ID %q; sends after the first would be silently dropped", firstPath)
	}
}

func TestGatusReportShape(t *testing.T) {
	c := newCatcher(t)
	cfg := beacon.TelemetryConfig{
		Type: "gatus",
		Settings: map[string]string{
			"url":          c.URL(),
			"endpoint_key": "group_myservice",
			"token_secret": "token",
		},
	}
	resolve := literalResolver(map[string]string{"token": "gatus-tok"})
	h := beacon.Health{Name: "nightly-backup", OK: false, Message: "exit code 1", Duration: 250 * time.Millisecond}
	if err := sendHealth(t, cfg, resolve, h); err != nil {
		t.Fatalf("Report: %v", err)
	}

	req := c.last(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	if req.Path != "/api/v1/endpoints/group_myservice/external" {
		t.Errorf("path = %q, want /api/v1/endpoints/group_myservice/external", req.Path)
	}
	if got := req.Query.Get("success"); got != "false" {
		t.Errorf("success = %q, want false", got)
	}
	if got := req.Query.Get("error"); got != h.Message {
		t.Errorf("error = %q, want %q", got, h.Message)
	}
	if got := req.Query.Get("duration"); got != h.Duration.String() {
		t.Errorf("duration = %q, want %q", got, h.Duration.String())
	}
	if auth := req.Header.Get("Authorization"); auth != "Bearer gatus-tok" {
		t.Errorf("Authorization = %q, want %q", auth, "Bearer gatus-tok")
	}

	// A successful report should carry no "error" query parameter at all.
	c2 := newCatcher(t)
	cfg2 := beacon.TelemetryConfig{
		Type: "gatus",
		Settings: map[string]string{
			"url":          c2.URL(),
			"endpoint_key": "group_myservice",
		},
	}
	hOK := beacon.Health{Name: "nightly-backup", OK: true, Duration: 10 * time.Millisecond}
	if err := sendHealth(t, cfg2, literalResolver(nil), hOK); err != nil {
		t.Fatalf("Report (success): %v", err)
	}
	req2 := c2.last(t)
	if got := req2.Query.Get("success"); got != "true" {
		t.Errorf("success = %q, want true", got)
	}
	if req2.Query.Has("error") {
		t.Errorf("successful report should not include an error param, got %q", req2.Query.Get("error"))
	}
}
