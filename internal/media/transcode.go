package media

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type JobStatus string

const (
	StatusStarting JobStatus = "starting"
	StatusRunning  JobStatus = "running"
	StatusReady    JobStatus = "ready"
	StatusDone     JobStatus = "done"
	StatusError    JobStatus = "error"
)

type Job struct {
	ID          string
	Src         string
	Dir         string
	Playlist    string
	Status      JobStatus
	Err         string
	cmd         *exec.Cmd
	readyOnce   chan struct{}
	readyClosed sync.Once
	done        chan struct{}
}

type Manager struct {
	cacheDir string
	mu       sync.Mutex
	jobs     map[string]*Job
}

func NewManager(cacheDir string) *Manager {
	return &Manager{
		cacheDir: cacheDir,
		jobs:     make(map[string]*Job),
	}
}

func (m *Manager) CacheDir() string {
	return m.cacheDir
}

// StartCacheCleanup deletes cache directories older than maxAge, then repeats
// on interval. A directory whose transcode is still running is left in place.
func (m *Manager) StartCacheCleanup(interval, maxAge time.Duration) {
	go func() {
		m.cleanupCache(maxAge)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			m.cleanupCache(maxAge)
		}
	}()
}

func (m *Manager) cleanupCache(maxAge time.Duration) {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		log.Printf("cache cleanup: %v", err)
		return
	}
	cutoff := time.Now().Add(-maxAge)
	var stale []string
	m.mu.Lock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || !createdAt(info).Before(cutoff) {
			continue
		}
		if job, ok := m.jobs[entry.Name()]; ok && jobInProgress(job) {
			continue
		}
		stale = append(stale, entry.Name())
	}
	m.mu.Unlock()

	for _, id := range stale {
		m.mu.Lock()
		if job, ok := m.jobs[id]; ok && jobInProgress(job) {
			m.mu.Unlock()
			continue
		}
		m.mu.Unlock()

		dir := filepath.Join(m.cacheDir, id)
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("cache cleanup %s: %v", id, err)
			continue
		}
		m.mu.Lock()
		if job, ok := m.jobs[id]; ok && !jobInProgress(job) {
			delete(m.jobs, id)
		}
		m.mu.Unlock()
		log.Printf("removed cache %s (older than %s)", id, maxAge)
	}
}

// ClearExcept deletes every cache directory except keepID. An empty keepID
// removes all of them. A transcode for a removed directory is stopped first.
func (m *Manager) ClearExcept(keepID string) (int, error) {
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var remove []string
	m.mu.Lock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if keepID != "" && id == keepID {
			continue
		}
		if job, ok := m.jobs[id]; ok {
			stopJob(job)
		}
		remove = append(remove, id)
	}
	m.mu.Unlock()

	removed := 0
	for _, id := range remove {
		if err := os.RemoveAll(filepath.Join(m.cacheDir, id)); err != nil {
			log.Printf("cache clear %s: %v", id, err)
			continue
		}
		m.mu.Lock()
		delete(m.jobs, id)
		m.mu.Unlock()
		removed++
		log.Printf("removed cache %s", id)
	}
	return removed, nil
}

func stopJob(job *Job) {
	if job == nil || job.cmd == nil || job.cmd.Process == nil {
		return
	}
	_ = job.cmd.Process.Kill()
}

func jobInProgress(job *Job) bool {
	switch job.Status {
	case StatusStarting, StatusRunning, StatusReady:
		return true
	default:
		return false
	}
}

