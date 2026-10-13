// Package push delivers Web Push notifications (RFC 8030 + VAPID) to users'
// subscribed browsers — no third-party service involved; the browser vendor's
// push endpoint is contacted directly from this server.
package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/skyleb/talaria/internal/model"
	"github.com/skyleb/talaria/internal/store"
)

type Sender struct {
	st      *store.Store
	log     *slog.Logger
	public  string
	private string
	contact string
}

// Notification is the payload the service worker renders.
type Notification struct {
	Type      string `json:"type"` // "message" | "call"
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url"`
	Tag       string `json:"tag"`
	ChannelID string `json:"channel_id"`
	FromID    string `json:"from_id"`
	Icon      string `json:"icon,omitempty"`
}

// New loads the server's VAPID key pair from settings, generating and saving
// one on first run. contact is the VAPID subject (mailto: or https: URL).
func New(ctx context.Context, st *store.Store, log *slog.Logger, contact string) (*Sender, error) {
	pub, _ := st.GetSetting(ctx, "vapid_public")
	priv, _ := st.GetSetting(ctx, "vapid_private")
	if pub == "" || priv == "" {
		var err error
		priv, pub, err = webpush.GenerateVAPIDKeys()
		if err != nil {
			return nil, err
		}
		if err := st.SetSetting(ctx, "vapid_public", pub); err != nil {
			return nil, err
		}
		if err := st.SetSetting(ctx, "vapid_private", priv); err != nil {
			return nil, err
		}
		log.Info("generated web push (VAPID) keys")
	}
	if contact == "" {
		contact = "https://github.com/skyleb/talaria"
	}
	return &Sender{st: st, log: log, public: pub, private: priv, contact: contact}, nil
}

func (s *Sender) PublicKey() string { return s.public }

// SendToUsers pushes n to every subscribed device of the given users. Dead
// subscriptions (404/410 from the push service) are pruned.
func (s *Sender) SendToUsers(ctx context.Context, userIDs []string, n Notification) {
	subs, err := s.st.PushSubscriptionsForUsers(ctx, userIDs)
	if err != nil {
		s.log.Error("load push subscriptions", "err", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	payload, err := json.Marshal(n)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Add(1)
		go func(sub *model.PushSubscription) {
			defer wg.Done()
			s.sendOne(ctx, sub, payload, n.Type == "call")
		}(sub)
	}
	wg.Wait()
}

func (s *Sender) sendOne(ctx context.Context, sub *model.PushSubscription, payload []byte, urgent bool) {
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	opts := &webpush.Options{
		Subscriber:      s.contact,
		VAPIDPublicKey:  s.public,
		VAPIDPrivateKey: s.private,
		TTL:             300,
		Urgency:         webpush.UrgencyNormal,
	}
	if urgent {
		opts.TTL = 45
		opts.Urgency = webpush.UrgencyHigh
	}
	resp, err := webpush.SendNotificationWithContext(sendCtx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh},
	}, opts)
	if err != nil {
		s.log.Debug("push send failed", "err", err)
		return
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		// The browser unsubscribed or the device is gone.
		s.st.DeletePushEndpoint(context.Background(), sub.Endpoint)
	default:
		if resp.StatusCode >= 400 {
			s.log.Debug("push rejected", "status", resp.StatusCode, "endpoint", sub.Endpoint[:min(40, len(sub.Endpoint))])
		}
	}
}
