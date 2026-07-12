package ember

import (
	"errors"
	"testing"
)

func TestRuntimeOwnerStartsWithLazyMaps(t *testing.T) {
	owner := newRuntimeOwner()

	if owner.threads != nil {
		t.Fatal("new owner eagerly allocated threads map")
	}
	if owner.coroutines != nil {
		t.Fatal("new owner eagerly allocated coroutines map")
	}
	if owner.coroutineRefs != nil {
		t.Fatal("new owner eagerly allocated coroutine refs map")
	}
	if owner.roots != nil {
		t.Fatal("new owner eagerly allocated roots map")
	}
	if owner.pins != nil {
		t.Fatal("new owner eagerly allocated pins map")
	}
	if owner.pinStates != nil {
		t.Fatal("new owner eagerly allocated pin states map")
	}
}

func TestRuntimeOwnerLazyMapsInitializeOnFirstWrite(t *testing.T) {
	t.Run("thread", func(t *testing.T) {
		owner := newRuntimeOwner()
		thread := &vmThread{}
		if err := owner.registerThread(thread); err != nil {
			t.Fatalf("register thread: %v", err)
		}
		if owner.threads == nil || len(owner.threads) != 1 {
			t.Fatalf("threads map = %#v, want one registered thread", owner.threads)
		}
	})

	t.Run("coroutine", func(t *testing.T) {
		owner := newRuntimeOwner()
		coroutine := &vmCoroutine{}
		if err := owner.registerCoroutine(coroutine); err != nil {
			t.Fatalf("register coroutine: %v", err)
		}
		if owner.coroutines == nil || len(owner.coroutines) != 1 {
			t.Fatalf("coroutines map = %#v, want one registered coroutine", owner.coroutines)
		}
	})

	t.Run("coroutine refs", func(t *testing.T) {
		owner := newRuntimeOwner()
		coroutine := &vmCoroutine{}
		if err := owner.retainCoroutine(coroutine); err != nil {
			t.Fatalf("retain coroutine: %v", err)
		}
		if owner.coroutineRefs == nil || len(owner.coroutineRefs) != 1 {
			t.Fatalf("coroutine refs map = %#v, want one retained coroutine", owner.coroutineRefs)
		}
	})

	t.Run("root", func(t *testing.T) {
		owner := newRuntimeOwner()
		root, err := owner.root(slotNil)
		if err != nil {
			t.Fatalf("root nil slot: %v", err)
		}
		if owner.roots == nil || len(owner.roots) != 1 {
			t.Fatalf("roots map = %#v, want one root", owner.roots)
		}
		root.release()
	})

	t.Run("pin", func(t *testing.T) {
		owner := newRuntimeOwner()
		pin, err := owner.pin(TableValue(NewTable()))
		if err != nil {
			t.Fatalf("pin table: %v", err)
		}
		if owner.pins == nil || len(owner.pins) != 1 {
			t.Fatalf("pins map = %#v, want one pin", owner.pins)
		}
		if owner.pinStates == nil || len(owner.pinStates) != 1 {
			t.Fatalf("pin states map = %#v, want one pin state", owner.pinStates)
		}
		pin.release()
	})
}

func TestRuntimeOwnerNilMapsSupportReadsAndClose(t *testing.T) {
	owner := newRuntimeOwner()

	if got := owner.threadCount(); got != 0 {
		t.Fatalf("thread count = %d, want 0", got)
	}
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("coroutine count = %d, want 0", got)
	}
	owner.unregisterThread(&vmThread{})
	owner.unregisterCoroutine(&vmCoroutine{})
	owner.releaseCoroutine(&vmCoroutine{})
	if err := owner.close(); err != nil {
		t.Fatalf("close fresh owner: %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatalf("idempotent close fresh owner: %v", err)
	}
	if owner.threads != nil || owner.coroutines != nil || owner.coroutineRefs != nil || owner.roots != nil || owner.pins != nil || owner.pinStates != nil {
		t.Fatal("nil maps were initialized by read/close operations")
	}
}

func TestRuntimeOwnerLazyMapsPreserveCloseSemantics(t *testing.T) {
	owner := newRuntimeOwner()
	thread := &vmThread{}
	coroutine := &vmCoroutine{}
	if err := owner.registerThread(thread); err != nil {
		t.Fatalf("register thread: %v", err)
	}
	if err := owner.registerCoroutine(coroutine); err != nil {
		t.Fatalf("register coroutine: %v", err)
	}
	if err := owner.close(); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("close with active registrations = %v, want %v", err, errRuntimeOwnerActive)
	}
	owner.unregisterThread(thread)
	owner.unregisterCoroutine(coroutine)
	if err := owner.close(); err != nil {
		t.Fatalf("close after unregister: %v", err)
	}
}
