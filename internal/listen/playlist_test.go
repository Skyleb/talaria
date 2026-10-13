package listen

import "testing"

func TestParseYouTubePlaylistID(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/playlist?list=PLabc123DEF456_-ghi":                  "PLabc123DEF456_-ghi",
		"https://music.youtube.com/playlist?list=OLAK5uy_kXyz0123456789abcdefg&si=x": "OLAK5uy_kXyz0123456789abcdefg",
		"youtube.com/playlist?list=PLabc123DEF456_-ghi":                              "PLabc123DEF456_-ghi",
		// A video inside a playlist stays a single video.
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PLabc123DEF456_-ghi": "",
		// Mixes are per-viewer and cannot be fetched.
		"https://www.youtube.com/playlist?list=RDdQw4w9WgXcQ":   "",
		"https://youtu.be/dQw4w9WgXcQ":                          "",
		"https://example.com/playlist?list=PLabc123DEF456_-ghi": "",
	}
	for in, want := range cases {
		got, ok := ParseYouTubePlaylistID(in)
		if (want == "") == ok || got != want {
			t.Errorf("%s: got (%q,%v) want %q", in, got, ok, want)
		}
	}
}
