// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

//go:build integration

package integration

import (
	"testing"

	"github.com/tagwright/courier"
)

// gotifyAdminUser and gotifyAdminPass are the default credentials the
// gotify/server image ships with. run.sh does not change them, since this
// harness only ever talks to a throwaway beacon-itest-gotify container.
const (
	gotifyAdminUser = "admin"
	gotifyAdminPass = "admin"
)

type gotifyApplication struct {
	ID    int    `json:"id"`
	Token string `json:"token"`
}

type gotifyClient struct {
	ID    int    `json:"id"`
	Token string `json:"token"`
}

type gotifyMessageList struct {
	Messages []struct {
		AppID    int    `json:"appid"`
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	} `json:"messages"`
}

// TestGotifyDelivery proves beacon's gotify backend actually delivers: it
// creates a real application token against a live Gotify server (the same
// way a Gotify admin would), sends a Notification through beacon's gotify
// backend, then reads the message back via Gotify's own REST API using a
// separately-created client token.
func TestGotifyDelivery(t *testing.T) {
	server := mustEnv(t, "GOTIFY_URL")

	var app gotifyApplication
	httpJSON(t, "POST", server+"/application", gotifyAdminUser, gotifyAdminPass,
		map[string]string{"name": "beacon-itest-app"}, &app)
	if app.Token == "" {
		t.Fatal("gotify: application token is empty")
	}

	var client gotifyClient
	httpJSON(t, "POST", server+"/client", gotifyAdminUser, gotifyAdminPass,
		map[string]string{"name": "beacon-itest-client"}, &client)
	if client.Token == "" {
		t.Fatal("gotify: client token is empty")
	}

	cfg := courier.ChannelConfig{
		Type: "gotify",
		Settings: map[string]string{
			"server":       server,
			"token_secret": "apptoken",
		},
	}
	n := courier.Notification{
		Title: "beacon gotify integration test",
		Body:  "this message proves the gotify backend delivers",
		Level: courier.LevelError,
	}
	resolve := literalResolver(map[string]string{"apptoken": app.Token})
	if err := sendNotification(t, cfg, resolve, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var list gotifyMessageList
	found := false
	waitFor(t, defaultWait, "message to appear via Gotify /message", func() bool {
		list = gotifyMessageList{}
		httpJSON(t, "GET", server+"/message?token="+client.Token, "", "", nil, &list)
		for _, m := range list.Messages {
			if m.Title == n.Title && m.AppID == app.ID {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Fatalf("gotify: message %q from app %d never appeared: %+v", n.Title, app.ID, list)
	}

	for _, m := range list.Messages {
		if m.Title != n.Title || m.AppID != app.ID {
			continue
		}
		if m.Message != n.Body {
			t.Errorf("message = %q, want %q", m.Message, n.Body)
		}
		const wantPriority = 5 // levelPriority(LevelError)
		if m.Priority != wantPriority {
			t.Errorf("priority = %d, want %d", m.Priority, wantPriority)
		}
	}
}
