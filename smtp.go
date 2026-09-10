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

// SMTPBackend sends notifications as email through an SMTP relay.
type SMTPBackend struct {
	host        string
	port        int
	from        string
	to          []string
	tlsPolicy   mail.TLSPolicy
	implicitTLS bool

	usernameSetting string
	passwordSetting string
	resolve         SecretResolver
}

// newSMTPBackendFromSettings builds the registered "smtp" backend. Settings
// requires "host", "from", and "to" (a comma-separated address list).
// "port" is optional and defaults to go-mail's own default. "encryption" is
// one of "starttls" (the default), "tls", or "none". The secrets
// "username_secret" and "password_secret" are both optional, for relays
// that accept unauthenticated mail; if either is set, both are required.
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
	if (usernameSetting == "") != (passwordSetting == "") {
		return nil, fmt.Errorf("%q and %q must both be set or both be absent", "username_secret", "password_secret")
	}

	return &SMTPBackend{
		host:            host,
		port:            port,
		from:            from,
		to:              to,
		tlsPolicy:       tlsPolicy,
		implicitTLS:     implicitTLS,
		usernameSetting: usernameSetting,
		passwordSetting: passwordSetting,
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
