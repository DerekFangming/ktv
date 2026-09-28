package youtube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const prefix = "youtube:"

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func ID(file string) (string, bool) {
	id, ok := strings.CutPrefix(file, prefix)
	if !ok || !idPattern.MatchString(id) {
		return "", false
	}
	return id, true
}

func File(id string) string {
	return prefix + id
}

// ParseID accepts a video id or a youtube.com / youtu.be watch URL.
func ParseID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if idPattern.MatchString(raw) {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("enter a YouTube URL")
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(parsed.Path, "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		id = parsed.Query().Get("v")
		if id == "" {
			path := strings.Trim(parsed.Path, "/")
			for _, kind := range []string{"embed/", "shorts/", "live/", "v/"} {
				if rest, ok := strings.CutPrefix(path, kind); ok {
					id = rest
					break
				}
			}
		}
	default:
		return "", fmt.Errorf("enter a YouTube URL")
	}
	id = strings.Split(id, "/")[0]
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("that YouTube URL has no video id")
	}
	return id, nil
}
