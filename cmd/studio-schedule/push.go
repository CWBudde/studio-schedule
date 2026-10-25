package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/config"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/webpush"
)

// newPusher returns the web push sender of cfg (ADR 0017), or nil without
// the VAPID keys: then push is off.
func newPusher(cfg config.Config, clk clock.Clock, logger *slog.Logger) (service.PushSender, error) {
	if !cfg.Push.Enabled() {
		logger.Debug("web push is off", slog.String("hint", "set the VAPID_* variables (studio-schedule vapid-keys)"))

		return nil, nil // no push is a valid configuration
	}

	keys, err := webpush.ParseKeys(cfg.Push.PublicKey, cfg.Push.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("web push: %w", err) // config.Load checked them
	}

	sender, err := webpush.New(webpush.Config{Keys: keys, Subject: cfg.Push.Subject, Clock: clk})
	if err != nil {
		return nil, fmt.Errorf("web push: %w", err)
	}

	return pushSender{sender}, nil
}

// pushSender adapts webpush.Sender to service.PushSender.
type pushSender struct{ s *webpush.Sender }

func (p pushSender) PublicKey() string { return p.s.PublicKey() }

// Send sends with high urgency (an invite runs out) and maps the push
// service's refusals to the service's errors.
func (p pushSender) Send(ctx context.Context, sub domain.PushSubscription, payload []byte, ttl time.Duration) error {
	err := p.s.Send(ctx, webpush.Subscription{Endpoint: sub.Endpoint, P256DH: sub.P256DH, Auth: sub.Auth}, payload,
		webpush.Options{TTL: ttl, Urgency: webpush.UrgencyHigh})

	var status *webpush.StatusError

	switch {
	case err == nil:
		return nil
	case errors.As(err, &status) && status.Gone():
		return fmt.Errorf("%w: %w", service.ErrPushGone, err)
	case errors.As(err, &status) && status.Permanent(),
		errors.Is(err, webpush.ErrPayloadTooLarge), errors.Is(err, webpush.ErrInvalidKey),
		errors.Is(err, webpush.ErrInvalidEndpoint), errors.Is(err, webpush.ErrPrivateAddress):
		return fmt.Errorf("%w: %w", service.ErrPushRefused, err)
	default:
		return err
	}
}
