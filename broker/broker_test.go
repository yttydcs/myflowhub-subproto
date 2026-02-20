package broker

import "testing"

func TestBroker_Deliver(t *testing.T) {
	b := New[int]()
	ch, cancel := b.Register("req1")
	defer cancel()

	if ok := b.Deliver("req1", 42); !ok {
		t.Fatalf("expected deliver ok")
	}
	v, ok := <-ch
	if !ok || v != 42 {
		t.Fatalf("unexpected resp: ok=%v v=%v", ok, v)
	}
}
