package api

import (
	"sync"

	"safeops-agent/internal/agent"
)

type eventHub struct {
	mu          sync.Mutex
	history     map[string][]agent.RuntimeEvent
	next        map[string]uint64
	subscribers map[string]map[chan agent.RuntimeEvent]struct{}
}

type replayWindow struct {
	Events []agent.RuntimeEvent
	Oldest uint64
	Latest uint64
	Gap    bool
}

func newEventHub() *eventHub {
	return &eventHub{history: map[string][]agent.RuntimeEvent{}, next: map[string]uint64{}, subscribers: map[string]map[chan agent.RuntimeEvent]struct{}{}}
}

func (h *eventHub) publish(event agent.RuntimeEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next[event.TaskID]++
	event.Sequence = h.next[event.TaskID]
	history := append(h.history[event.TaskID], event)
	if len(history) > 200 {
		history = history[len(history)-200:]
	}
	h.history[event.TaskID] = history
	for ch := range h.subscribers[event.TaskID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *eventHub) subscribe(taskID string, after uint64) (replayWindow, <-chan agent.RuntimeEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	retained := h.history[taskID]
	window := replayWindow{Latest: h.next[taskID]}
	if len(retained) > 0 {
		window.Oldest = retained[0].Sequence
	}
	if after > 0 && (after > window.Latest || len(retained) == 0 && window.Latest == 0 || len(retained) > 0 && after+1 < window.Oldest) {
		window.Gap = true
	}
	for _, event := range retained {
		if event.Sequence > after {
			window.Events = append(window.Events, event)
		}
	}
	ch := make(chan agent.RuntimeEvent, 32)
	if h.subscribers[taskID] == nil {
		h.subscribers[taskID] = map[chan agent.RuntimeEvent]struct{}{}
	}
	h.subscribers[taskID][ch] = struct{}{}
	return window, ch, func() { h.mu.Lock(); defer h.mu.Unlock(); delete(h.subscribers[taskID], ch); close(ch) }
}
