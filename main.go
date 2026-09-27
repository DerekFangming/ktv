package main

import (
	"crypto/sha1"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"videoplayer/internal/control"
	"videoplayer/internal/media"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	videoDir := os.Getenv("VIDEO_DIR")
	if videoDir == "" {
		videoDir = "/Users/fangming.ning/Documents/video"
	}
	videoDir, err = filepath.Abs(videoDir)
	if err != nil {
		log.Fatal(err)
	}
	cacheDir := filepath.Join(root, ".cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		log.Fatal(err)
	}

	mgr := media.NewManager(cacheDir)
	mgr.StartCacheCleanup(24*time.Hour, 24*time.Hour)
	hub := control.NewHub()
	mux := http.NewServeMux()

	webFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServer(http.FS(webFS))

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, webFS, "index.html")
	})
	mux.HandleFunc("GET /controls", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, webFS, "controls.html")
	})
	mux.HandleFunc("GET /controls-code.png", func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		target := scheme + "://" + r.Host + "/controls"
		png, err := qrcode.Encode(target, qrcode.Medium, 256)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(png)
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))
	mux.HandleFunc("GET /api/videos", func(w http.ResponseWriter, r *http.Request) {
		list, err := listVideos(videoDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, list)
	})
	mux.HandleFunc("GET /api/prepare", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("file")
		abs, err := resolveVideo(videoDir, rel)
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		info, err := media.Probe(abs, rel)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		id := cacheID(rel)
		job, err := mgr.Prepare(id, abs, len(info.AudioTracks))
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := job.WaitReady(45 * time.Second); err != nil {
			writeJSONStatus(w, http.StatusGatewayTimeout, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{
			"id":       job.ID,
			"playlist": job.Playlist,
			"status":   string(job.Status),
		})
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		writeCmd := func(cmd control.Command) error {
			payload, err := json.Marshal(cmd)
			if err != nil {
				return err
			}
			if _, err := w.Write([]byte("event: command\ndata: " + string(payload) + "\n\n")); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		_ = writeCmd(hub.StateCommand())

		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ping.C:
				if _, err := w.Write([]byte(": ping\n\n")); err != nil {
					return
				}
				flusher.Flush()
			case cmd, ok := <-ch:
				if !ok {
					return
				}
				if err := writeCmd(cmd); err != nil {
					return
				}
			}
		}
	})
	reply := func(w http.ResponseWriter, extra map[string]any) {
		n := hub.ClientCount()
		body := map[string]any{"ok": true, "clients": n}
		for k, v := range extra {
			body[k] = v
		}
		writeJSON(w, body)
	}

	playHandler := func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("file")
		if rel == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing file query parameter"})
			return
		}
		if _, err := resolveVideo(videoDir, rel); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		cmd := control.Command{Type: "play", File: rel}
		st := hub.Snapshot()
		st.File = rel
		st.Name = path.Base(rel)
		st.Paused = true
		st.Playing = false
		st.Status = "Starting…"
		n := 1
		if t := strings.TrimSpace(r.URL.Query().Get("track")); t != "" {
			parsed, err := strconv.Atoi(t)
			if err != nil || parsed < 0 {
				writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid track"})
				return
			}
			n = parsed
		}
		cmd.Track = &n
		st.Track = n
		hub.SetState(st)
		hub.Broadcast(cmd)
		hub.BroadcastState()
		reply(w, map[string]any{"file": rel})
	}
	pauseHandler := func(w http.ResponseWriter, r *http.Request) {
		st := hub.Snapshot()
		st.Paused = true
		st.Playing = false
		st.Status = "Paused"
		hub.SetState(st)
		hub.Broadcast(control.Command{Type: "pause"})
		hub.BroadcastState()
		reply(w, nil)
	}
	resumeHandler := func(w http.ResponseWriter, r *http.Request) {
		st := hub.Snapshot()
		st.Paused = false
		st.Status = "Resuming…"
		hub.SetState(st)
		hub.Broadcast(control.Command{Type: "resume"})
		hub.BroadcastState()
		reply(w, nil)
	}
	trackHandler := func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("track")))
		if err != nil || n < 0 {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid track"})
			return
		}
		track := n
		st := hub.Snapshot()
		st.Track = n
		hub.SetState(st)
		hub.Broadcast(control.Command{Type: "track", Track: &track})
		hub.BroadcastState()
		reply(w, map[string]any{"track": n})
	}
	statePost := func(w http.ResponseWriter, r *http.Request) {
		var st control.State
		if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		hub.SetState(st)
		hub.BroadcastState()
		writeJSON(w, st)
	}

	startFile := func(rel string) {
		st := hub.Snapshot()
		st.File = rel
		st.Name = path.Base(rel)
		st.Paused = true
		st.Playing = false
		st.Status = "Starting…"
		track := 1
		st.Track = track
		hub.SetState(st)
		hub.Broadcast(control.Command{Type: "play", File: rel, Track: &track})
		hub.BroadcastState()
	}
	queueHandler := func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("file")
		if rel == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing file query parameter"})
			return
		}
		if _, err := resolveVideo(videoDir, rel); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if hub.Snapshot().File == "" {
			startFile(rel)
			reply(w, map[string]any{"file": rel, "started": true})
			return
		}
		item := hub.Enqueue(rel, path.Base(rel))
		hub.BroadcastState()
		reply(w, map[string]any{"file": rel, "id": item.ID, "started": false})
	}
	removeHandler := func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		if !hub.RemoveQueued(id) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not in queue"})
			return
		}
		hub.BroadcastState()
		reply(w, map[string]any{"id": id})
	}
	nextHandler := func(w http.ResponseWriter, r *http.Request) {
		item, ok := hub.PopNext()
		if !ok {
			st := hub.Snapshot()
			st.File = ""
			st.Name = ""
			st.Paused = true
			st.Playing = false
			st.Status = ""
			st.Tracks = nil
			st.Track = 0
			hub.SetState(st)
			hub.Broadcast(control.Command{Type: "idle"})
			hub.BroadcastState()
			reply(w, map[string]any{"idle": true})
			return
		}
		startFile(item.File)
		reply(w, map[string]any{"file": item.File})
	}
	mux.HandleFunc("GET /api/play", playHandler)
	mux.HandleFunc("POST /api/play", playHandler)
	mux.HandleFunc("GET /api/queue", queueHandler)
	mux.HandleFunc("POST /api/queue", queueHandler)
	topHandler := func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
			return
		}
		if !hub.MoveToFront(id) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "not in queue"})
			return
		}
		hub.BroadcastState()
		reply(w, map[string]any{"id": id})
	}
	mux.HandleFunc("POST /api/queue/remove", removeHandler)
	mux.HandleFunc("POST /api/queue/top", topHandler)
	mux.HandleFunc("GET /api/next", nextHandler)
	mux.HandleFunc("POST /api/next", nextHandler)
	mux.HandleFunc("GET /api/pause", pauseHandler)
	mux.HandleFunc("POST /api/pause", pauseHandler)
	mux.HandleFunc("GET /api/resume", resumeHandler)
	mux.HandleFunc("POST /api/resume", resumeHandler)
	mux.HandleFunc("GET /api/track", trackHandler)
	mux.HandleFunc("POST /api/track", trackHandler)
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, hub.Snapshot())
	})
	mux.HandleFunc("POST /api/state", statePost)
	mux.Handle("GET /hls/", http.StripPrefix("/hls/", serveHLS(cacheDir)))

	addr := ":8080"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + strings.TrimPrefix(v, ":")
	}
	log.Printf("video server listening on http://localhost%s", addr)
	log.Printf("library: %s", videoDir)
	log.Fatal(http.ListenAndServe(addr, withCORS(mux)))
}