func (m *Manager) Prepare(id, src string, audioCount int) (*Job, error) {
	m.mu.Lock()
	if job, ok := m.jobs[id]; ok {
		status := job.Status
		m.mu.Unlock()
		if status == StatusError {
			m.mu.Lock()
			delete(m.jobs, id)
			m.mu.Unlock()
		} else {
			return job, nil
		}
	} else {
		m.mu.Unlock()
	}

	dir := filepath.Join(m.cacheDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	job := &Job{
		ID:        id,
		Src:       src,
		Dir:       dir,
		Playlist:  "/hls/" + id + "/master.m3u8",
		Status:    StatusStarting,
		readyOnce: make(chan struct{}),
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	if existing, ok := m.jobs[id]; ok && existing.Status != StatusError {
		m.mu.Unlock()
		return existing, nil
	}
	m.jobs[id] = job
	m.mu.Unlock()

	if transcodeComplete(dir) {
		job.Status = StatusDone
		job.signalReady()
		close(job.done)
		return job, nil
	}

	// Incomplete leftover from a previous run: start a fresh transcode.
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	if err := startFFmpeg(job, audioCount); err != nil {
		job.Status = StatusError
		job.Err = err.Error()
		return job, err
	}
	return job, nil
}

func (j *Job) WaitReady(timeout time.Duration) error {
	select {
	case <-j.readyOnce:
		if j.Status == StatusError {
			return fmt.Errorf("%s", j.Err)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for transcode to start")
	}
}

func startFFmpeg(job *Job, audioCount int) error {
	if audioCount < 1 {
		audioCount = 1
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-i", job.Src,
		"-map", "0:v:0",
	}
	for i := 0; i < audioCount; i++ {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", i))
	}
	args = append(args,
		// Some files store a nonsense sample aspect ratio (for example 1:9).
		// Honor a normal ratio by baking it into square pixels, and keep the
		// coded frame when the ratio is not a real pixel shape.
		"-vf", "scale=w='if(between(sar,0.5,2),trunc(iw*sar/2)*2,iw)':h=ih:flags=lanczos,setsar=1",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "21",
		"-pix_fmt", "yuv420p",
		"-g", "48",
		"-keyint_min", "48",
		"-sc_threshold", "0",
		"-c:a", "aac",
		"-b:a", "160k",
		"-ac", "2",
		"-var_stream_map", varStreamMap(audioCount),
		"-master_pl_name", "master.m3u8",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(job.Dir, "s%v_%05d.ts"),
		filepath.Join(job.Dir, "s%v.m3u8"),
	)

	cmd := exec.Command("ffmpeg", args...)
	logFile, err := os.Create(filepath.Join(job.Dir, "ffmpeg.log"))
	if err != nil {
		return err
	}
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return err
	}
	job.cmd = cmd
	job.Status = StatusRunning
	log.Printf("ffmpeg started for %s (pid %d)", job.ID, cmd.Process.Pid)

	go watchReady(job)
	go func() {
		err := cmd.Wait()
		logFile.Close()
		if err != nil && job.Status != StatusDone && job.Status != StatusReady {
			job.Status = StatusError
			job.Err = err.Error()
			log.Printf("ffmpeg failed for %s: %v", job.ID, err)
		} else if job.Status != StatusError {
			job.Status = StatusDone
		}
		job.signalReady()
		close(job.done)
	}()

	return nil
}

func (j *Job) signalReady() {
	j.readyClosed.Do(func() {
		close(j.readyOnce)
	})
}

func varStreamMap(audioCount int) string {
	// One video rendition plus alternate audio renditions in the same group.
	mapStr := "v:0,agroup:audio,default:yes"
	for i := 0; i < audioCount; i++ {
		def := ""
		if i == 0 {
			def = ",default:yes"
		}
		mapStr += fmt.Sprintf(" a:%d,agroup:audio%s,name:track%d", i, def, i+1)
	}
	return mapStr
}

func watchReady(job *Job) {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if masterReady(job.Dir) {
			if job.Status == StatusRunning {
				job.Status = StatusReady
			}
			job.signalReady()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func masterReady(dir string) bool {
	master := filepath.Join(dir, "master.m3u8")
	st, err := os.Stat(master)
	if err != nil || st.Size() == 0 {
		return false
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "s*_*.ts"))
	return len(matches) > 0
}

func transcodeComplete(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "s0.m3u8"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "EXT-X-ENDLIST")
}
