package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
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

type Hit struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
}

func Search(ctx context.Context, apiKey, query string) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Hit{}, nil
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("YouTube search is not configured")
	}
	endpoint, err := url.Parse("https://www.googleapis.com/youtube/v3/search")
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("part", "snippet")
	params.Set("type", "video")
	params.Set("videoEmbeddable", "true")
	params.Set("maxResults", "8")
	params.Set("q", query)
	params.Set("key", apiKey)
	endpoint.RawQuery = params.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Error.Message != "" {
			return nil, fmt.Errorf("%s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("YouTube search failed")
	}
	var payload struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title      string `json:"title"`
				Channel    string `json:"channelTitle"`
				Thumbnails map[string]struct {
					URL string `json:"url"`
				} `json:"thumbnails"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := []Hit{}
	for _, item := range payload.Items {
		if !idPattern.MatchString(item.ID.VideoID) {
			continue
		}
		thumb := item.Snippet.Thumbnails["medium"].URL
		if thumb == "" {
			thumb = item.Snippet.Thumbnails["high"].URL
		}
		if thumb == "" {
			thumb = item.Snippet.Thumbnails["default"].URL
		}
		out = append(out, Hit{
			ID:        item.ID.VideoID,
			Title:     html.UnescapeString(item.Snippet.Title),
			Channel:   html.UnescapeString(item.Snippet.Channel),
			Thumbnail: thumb,
		})
	}
	return out, nil
}
