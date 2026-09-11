// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWebhookBearer proves the optional bearer_secret is resolved through the
// injected resolver and sent as an "Authorization: Bearer <token>" header. The
// token is a fake handed in through the resolver; courier never acquires it.
func TestWebhookBearer(t *testing.T) {
	const fakeToken = "fake-access-token"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resolve := func(name string) (string, error) {
		switch name {
		case "hook_url":
			return srv.URL, nil
		case "hook_token":
			return fakeToken, nil
		}
		return "", errUnknownSecret
	}
	b, err := newWebhookBackendFromSettings(map[string]string{
		"url_secret":    "hook_url",
		"bearer_secret": "hook_token",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := b.Send(context.Background(), Notification{Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if want := "Bearer " + fakeToken; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

// TestWebhookSignUnchanged proves the existing HMAC signing path still works
// alongside the additive bearer option: with only sign_secret set, the request
// carries the signature header and no Authorization header.
func TestWebhookSignUnchanged(t *testing.T) {
	const key = "signing-key"
	var gotSig, gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Beacon-Signature")
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resolve := func(name string) (string, error) {
		switch name {
		case "hook_url":
			return srv.URL, nil
		case "hook_key":
			return key, nil
		}
		return "", errUnknownSecret
	}
	b, err := newWebhookBackendFromSettings(map[string]string{
		"url_secret":  "hook_url",
		"sign_secret": "hook_key",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := b.Send(context.Background(), Notification{Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(gotBody)
	if want := hex.EncodeToString(mac.Sum(nil)); gotSig != want {
		t.Errorf("X-Beacon-Signature = %q, want %q", gotSig, want)
	}
	if gotAuth != "" {
		t.Errorf("unexpected Authorization header %q with no bearer_secret", gotAuth)
	}
}

// TestHTTPBackendBearer proves the generic http channel resolves bearer_secret
// through the injected resolver and sets the Authorization header on the POST.
func TestHTTPBackendBearer(t *testing.T) {
	const fakeToken = "fake-api-token"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		io.Copy(io.Discard, r.Body)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	resolve := func(name string) (string, error) {
		if name == "api_token" {
			return fakeToken, nil
		}
		return "", errUnknownSecret
	}
	b, err := newHTTPBackendFromSettings(map[string]string{
		"url":           srv.URL,
		"bearer_secret": "api_token",
		"body_template": `{}`,
		"success_field": "ok",
	}, resolve)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := b.Send(context.Background(), Notification{Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if want := "Bearer " + fakeToken; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}
