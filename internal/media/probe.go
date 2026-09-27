package media

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type AudioTrack struct {
	Index int    `json:"index"`
	Codec string `json:"codec"`
	Title string `json:"title"`
	Lang  string `json:"lang,omitempty"`
}

type VideoInfo struct {
	Name        string       `json:"name"`
	Path        string       `json:"path"`
	Duration    float64      `json:"duration"`
	Width       int          `json:"width"`
	Height      int          `json:"height"`
	VideoCodec  string       `json:"videoCodec"`
	AudioTracks []AudioTrack `json:"audioTracks"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	Index     int               `json:"index"`
	CodecType string            `json:"codec_type"`
	CodecName string            `json:"codec_name"`
	Width     int               `json:"width"`
	Height    int               `json:"height"`
	Tags      map[string]string `json:"tags"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

func Probe(absPath, relPath string) (VideoInfo, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_streams",
		"-show_format",
		"-print_format", "json",
		absPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return VideoInfo{}, fmt.Errorf("ffprobe %s: %w", relPath, err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return VideoInfo{}, err
	}

	info := VideoInfo{
		Name: filepath.Base(relPath),
		Path: relPath,
	}
	if probe.Format.Duration != "" {
		info.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	}

	audioN := 0
	for _, s := range probe.Streams {
		switch s.CodecType {
		case "video":
			if info.VideoCodec == "" {
				info.VideoCodec = s.CodecName
				info.Width = s.Width
				info.Height = s.Height
			}
		case "audio":
			title := trackTitle(s.Tags, audioN)
			info.AudioTracks = append(info.AudioTracks, AudioTrack{
				Index: audioN,
				Codec: s.CodecName,
				Title: title,
				Lang:  firstTag(s.Tags, "language", "LANGUAGE"),
			})
			audioN++
		}
	}
	return info, nil
}

func trackTitle(tags map[string]string, n int) string {
	if t := firstTag(tags, "title", "TITLE"); t != "" {
		return t
	}
	if lang := firstTag(tags, "language", "LANGUAGE"); lang != "" && !strings.EqualFold(lang, "und") {
		return fmt.Sprintf("Track %d (%s)", n+1, lang)
	}
	return fmt.Sprintf("Track %d", n+1)
}

func firstTag(tags map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(tags[k]); v != "" {
			return v
		}
	}
	return ""
}
