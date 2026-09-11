// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 techgaud

package courier

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	mail "github.com/wneessen/go-mail"
)

func init() {
	RegisterBackend("smtp", newSMTPBackendFromSettings)
}

// defaultSMTPSubject is used when a Notification has no Title, since an
// email with no subject line reads as broken in most clients.
const defaultSMTPSubject = "Notification"

// SMTP auth modes selectable through the "auth" setting.
const (
	// smtpAuthAuto is the default: username/password credentials, if any,
	// negotiated with go-mail's auto-discover. This is the historical
	// behavior and the value assumed when "auth" is unset.
	smtpAuthAuto = "auto"

	// smtpAuthXOAUTH2 authenticates with the XOAUTH2 SASL mechanism, using a
	// username and an OAuth2 access token. courier does not acquire or
	// refresh the token; the host resolves an already-valid one through the
	// injected resolver, exactly as it does the SMTP password.
	smtpAuthXOAUTH2 = "xoauth2"
)

// SMTPBackend sends notifications as email through an SMTP relay.
type SMTPBackend struct {
	host        string
	port        int
	from        string
	to          []string
	tlsPolicy   mail.TLSPolicy
	implicitTLS bool

	authMode        string
	usernameSetting string
	passwordSetting string
	tokenSetting    string
	resolve         SecretResolver
}

// newSMTPBackendFromSettings builds the registered "smtp" backend. Settings
// requires "host", "from", and "to" (a comma-separated address list).
// "port" is optional and defaults to go-mail's own default. "encryption" is
// one of "starttls" (the default), "tls", or "none".
//
// "auth" selects the authentication mode:
//
//   - "" or "auto" (default): username/password. The secrets
//     "username_secret" and "password_secret" are both optional, for relays
//     that accept unauthenticated mail; if either is set, both are required.
//   - "xoauth2": the XOAUTH2 SASL mechanism, required by Gmail and O365,
//     which have disabled basic-auth SMTP. It uses "username_secret" (the
//     account) and "token_secret" (the OAuth2 access token), both resolved
//     through the injected resolver at send time. courier only uses the
//     token it is handed; it never runs the OAuth flow, and never acquires,
//     refreshes, or stores a token. That is the host's job.
func newSMTPBackendFromSettings(settings map[string]string, resolve SecretResolver) (Backend, error) {
	host, err := requiredSetting(settings, "host")
	if err != nil {
		return nil, err
	}
	from, err := requiredSetting(settings, "from")
	if err != nil {
		return nil, err
	}
	toRaw, err := requiredSetting(settings, "to")
	if err != nil {
		return nil, err
	}
	to := splitAndTrim(toRaw)
	if len(to) == 0 {
		return nil, fmt.Errorf("setting %q has no addresses", "to")
	}

	port := 0
	if p := settings["port"]; p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("setting %q: %w", "port", err)
		}
	}

	tlsPolicy, implicitTLS, err := smtpEncryptionPolicy(settings["encryption"])
	if err != nil {
		return nil, err
	}

	usernameSetting := settings["username_secret"]
	passwordSetting := settings["password_secret"]
	tokenSetting := settings["token_secret"]

	authMode := settings["auth"]
	switch authMode {
	case "", smtpAuthAuto:
		authMode = smtpAuthAuto
		if (usernameSetting == "") != (passwordSetting == "") {
			return nil, fmt.Errorf("%q and %q must both be set or both be absent", "username_secret", "password_secret")
		}
		if tokenSetting != "" {
			return nil, fmt.Errorf("setting %q is only used with auth %q", "token_secret", smtpAuthXOAUTH2)
		}
	case smtpAuthXOAUTH2:
		if usernameSetting == "" || tokenSetting == "" {
			return nil, fmt.Errorf("auth %q requires both %q and %q", smtpAuthXOAUTH2, "username_secret", "token_secret")
		}
		if passwordSetting != "" {
			return nil, fmt.Errorf("setting %q is not used with auth %q; set %q instead", "password_secret", smtpAuthXOAUTH2, "token_secret")
		}
	default:
		return nil, fmt.Errorf("setting %q: unknown value %q", "auth", authMode)
	}

	return &SMTPBackend{
		host:            host,
		port:            port,
		from:            from,
		to:              to,
		tlsPolicy:       tlsPolicy,
		implicitTLS:     implicitTLS,
		authMode:        authMode,
		usernameSetting: usernameSetting,
		passwordSetting: passwordSetting,
		tokenSetting:    tokenSetting,
		resolve:         resolve,
	}, nil
}

func smtpEncryptionPolicy(encryption string) (policy mail.TLSPolicy, implicitTLS bool, err error) {
	switch encryption {
	case "", "starttls":
		return mail.TLSMandatory, false, nil
	case "tls":
		return mail.TLSMandatory, true, nil
	case "none":
		return mail.NoTLS, false, nil
	default:
		return 0, false, fmt.Errorf("setting %q: unknown value %q", "encryption", encryption)
	}
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Name returns "smtp".
func (b *SMTPBackend) Name() string {
	return "smtp"
}

// Send dials the configured relay and delivers n as an email. Credentials,
// when configured, are resolved fresh on every call so rotating them never
// requires a rebuild or restart.
func (b *SMTPBackend) Send(ctx context.Context, n Notification) error {
	opts := []mail.Option{mail.WithTLSPolicy(b.tlsPolicy)}
	if b.port != 0 {
		opts = append(opts, mail.WithPort(b.port))
	}
	if b.implicitTLS {
		opts = append(opts, mail.WithSSL())
	}

	switch b.authMode {
	case smtpAuthXOAUTH2:
		username, err := resolveSecretByName(b.resolve, b.usernameSetting)
		if err != nil {
			return err
		}
		// The access token is resolved through the injected resolver, the
		// same path as the SMTP password. courier only uses a token handed
		// to it; acquiring and refreshing it is the host's responsibility.
		token, err := resolveSecretByName(b.resolve, b.tokenSetting)
		if err != nil {
			return err
		}
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthXOAUTH2),
			mail.WithUsername(username),
			mail.WithPassword(token),
		)
	default:
		if b.usernameSetting != "" {
			username, err := resolveSecretByName(b.resolve, b.usernameSetting)
			if err != nil {
				return err
			}
			password, err := resolveSecretByName(b.resolve, b.passwordSetting)
			if err != nil {
				return err
			}
			opts = append(opts,
				mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
				mail.WithUsername(username),
				mail.WithPassword(password),
			)
		}
	}

	client, err := mail.NewClient(b.host, opts...)
	if err != nil {
		return fmt.Errorf("building SMTP client: %w", err)
	}

	subject := n.Title
	if subject == "" {
		subject = defaultSMTPSubject
	}

	msg := mail.NewMsg()
	if err := msg.From(b.from); err != nil {
		return fmt.Errorf("setting From: %w", err)
	}
	if err := msg.To(b.to...); err != nil {
		return fmt.Errorf("setting To: %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, n.Body)

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("sending mail: %w", err)
	}
	return nil
}
