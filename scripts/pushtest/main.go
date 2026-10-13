// Command pushtest sends one Web Push notification to a single subscription
// and prints the push service's full response, including the body that the
// server only logs as a status code. Useful when Apple/Google reject pushes
// with a bare 403.
//
// The VAPID keys live in the settings table and the subscription in
// push_subscriptions; pass them explicitly:
//
//	go run ./scripts/pushtest -endpoint ... -p256dh ... -auth ... \
//	  -pub <vapid_public> -priv <vapid_private> -sub you@example.com
//
// Give -sub a bare e-mail address or an https: URL; webpush-go adds the
// mailto: prefix itself and Apple rejects a doubled one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

func main() {
	endpoint := flag.String("endpoint", "", "subscription endpoint URL")
	p256dh := flag.String("p256dh", "", "subscription p256dh key")
	auth := flag.String("auth", "", "subscription auth secret")
	pub := flag.String("pub", "", "VAPID public key")
	priv := flag.String("priv", "", "VAPID private key")
	sub := flag.String("sub", "", "VAPID subject (mailto: or https: URL)")
	title := flag.String("title", "Talaria test", "notification title")
	body := flag.String("body", "Push delivery works.", "notification body")
	flag.Parse()
	for name, v := range map[string]string{"endpoint": *endpoint, "p256dh": *p256dh, "auth": *auth, "pub": *pub, "priv": *priv, "sub": *sub} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "missing -%s\n", name)
			os.Exit(2)
		}
	}
	payload, _ := json.Marshal(map[string]string{"title": *title, "body": *body, "type": "test"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: *endpoint,
		Keys:     webpush.Keys{Auth: *auth, P256dh: *p256dh},
	}, &webpush.Options{
		Subscriber: *sub, VAPIDPublicKey: *pub, VAPIDPrivateKey: *priv, TTL: 60, Urgency: webpush.UrgencyHigh,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	for _, h := range []string{"Apns-Id", "Location", "Content-Type"} {
		if v := resp.Header.Get(h); v != "" {
			fmt.Printf("%s: %s\n", h, v)
		}
	}
	fmt.Println(string(raw))
	if resp.StatusCode >= 300 {
		os.Exit(1)
	}
}
