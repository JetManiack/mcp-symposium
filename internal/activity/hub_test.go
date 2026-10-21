package activity

import (
	"testing"
	"time"
)

func TestNotifyWakesRegisteredWaiter(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Register("a")
	defer cancel()

	h.Notify("a")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("waiter was not woken")
	}
}

func TestNotifyOtherActorDoesNotWake(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Register("a")
	defer cancel()

	h.Notify("b")
	select {
	case <-ch:
		t.Fatal("waiter for a woken by notify for b")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRegistrationIsOneShot(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Register("a")
	defer cancel()

	h.Notify("a")
	<-ch
	h.Notify("a")
	select {
	case <-ch:
		t.Fatal("one-shot registration fired twice")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCancelRemovesWaiter(t *testing.T) {
	h := NewHub()
	ch, cancel := h.Register("a")
	cancel()

	h.Notify("a")
	select {
	case <-ch:
		t.Fatal("cancelled waiter still woken")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNotifyFansOutToAllWaiters(t *testing.T) {
	h := NewHub()
	ch1, c1 := h.Register("a")
	defer c1()
	ch2, c2 := h.Register("a")
	defer c2()

	h.Notify("a")
	for i, ch := range []<-chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d was not woken", i+1)
		}
	}
}
