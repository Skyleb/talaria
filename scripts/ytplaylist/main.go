// ytplaylist prints what Talaria would import from a YouTube / YouTube Music
// playlist or album link. Handy for checking the keyless page parser.
//
//	go run ./scripts/ytplaylist "https://music.youtube.com/playlist?list=OLAK5uy_..."
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/skyleb/talaria/internal/listen"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ytplaylist <playlist-url> [api-key]")
		os.Exit(2)
	}
	id, ok := listen.ParseYouTubePlaylistID(os.Args[1])
	if !ok {
		fmt.Fprintln(os.Stderr, "not a playlist/album link")
		os.Exit(1)
	}
	key := ""
	if len(os.Args) > 2 {
		key = os.Args[2]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	start := time.Now()
	title, entries, err := listen.FetchYouTubePlaylist(ctx, id, key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("%q: %d videos in %s\n", title, len(entries), time.Since(start).Round(time.Millisecond))
	for i, e := range entries {
		if i >= 8 {
			fmt.Printf("  … %d more\n", len(entries)-8)
			break
		}
		fmt.Printf("  %2d. %s — %s (%ds)\n", i+1, e.Title, e.Author, e.DurationMS/1000)
	}
}
