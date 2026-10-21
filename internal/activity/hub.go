// Package activity is the in-process wake-up channel behind the
// wait_for_activity long-poll: write paths signal the actors they
// affected, waiters block until signalled or timed out.
//
// The hub is deliberately only an accelerator. Waiters also re-check the
// database on a short interval, so a signal lost to a restart or raised on
// another replica only delays delivery by one poll interval instead of
// losing it. That keeps the hub correct on SQLite and multi-replica
// Postgres alike without a LISTEN/NOTIFY dependency; swapping the poll for
// LISTEN/NOTIFY later changes this package only.
package activity

import "sync"

// Hub fans one Notify out to every current waiter of that actor.
type Hub struct {
	mu      sync.Mutex
	waiters map[string][]chan struct{}
}

func NewHub() *Hub {
	return &Hub{waiters: map[string][]chan struct{}{}}
}

// Notify wakes every goroutine currently blocked in Wait for any of
// actorIDs. It never blocks: each waiter channel has capacity 1 and is
// removed after its first signal.
func (h *Hub) Notify(actorIDs ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range actorIDs {
		for _, ch := range h.waiters[id] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		delete(h.waiters, id)
	}
}

// Register returns a channel that receives one signal on the next Notify
// for actorID, plus a cancel func the caller must run when done waiting.
func (h *Hub) Register(actorID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.waiters[actorID] = append(h.waiters[actorID], ch)
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		ws := h.waiters[actorID]
		for i, w := range ws {
			if w == ch {
				h.waiters[actorID] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
		if len(h.waiters[actorID]) == 0 {
			delete(h.waiters, actorID)
		}
	}
	return ch, cancel
}
