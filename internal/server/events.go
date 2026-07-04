package server

import (
	"encoding/json"
	"sync"
)

type EventHub struct {
	mu          sync.Mutex
	subscribers map[chan []byte]struct{}
	last        []byte
}

func NewEventHub() *EventHub {
	return &EventHub{subscribers: map[chan []byte]struct{}{}}
}

func (h *EventHub) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	if len(h.last) > 0 {
		ch <- h.last
	}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		close(ch)
		h.mu.Unlock()
	}
}

func (h *EventHub) Publish(event ScanEvent) {
	h.PublishAny(event)
}

func (h *EventHub) PublishUpdate(event UpdateEvent) {
	h.PublishAny(event)
}

func (h *EventHub) PublishAny(event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.last = payload
	for ch := range h.subscribers {
		select {
		case ch <- payload:
		default:
		}
	}
	h.mu.Unlock()
}
