// SPDX-License-Identifier: GPL-3.0-or-later

package beacon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpTimeout bounds every request made through postJSON and postForm. A
// notification send is meant to be quick and must never hang a caller
// indefinitely on a slow or unreachable service.
const httpTimeout = 10 * time.Second

// maxErrorBodyLen bounds how much of a non-2xx response body is echoed back
// in an error, so a misbehaving server cannot balloon an error message.
const maxErrorBodyLen = 512

var httpClient = &http.Client{Timeout: httpTimeout}

// postJSON marshals body as JSON and POSTs it to target, applying headers on
// top of a Content-Type of application/json. It returns the response body on
// a 2xx response, or an error naming the status code and a truncated
// response body otherwise.
func postJSON(ctx context.Context, target string, body any, headers map[string]string) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding JSON body: %w", err)
	}
	return postJSONBytes(ctx, target, encoded, headers)
}

// postJSONBytes POSTs an already-encoded JSON body to target. It exists
// alongside postJSON for callers that must know the exact bytes sent, such
// as a backend that signs the body before transmission: marshaling twice
// could reorder map keys and silently invalidate a signature computed over
// the first encoding.
func postJSONBytes(ctx context.Context, target string, encoded []byte, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}

// postText POSTs body as a raw request body to target, applying headers
// with no default Content-Type. It is meant for services such as ntfy that
// accept the message as a plain-text body rather than JSON or form fields.
// It returns the response body on a 2xx response, or an error naming the
// status code and a truncated response body otherwise.
func postText(ctx context.Context, target, body string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}

// postForm URL-encodes form and POSTs it to target, applying headers on top
// of a Content-Type of application/x-www-form-urlencoded. It returns the
// response body on a 2xx response, or an error naming the status code and a
// truncated response body otherwise.
func postForm(ctx context.Context, target string, form url.Values, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return doRequest(req)
}

func doRequest(req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", req.URL.Host, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned status %d: %s", req.URL.Host, resp.StatusCode, truncateBody(respBody))
	}

	return respBody, nil
}

func truncateBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) <= maxErrorBodyLen {
		return s
	}
	return s[:maxErrorBodyLen] + "...(truncated)"
}

// levelPriority maps a Level to the 1-5 priority scale used by several
// notification services (ntfy, Gotify), where 3 is the default, non-urgent
// priority and 5 is the most urgent. Backends with a different native scale
// convert from this rather than switching on Level themselves, so the
// ordering stays consistent everywhere it appears.
func levelPriority(l Level) int {
	switch l {
	case LevelError:
		return 5
	case LevelWarning:
		return 4
	default:
		return 3
	}
}

// combineTitleBody joins a notification's Title and Body into a single
// block of text for services that accept only one message field, such as
// Telegram, Slack, and Mattermost.
func combineTitleBody(n Notification) string {
	switch {
	case n.Title == "":
		return n.Body
	case n.Body == "":
		return n.Title
	default:
		return n.Title + "\n\n" + n.Body
	}
}

// requiredSetting returns settings[key], or an error if it is absent or
// empty. It is used for literal, non-secret settings that a backend cannot
// operate without.
func requiredSetting(settings map[string]string, key string) (string, error) {
	v := settings[key]
	if v == "" {
		return "", fmt.Errorf("missing required setting %q", key)
	}
	return v, nil
}

// resolveSecretByName resolves an already-validated secret name through
// resolve, erroring if it resolves to an empty value. It is used at send
// time by backends that stored the secret name (not the settings map) at
// construction.
func resolveSecretByName(resolve SecretResolver, name string) (string, error) {
	val, err := resolve(name)
	if err != nil {
		return "", fmt.Errorf("resolving secret %q: %w", name, err)
	}
	if val == "" {
		return "", fmt.Errorf("secret %q resolved to an empty value", name)
	}
	return val, nil
}

// resolveRequiredSecret reads the secret name out of settings[key] and
// resolves it through resolve. It errors if the setting naming the secret is
// absent, or if the secret it names resolves to an empty value, so a
// backend never sends with a silently-missing credential.
func resolveRequiredSecret(settings map[string]string, key string, resolve SecretResolver) (string, error) {
	name, err := requiredSetting(settings, key)
	if err != nil {
		return "", err
	}
	return resolveSecretByName(resolve, name)
}
