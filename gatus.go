// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package beacon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func init() {
	RegisterSink("gatus", newGatusSinkFromSettings)
}

// GatusSink pushes health results to a Gatus external endpoint, Gatus's
// push-based heartbeat API for monitoring things Gatus cannot reach or poll
// itself (a cron job, a background worker, a service behind a firewall).
// Gatus treats the endpoint as unhealthy if it does not receive a push
// within its configured interval, so Report should be called on a
// dead-man's-switch schedule even when h.OK is true.
type GatusSink struct {
	url          string
	endpointKey  string
	tokenSetting string
	resolve      SecretResolver
}

// newGatusSinkFromSettings builds the registered "gatus" sink. Settings
// requires "url" (the Gatus base URL) and "endpoint_key" (the external
// endpoint's key, i.e. the "group_endpoint-name" path segment configured
// for it in Gatus). The optional secret "token_secret" names a bearer
// token, sent as an Authorization header when present.
func newGatusSinkFromSettings(settings map[string]string, resolve SecretResolver) (TelemetrySink, error) {
	base, err := requiredSetting(settings, "url")
	if err != nil {
		return nil, err
	}
	key, err := requiredSetting(settings, "endpoint_key")
	if err != nil {
		return nil, err
	}
	return &GatusSink{
		url:          strings.TrimRight(base, "/"),
		endpointKey:  key,
		tokenSetting: settings["token_secret"],
		resolve:      resolve,
	}, nil
}

// Name returns "gatus".
func (s *GatusSink) Name() string {
	return "gatus"
}

// Report POSTs h to the configured external endpoint's push URL. success is
// h.OK; duration is encoded as a Go duration string (e.g. "250ms"); error is
// included, carrying h.Message, only when h.OK is false. All three are sent
// as URL query parameters, since Gatus's external-endpoint API takes no
// request body.
func (s *GatusSink) Report(ctx context.Context, h Health) error {
	q := url.Values{}
	q.Set("success", strconv.FormatBool(h.OK))
	q.Set("duration", h.Duration.String())
	if !h.OK {
		q.Set("error", h.Message)
	}

	target := fmt.Sprintf("%s/api/v1/endpoints/%s/external?%s",
		s.url, url.PathEscape(s.endpointKey), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	if s.tokenSetting != "" {
		token, err := resolveSecretByName(s.resolve, s.tokenSetting)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	_, err = doRequest(req)
	return err
}
