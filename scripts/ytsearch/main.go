// Command ytsearch exercises the keyless YouTube search from the shell.
//
//	go run ./scripts/ytsearch "query"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skyleb/talaria/internal/listen"
)

func main() {
	q := strings.Join(os.Args[1:], " ")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	res, err := listen.SearchYouTube(ctx, q, os.Getenv("YOUTUBE_API_KEY"))
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	fmt.Printf("%d results in %s\n", len(res), time.Since(start).Round(time.Millisecond))
	for _, r := range res {
		fmt.Printf("  %s  %-55.55s  %-22.22s  %ds\n", r.ID, r.Title, r.Channel, r.DurationMS/1000)
	}
}
