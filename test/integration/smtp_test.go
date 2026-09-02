// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/tagwright/beacon"
)

type mailpitMessageSummary struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
}

type mailpitMessageList struct {
	Messages []mailpitMessageSummary `json:"messages"`
}

type mailpitMessage struct {
	Subject string `json:"Subject"`
	Text    string `json:"Text"`
}

// TestSMTPDelivery proves beacon's smtp backend actually delivers: it sends
// a Notification as email through a real SMTP relay (mailpit) and queries
// mailpit's own HTTP API to confirm the message arrived with the right
// subject and body.
func TestSMTPDelivery(t *testing.T) {
	host := mustEnv(t, "MAILPIT_SMTP_HOST")
	port := mustEnv(t, "MAILPIT_SMTP_PORT")
	apiURL := mustEnv(t, "MAILPIT_HTTP_URL")

	subject := "beacon smtp integration test " + randomTopic(t)
	cfg := beacon.ChannelConfig{
		Type: "smtp",
		Settings: map[string]string{
			"host":       host,
			"port":       port,
			"from":       "beacon@beacon-itest.local",
			"to":         "itest@beacon-itest.local",
			"encryption": "none",
		},
	}
	n := beacon.Notification{
		Title: subject,
		Body:  "this message proves the smtp backend delivers",
		Level: beacon.LevelInfo,
	}
	if err := sendNotification(t, cfg, literalResolver(nil), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var id string
	waitFor(t, defaultWait, "message to appear in mailpit", func() bool {
		var list mailpitMessageList
		httpJSON(t, "GET", apiURL+"/api/v1/messages", "", "", nil, &list)
		for _, m := range list.Messages {
			if m.Subject == subject {
				id = m.ID
				return true
			}
		}
		return false
	})
	if id == "" {
		t.Fatalf("smtp: message with subject %q never appeared in mailpit", subject)
	}

	var full mailpitMessage
	httpJSON(t, "GET", apiURL+"/api/v1/message/"+id, "", "", nil, &full)
	if full.Subject != subject {
		t.Errorf("subject = %q, want %q", full.Subject, subject)
	}
	if !strings.Contains(full.Text, n.Body) {
		t.Errorf("body = %q, want it to contain %q", full.Text, n.Body)
	}
}
