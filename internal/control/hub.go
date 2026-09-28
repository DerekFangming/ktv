package control

import (
	"strconv"
	"sync"
)

type QueueItem struct {
	ID   string `json:"id"`
	File string `json:"file"`
	Name string `json:"name"`
}

type Command struct {
	Type    string      `json:"type"`
	File    string      `json:"file,omitempty"`
	Name    string      `json:"name,omitempty"`
	Track   *int        `json:"track,omitempty"`
	Delta   int         `json:"delta,omitempty"`
	Paused  bool        `json:"paused"`
	Playing bool        `json:"playing"`
	Status  string      `json:"status,omitempty"`
	Tracks  []string    `json:"tracks,omitempty"`
	Queue   []QueueItem `json:"queue"`
}

type State struct {
	File    string      `json:"file,omitempty"`
	Name    string      `json:"name,omitempty"`
	Track   int         `json:"track"`
	Paused  bool        `json:"paused"`
	Playing bool        `json:"playing"`
	Status  string      `json:"status,omitempty"`
	Tracks  []string    `json:"tracks,omitempty"`
	Queue   []QueueItem `json:"queue"`
}

type Hub struct {
	mu      sync.Mutex
	clients map[chan Command]struct{}
	state   State
	seq     int64
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan Command]struct{})}
}

func (h *Hub) Snapshot() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.state
	s.Queue = append([]QueueItem(nil), h.state.Queue...)
	if s.Queue == nil {
		s.Queue = []QueueItem{}
	}
	return s
}

func (h *Hub) SetState(s State) {
	h.mu.Lock()
	s.Queue = h.state.Queue
	h.state = s
	h.mu.Unlock()
}

func (h *Hub) Enqueue(file, name string) QueueItem {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	item := QueueItem{ID: strconv.FormatInt(h.seq, 10), File: file, Name: name}
	h.state.Queue = append(h.state.Queue, item)
	return item
}

func (h *Hub) RemoveQueued(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, item := range h.state.Queue {
		if item.ID != id {
			continue
		}
		h.state.Queue = append(h.state.Queue[:i], h.state.Queue[i+1:]...)
		return true
	}
	return false
}

func (h *Hub) MoveToFront(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, item := range h.state.Queue {
		if item.ID != id {
			continue
		}
		if i == 0 {
			return true
		}
		rest := append([]QueueItem{}, h.state.Queue[i+1:]...)
		head := append([]QueueItem{}, h.state.Queue[:i]...)
		h.state.Queue = append(append([]QueueItem{item}, head...), rest...)
		return true
	}
	return false
}

func (h *Hub) PopNext() (QueueItem, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.state.Queue) == 0 {
		return QueueItem{}, false
	}
	item := h.state.Queue[0]
	h.state.Queue = append([]QueueItem(nil), h.state.Queue[1:]...)
	return item, true
}

func (h *Hub) StateCommand() Command {
	s := h.Snapshot()
	track := s.Track
	return Command{
		Type:    "state",
		File:    s.File,
		Name:    s.Name,
		Track:   &track,
		Paused:  s.Paused,
		Playing: s.Playing,
		Status:  s.Status,
		Tracks:  s.Tracks,
		Queue:   s.Queue,
	}
}

func (h *Hub) BroadcastState() int {
	return h.Broadcast(h.StateCommand())
}

func (h *Hub) Subscribe() chan Command {
	ch := make(chan Command, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan Command) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) Broadcast(cmd Command) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for ch := range h.clients {
		select {
		case ch <- cmd:
			n++
		default:
			// Slow client; drop this event rather than blocking the API.
		}
	}
	return n
}
