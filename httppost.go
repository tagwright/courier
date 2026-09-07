// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"text/template"
)

func init() {
	RegisterBackend("http", newHTTPBackendFromSettings)
}

// HTTPBackend is the generic templatable HTTP channel: it renders a
// caller-supplied body template into an HTTP POST and, optionally, checks a
// named field of the JSON response to decide whether the delivery actually
// succeeded. It makes no assumption about the target's request or response
// shape, so any service that accepts an HTTP POST can be reached by
// configuration alone, without a bespoke backend and without a new dependency.
//
// It complements WebhookBackend: "webhook" sends beacon's own fixed JSON
// payload (optionally HMAC-signed), while "http" lets the caller define the
// exact bytes on the wire. Reach for "http" when a receiver dictates its own
// payload shape.
type HTTPBackend struct {
	urlSetting  string // literal URL, or "" when urlSecret is used
	urlSecret   string // secret name resolving to the URL, or ""
	recipient   string
	contentType string
	body        *template.Template
	successField  string
	successEquals string
	haveEquals    bool
	resolve     SecretResolver
}

// templateData is what a body template can reference. It is deliberately
// generic: a message (Title and Body joined), the individual parts, the level,
// an optional recipient the host configured, and any tags or structured
// fields the notification carried.
type templateData struct {
	Message   string
	Title     string
	Body      string
	Level     string
	Recipient string
	Tags      []string
	Fields    map[string]string
}

// templateFuncs are available inside a body template. "tojson" marshals a
// value to a JSON literal (a quoted, escaped string for a string input), so a
// template can safely embed arbitrary text into a JSON body without hand-rolled
// escaping. HTML escaping is disabled so characters like &, <, and > pass
// through as themselves rather than as \u00xx sequences.
var templateFuncs = template.FuncMap{
	"tojson": func(v any) (string, error) {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			return "", err
		}
		// Encoder.Encode appends a newline; drop it so the literal embeds cleanly.
		return string(bytes.TrimRight(buf.Bytes(), "\n")), nil
	},
}

// newHTTPBackendFromSettings builds the registered "http" backend. Settings:
//
//	url            the POST target (literal). Provide this OR url_secret.
//	url_secret     names a secret resolving to the POST target, for a URL
//	               that must not sit in config in the clear.
//	recipient      an optional value exposed to the template as .Recipient.
//	body_template  REQUIRED. A text/template rendered into the request body,
//	               with .Message/.Title/.Body/.Level/.Recipient/.Tags/.Fields
//	               and the tojson function.
//	content_type   request Content-Type, default "application/json".
//	success_field  optional. A top-level field of the JSON response that must
//	               be present for the send to count as delivered.
//	success_equals optional. When set alongside success_field, that field's
//	               value must equal this (compared as text: true, 1, ok). When
//	               omitted, the field only has to be truthy.
//
// With no success_field, any 2xx response is a success, as for a plain webhook.
func newHTTPBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	url, urlSecret := settings["url"], settings["url_secret"]
	switch {
	case url == "" && urlSecret == "":
		return nil, fmt.Errorf("missing required setting: set either %q or %q", "url", "url_secret")
	case url != "" && urlSecret != "":
		return nil, fmt.Errorf("set only one of %q or %q, not both", "url", "url_secret")
	}

	tmplText, err := requiredSetting(settings, "body_template")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("body").Funcs(templateFuncs).Parse(tmplText)
	if err != nil {
		return nil, fmt.Errorf("parsing body_template: %w", err)
	}

	contentType := settings["content_type"]
	if contentType == "" {
		contentType = "application/json"
	}

	equals, haveEquals := settings["success_equals"]
	return &HTTPBackend{
		urlSetting:    url,
		urlSecret:     urlSecret,
		recipient:     settings["recipient"],
		contentType:   contentType,
		body:          tmpl,
		successField:  settings["success_field"],
		successEquals: equals,
		haveEquals:    haveEquals,
		resolve:       resolve,
	}, nil
}

// Name returns "http".
func (b *HTTPBackend) Name() string {
	return "http"
}

// Send renders the body template for n and POSTs it. A transport error or a
// non-2xx status is a failure. When success_field is configured, the JSON
// response is parsed and the field is checked too, so a service that returns
// 200 while reporting a failure in its body (a common pattern) is not
// mistaken for a successful delivery.
func (b *HTTPBackend) Send(ctx context.Context, n Notification) error {
	target := b.urlSetting
	if b.urlSecret != "" {
		var err error
		if target, err = resolveSecretByName(b.resolve, b.urlSecret); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	if err := b.body.Execute(&buf, templateData{
		Message:   combineTitleBody(n),
		Title:     n.Title,
		Body:      n.Body,
		Level:     n.Level.String(),
		Recipient: b.recipient,
		Tags:      n.Tags,
		Fields:    n.Fields,
	}); err != nil {
		return fmt.Errorf("rendering body_template: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", b.contentType)

	respBody, err := doRequest(req)
	if err != nil {
		return err
	}
	return b.checkSuccess(respBody)
}

// checkSuccess applies the optional response-body success check. It returns
// nil when no check is configured.
func (b *HTTPBackend) checkSuccess(respBody []byte) error {
	if b.successField == "" {
		return nil
	}
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("parsing response for success check: %w", err)
	}
	val, ok := resp[b.successField]
	if !ok {
		return fmt.Errorf("response has no success field %q", b.successField)
	}
	if b.haveEquals {
		if got := scalarText(val); got != b.successEquals {
			return fmt.Errorf("success field %q is %q, want %q", b.successField, got, b.successEquals)
		}
		return nil
	}
	if !truthy(val) {
		return fmt.Errorf("success field %q is not truthy (%v)", b.successField, val)
	}
	return nil
}

// scalarText renders a decoded JSON scalar to the text form used for
// success_equals comparison: bools as true/false, numbers without a trailing
// ".0", strings verbatim.
func scalarText(v any) string {
	switch t := v.(type) {
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}

// truthy reports whether a decoded JSON value counts as success when no
// explicit success_equals is set: true, a non-zero number, or a non-empty
// string.
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t != ""
	default:
		return t != nil
	}
}
