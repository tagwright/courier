// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	RegisterBackend("matrix", newMatrixBackendFromSettings)
}

// matrixMessage is the JSON body Matrix's room-send endpoint expects for an
// m.room.message event.
type matrixMessage struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

// MatrixBackend sends notifications to a Matrix room as a bot user, via the
// client-server API's room-send endpoint. It only sends: the bot account
// named by token_secret must already be joined to the room, since this
// backend has no way to accept or act on an invite.
type MatrixBackend struct {
	homeserver   string
	roomID       string
	msgType      string
	tokenSetting string
	resolve      SecretResolver
}

// newMatrixBackendFromSettings builds the registered "matrix" backend.
// Settings requires "homeserver" (the base URL, e.g. https://matrix.org)
// and "room_id" (the target room, e.g. !abc:matrix.org). The optional
// "msgtype" setting selects the Matrix message type, defaulting to
// "m.text"; "m.notice" is also common for bot traffic that clients should
// not push-notify as loudly. The secret "token_secret" names the bot's
// access token, required and resolved at send time.
func newMatrixBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	homeserver, err := requiredSetting(settings, "homeserver")
	if err != nil {
		return nil, err
	}
	roomID, err := requiredSetting(settings, "room_id")
	if err != nil {
		return nil, err
	}
	if _, err := requiredSetting(settings, "token_secret"); err != nil {
		return nil, err
	}

	msgType := settings["msgtype"]
	if msgType == "" {
		msgType = "m.text"
	}

	return &MatrixBackend{
		homeserver:   strings.TrimRight(homeserver, "/"),
		roomID:       roomID,
		msgType:      msgType,
		tokenSetting: settings["token_secret"],
		resolve:      resolve,
	}, nil
}

// Name returns "matrix".
func (b *MatrixBackend) Name() string {
	return "matrix"
}

// Send PUTs n to the room-send endpoint for the configured room, combining
// Title and Body into the message's "body" field. Every call generates a
// fresh transaction ID: Matrix homeservers deduplicate room-send requests
// by (access token, transaction ID), so reusing one across sends would
// cause every notification after the first to be silently dropped instead
// of posted.
func (b *MatrixBackend) Send(ctx context.Context, n Notification) error {
	token, err := resolveSecretByName(b.resolve, b.tokenSetting)
	if err != nil {
		return err
	}

	target := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		b.homeserver, url.PathEscape(b.roomID), url.PathEscape(newMatrixTxnID()))

	msg := matrixMessage{
		MsgType: b.msgType,
		Body:    combineTitleBody(n),
	}
	headers := map[string]string{"Authorization": "Bearer " + token}

	_, err = putJSON(ctx, target, msg, headers)
	return err
}

// newMatrixTxnID returns a transaction ID unique to this process and call:
// a nanosecond timestamp (unique across calls in practice, but not
// guaranteed on platforms with coarse clock resolution or under heavy
// concurrency) combined with a random suffix from crypto/rand, so two
// sends can never collide even if the clock does not advance between them.
func newMatrixTxnID() string {
	var suffix [8]byte
	_, _ = rand.Read(suffix[:]) // best-effort: the timestamp alone is already unique in the overwhelmingly common case
	return fmt.Sprintf("beacon-%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))
}

// putJSON marshals body as JSON and PUTs it to target, applying headers on
// top of a Content-Type of application/json. It mirrors postJSON in
// http.go, which only offers POST; Matrix's room-send endpoint is a PUT.
// It returns the response body on a 2xx response, or an error naming the
// status code and a truncated response body otherwise.
func putJSON(ctx context.Context, target string, body any, headers map[string]string) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding JSON body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}