func listVideos(videoDir string) ([]media.VideoInfo, error) {
	var out []media.VideoInfo
	err := filepath.WalkDir(videoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".mkv") {
			return nil
		}
		rel, err := filepath.Rel(videoDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := media.Probe(p, rel)
		if err != nil {
			log.Printf("skip %s: %v", rel, err)
			return nil
		}
		out = append(out, info)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if out == nil {
		out = []media.VideoInfo{}
	}
	return out, nil
}

func resolveVideo(videoDir, rel string) (string, error) {
	rel = path.Clean("/" + strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		return "", os.ErrInvalid
	}
	if !strings.EqualFold(filepath.Ext(rel), ".mkv") {
		return "", os.ErrInvalid
	}
	abs := filepath.Join(videoDir, filepath.FromSlash(rel))
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(videoDir)
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func cacheID(rel string) string {
	sum := sha1.Sum([]byte(rel))
	return hex.EncodeToString(sum[:])[:16]
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// seekOnlyFile is an io.ReadSeeker that is not an *os.File, so net/http copies
// it with Read/Write instead of sendfile. macOS sendfile can produce a response
// that remote browsers reject as ERR_INVALID_HTTP_RESPONSE.
type seekOnlyFile struct{ f *os.File }

func (s seekOnlyFile) Read(p []byte) (int, error) { return s.f.Read(p) }
func (s seekOnlyFile) Seek(offset int64, whence int) (int64, error) {
	return s.f.Seek(offset, whence)
}

func serveHLS(cacheDir string) http.Handler {
	root, err := filepath.Abs(cacheDir)
	if err != nil {
		root = cacheDir
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := path.Clean("/" + r.URL.Path)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || rel == "." || strings.Contains(rel, "..") {
			http.NotFound(w, r)
			return
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		abs, err := filepath.Abs(abs)
		if err != nil || (abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator))) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(abs)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		stat, err := f.Stat()
		if err != nil || stat.IsDir() {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(strings.ToLower(stat.Name()), ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), seekOnlyFile{f})
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}
