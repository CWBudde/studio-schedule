package main

import (
	"fmt"
	"log/slog"

	"github.com/cwbudde/studio-schedule/cmd/studio-schedule/cli"
	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/mail"
	"github.com/cwbudde/studio-schedule/internal/mail/smtp"
)

// newDelivery builds the senders of cfg (cli.System.Delivery): the SMTP
// sender of SMTP_*, nil without SMTP_HOST, and the web push sender of
// VAPID_*, nil without the keys. serve and the commands that send (jobs,
// mail test) build them here, so they cannot drift.
func newDelivery(cfg config.Config, clk clock.Clock, logger *slog.Logger) (cli.Delivery, error) {
	mailer, err := newMailer(cfg, clk, logger)
	if err != nil {
		return cli.Delivery{}, err
	}

	pusher, err := newPusher(cfg, clk, logger)
	if err != nil {
		return cli.Delivery{}, err
	}

	return cli.Delivery{Mailer: mailer, Pusher: pusher}, nil
}

// newMailer returns the SMTP sender of cfg, or nil without SMTP_HOST: then
// mails stay in the outbox.
func newMailer(cfg config.Config, clk clock.Clock, logger *slog.Logger) (mail.Mailer, error) {
	if cfg.SMTP.Host == "" {
		if cfg.DatabaseURL != "" {
			logger.Warn("no mail server configured: mails stay in the outbox and are never sent",
				slog.String("hint", "set "+config.EnvSMTPHost+" and "+config.EnvSMTPFrom))
		}

		return nil, nil // no mailer is a valid configuration
	}

	sender, err := smtp.New(smtp.Config{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password,
		From: cfg.SMTP.From, TLS: smtp.TLSMode(cfg.SMTP.TLS),
	}, clk)
	if err != nil {
		return nil, fmt.Errorf("mail server: %w", err)
	}

	return sender, nil
}
