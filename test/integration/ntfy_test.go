// SPDX-License-Identifier: GPL-3.0-or-later

//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tagwright/beacon"
)

// ntfyMessage is the subset of ntfy's JSON message format this test cares
// about. See https://docs.ntfy.sh/subscribe/api/#json-message-format.
type ntfyMessage struct {
	Title    string   `json:"title"`
	Message  string   `json:"message"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags"`
}

func randomTopic(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generating random topic suffix: %v", err)
	}
	return "beacon-itest-" + hex.EncodeToString(b[:])
}

// TestNtfyDelivery proves beacon's ntfy backend actually delivers: it sends
// a Notification through a real ntfy server and polls that server's own
// JSON API to confirm the message arrived with the right title, body, and
// priority.
func TestNtfyDelivery(t *testing.T) {
	server := mustEnv(t, "NTFY_URL")
	topic := randomTopic(t)

	cfg := beacon.ChannelConfig{
		Type: "ntfy",
		Settings: map[string]string{
			"server": server,
			"topic":  topic,
		},
	}
	n := beacon.Notification{
		Title: "beacon ntfy integration test",
		Body:  "this message proves the ntfy backend delivers",
		Level: beacon.LevelWarning,
		Tags:  []string{"warning", "robot"},
	}
	if err := sendNotification(t, cfg, literalResolver(nil), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var got ntfyMessage
	found := false
	waitFor(t, defaultWait, "message to appear in ntfy topic "+topic, func() bool {
		resp, err := http.Get(server + "/" + topic + "/json?poll=1")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return false
		}
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if line == "" {
				continue
			}
			var m ntfyMessage
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				continue
			}
			if m.Title == n.Title {
				got = m
				found = true
			}
		}
		return found
	})

	if got.Message != n.Body {
		t.Errorf("message = %q, want %q", got.Message, n.Body)
	}
	const wantPriority = 4 // levelPriority(LevelWarning)
	if got.Priority != wantPriority {
		t.Errorf("priority = %d, want %d", got.Priority, wantPriority)
	}
	for _, tag := range n.Tags {
		if !containsString(got.Tags, tag) {
			t.Errorf("tags = %v, missing %q", got.Tags, tag)
		}
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
