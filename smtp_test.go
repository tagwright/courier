// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// smtpCatcher is a minimal in-process SMTP server used to observe the exact
// AUTH exchange a backend performs, without a real relay or network egress.
// It advertises a chosen set of AUTH mechanisms, drives one session through to
// a successful send, and records the AUTH command and any continuation line so
// a test can assert what was put on the wire.
type smtpCatcher struct {
	ln       net.Listener
	mechs    string // space-separated AUTH mechanisms to advertise, "" for none
	authCmd  string // the raw "AUTH ..." command line received
	authCont string // the raw continuation line received (CRAM-MD5 response), if any
}

// newSMTPCatcher starts a catcher advertising the given mechanisms and returns
// it with the host and port to point a backend at. It stops when the test ends.
func newSMTPCatcher(t *testing.T, mechs string) (*smtpCatcher, string, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &smtpCatcher{ln: ln, mechs: mechs}
	go c.serve()
	t.Cleanup(func() { ln.Close() })

	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return c, host, port
}

func (c *smtpCatcher) serve() {
	conn, err := c.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	r := bufio.NewReader(conn)
	w := func(s string) { fmt.Fprintf(conn, "%s\r\n", s) }

	w("220 localhost ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250-localhost")
			if c.mechs != "" {
				w("250-AUTH " + c.mechs)
			}
			w("250 SMTPUTF8")
		case strings.HasPrefix(cmd, "AUTH"):
			c.authCmd = line
			switch {
			case strings.Contains(cmd, "CRAM-MD5"):
				// Issue a challenge, read the client's response line, accept.
				w("334 " + base64.StdEncoding.EncodeToString([]byte("<challenge@localhost>")))
				cont, err := r.ReadString('\n')
				if err != nil {
					return
				}
				c.authCont = strings.TrimRight(cont, "\r\n")
				w("235 2.7.0 Authentication successful")
			default:
				// XOAUTH2 and other single-shot mechanisms carry their
				// initial response on the AUTH line itself.
				w("235 2.7.0 Authentication successful")
			}
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
			w("250 2.1.0 OK")
		case strings.HasPrefix(cmd, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			for {
				body, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(body, "\r\n") == "." {
					break
				}
			}
			w("250 2.0.0 OK")
		case strings.HasPrefix(cmd, "QUIT"):
			w("221 2.0.0 Bye")
			return
		default:
			w("250 2.0.0 OK")
		}
	}
}

// TestSMTPXOAUTH2 proves the xoauth2 auth mode forms the XOAUTH2 SASL exchange
// from the injected username and access token: the AUTH command names XOAUTH2
// and its base64 initial response is the documented
// "user=<u>\x01auth=Bearer <token>\x01\x01" client string. The token is a
// fake handed in through the resolver; no real provider and no OAuth flow are
// involved.
func TestSMTPXOAUTH2(t *testing.T) {
	catcher, host, port := newSMTPCatcher(t, "XOAUTH2")

	const (
		fakeUser  = "alerts@example.com"
		fakeToken = "ya29.FAKE-access-token-value"
	)
	resolve := func(name string) (string, error) {
		switch name {
		case "smtp_account":
			return fakeUser, nil
		case "smtp_token":
			return fakeToken, nil
		}
		return "", errUnknownSecret
	}

	b, err := newSMTPBackendFromSettings(map[string]string{
		"host":            host,
		"port":            port,
		"from":            "alerts@example.com",
		"to":              "ops@example.com",
		"encryption":      "none",
		"auth":            "xoauth2",
		"username_secret": "smtp_account",
		"token_secret":    "smtp_token",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Send(ctx, Notification{Title: "hi", Body: "body"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.HasPrefix(strings.ToUpper(catcher.authCmd), "AUTH XOAUTH2") {
		t.Fatalf("AUTH command = %q, want it to select XOAUTH2", catcher.authCmd)
	}
	fields := strings.Fields(catcher.authCmd)
	if len(fields) < 3 {
		t.Fatalf("AUTH command %q carries no initial response", catcher.authCmd)
	}
	raw, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		t.Fatalf("decoding XOAUTH2 initial response %q: %v", fields[2], err)
	}
	want := "user=" + fakeUser + "\x01auth=Bearer " + fakeToken + "\x01\x01"
	if string(raw) != want {
		t.Errorf("XOAUTH2 client string\n got: %q\nwant: %q", raw, want)
	}
}

// TestSMTPPasswordAuthUnchanged proves the default auth mode still negotiates
// username/password credentials exactly as before this change: with no "auth"
// setting, a relay advertising CRAM-MD5 draws an AUTH CRAM-MD5 exchange driven
// by the resolved username and password.
func TestSMTPPasswordAuthUnchanged(t *testing.T) {
	catcher, host, port := newSMTPCatcher(t, "CRAM-MD5")

	resolve := func(name string) (string, error) {
		switch name {
		case "smtp_user":
			return "relayuser", nil
		case "smtp_pass":
			return "relaypass", nil
		}
		return "", errUnknownSecret
	}

	b, err := newSMTPBackendFromSettings(map[string]string{
		"host":            host,
		"port":            port,
		"from":            "alerts@example.com",
		"to":              "ops@example.com",
		"encryption":      "none",
		"username_secret": "smtp_user",
		"password_secret": "smtp_pass",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Send(ctx, Notification{Title: "hi", Body: "body"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.HasPrefix(strings.ToUpper(catcher.authCmd), "AUTH CRAM-MD5") {
		t.Fatalf("AUTH command = %q, want a CRAM-MD5 password exchange", catcher.authCmd)
	}
	if catcher.authCont == "" {
		t.Error("CRAM-MD5 continuation (username + digest) was never sent")
	}
}

// TestSMTPConfigErrors covers construction-time validation of the auth modes,
// including that the additive xoauth2 mode does not disturb the existing
// username/password pairing rule.
func TestSMTPConfigErrors(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		s := map[string]string{
			"host": "localhost", "from": "a@b.c", "to": "d@e.f",
		}
		for k, v := range extra {
			s[k] = v
		}
		return s
	}
	cases := []struct {
		name    string
		extra   map[string]string
		wantErr bool
	}{
		{"default no auth", nil, false},
		{"default username+password", map[string]string{"username_secret": "u", "password_secret": "p"}, false},
		{"default username only", map[string]string{"username_secret": "u"}, true},
		{"default password only", map[string]string{"password_secret": "p"}, true},
		{"default with stray token", map[string]string{"token_secret": "t"}, true},
		{"xoauth2 complete", map[string]string{"auth": "xoauth2", "username_secret": "u", "token_secret": "t"}, false},
		{"xoauth2 missing token", map[string]string{"auth": "xoauth2", "username_secret": "u"}, true},
		{"xoauth2 missing username", map[string]string{"auth": "xoauth2", "token_secret": "t"}, true},
		{"xoauth2 with password", map[string]string{"auth": "xoauth2", "username_secret": "u", "token_secret": "t", "password_secret": "p"}, true},
		{"unknown auth mode", map[string]string{"auth": "kerberos"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newSMTPBackendFromSettings(base(tc.extra), nil)
			if tc.wantErr && err == nil {
				t.Error("expected a construction error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
