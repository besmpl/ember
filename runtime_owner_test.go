package ember

import (
	"errors"
	"runtime"
	"sync"
	"testing"
)

func TestRuntimeOwnerRunLeaseLifecycle(t *testing.T) {
	owner := newRuntimeOwner()
	lease, err := owner.beginRun()
	if err != nil {
		t.Fatalf("begin run: %v", err)
	}
	if err := owner.close(); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("close with active run = %v, want %v", err, errRuntimeOwnerActive)
	}
	lease.end()
	lease.end()
	if err := owner.close(); err != nil {
		t.Fatalf("close after run ended: %v", err)
	}
	if err := owner.close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, err := owner.beginRun(); !errors.Is(err, errRuntimeOwnerClosed) {
		t.Fatalf("begin after close = %v, want %v", err, errRuntimeOwnerClosed)
	}
}

func TestRuntimeOwnerRootAndPinTokens(t *testing.T) {
	owner := newRuntimeOwner()

	table := NewTable()
	tableSlot, err := owner.heap.importValue(TableValue(table))
	if err != nil {
		t.Fatalf("import table: %v", err)
	}
	root, err := owner.root(tableSlot)
	if err != nil {
		t.Fatalf("root table: %v", err)
	}
	if got, err := root.value(); err != nil || got != tableSlot {
		t.Fatalf("root value = %v, %v; want %v, nil", got, err, tableSlot)
	}
	root.release()
	root.release()
	if _, err := root.value(); !errors.Is(err, errRuntimeOwnerReleased) {
		t.Fatalf("released root value = %v, want %v", err, errRuntimeOwnerReleased)
	}
	if _, err := owner.heap.exportValue(tableSlot); err != nil {
		t.Fatalf("released root should not free heap handle: %v", err)
	}

	userdata := NewUserData("opaque")
	pin, err := owner.pin(UserDataValue(userdata))
	if err != nil {
		t.Fatalf("pin userdata: %v", err)
	}
	if !owner.heap.userdata.pinnedFor(pin.handle) {
		t.Fatal("opaque userdata pin did not set slab pin metadata")
	}
	if err := owner.close(); err != nil {
		t.Fatalf("close with pin: %v", err)
	}
	value, err := pin.value()
	if err != nil {
		t.Fatalf("export outstanding pin after close: %v", err)
	}
	if got, ok := value.UserData(); !ok || got != userdata {
		t.Fatalf("pinned userdata = %#v, want original userdata", value)
	}
	pin.release()
	pin.release()
	if _, err := pin.value(); !errors.Is(err, errRuntimeOwnerReleased) {
		t.Fatalf("released pin value = %v, want %v", err, errRuntimeOwnerReleased)
	}
	if _, err := owner.heap.exportValue(pin.handle); err != nil {
		t.Fatalf("released pin should not free heap handle: %v", err)
	}
	if !owner.heap.userdata.pinnedFor(pin.handle) {
		t.Fatal("released userdata pin removed the default opaque pin")
	}
}

func TestRuntimeOwnerPinReleaseUnpinsOnlyAfterLastToken(t *testing.T) {
	owner := newRuntimeOwner()
	userdata := NewUserData("shared pin")
	first, err := owner.pin(UserDataValue(userdata))
	if err != nil {
		t.Fatalf("first pin: %v", err)
	}
	second, err := owner.pin(UserDataValue(userdata))
	if err != nil {
		t.Fatalf("second pin: %v", err)
	}
	if first.handle != second.handle {
		t.Fatal("same userdata pointer produced different pin handles")
	}
	if !owner.heap.userdata.pinnedFor(first.handle) {
		t.Fatal("shared userdata pin is not marked")
	}

	first.release()
	if !owner.heap.userdata.pinnedFor(second.handle) {
		t.Fatal("first release cleared the last shared pin")
	}
	if value, err := second.value(); err != nil {
		t.Fatalf("second pin after first release: %v", err)
	} else if got, ok := value.UserData(); !ok || got != userdata {
		t.Fatalf("second pin value = (%p, %v), want (%p, true)", got, ok, userdata)
	}

	second.release()
	if !owner.heap.userdata.pinnedFor(second.handle) {
		t.Fatal("last shared pin removed the default opaque pin")
	}
}

func TestRuntimeOwnerPinAddsTransientPinForTable(t *testing.T) {
	owner := newRuntimeOwner()
	table := NewTable()
	handle, err := owner.heap.importValue(TableValue(table))
	if err != nil {
		t.Fatalf("import table: %v", err)
	}
	if owner.heap.tables.pinnedFor(handle) {
		t.Fatal("table starts pinned before an owner pin")
	}
	pin, err := owner.pin(TableValue(table))
	if err != nil {
		t.Fatalf("pin table: %v", err)
	}
	if !owner.heap.tables.pinnedFor(handle) {
		t.Fatal("table owner pin did not set slab metadata")
	}
	pin.release()
	if owner.heap.tables.pinnedFor(handle) {
		t.Fatal("table last pin did not clear transient slab metadata")
	}
}

func TestRuntimeOwnerRootValidatesInternalHandles(t *testing.T) {
	owner := newRuntimeOwner()
	cell := &cell{}
	handle, err := owner.heap.importCell(cell)
	if err != nil {
		t.Fatalf("import upvalue: %v", err)
	}
	root, err := owner.root(handle)
	if err != nil {
		t.Fatalf("root upvalue: %v", err)
	}
	if got, err := root.value(); err != nil || got != handle {
		t.Fatalf("root upvalue value = %#x, %v; want %#x, nil", got, err, handle)
	}
	if err := owner.heap.releaseHandle(handle); err != nil {
		t.Fatalf("release upvalue: %v", err)
	}
	if _, err := root.value(); err == nil {
		t.Fatal("root returned a stale released handle")
	}
}

func TestRuntimeOwnerRegistrationLifecycle(t *testing.T) {
	owner := newRuntimeOwner()
	thread := &vmThread{}
	coroutine := &vmCoroutine{}
	if err := owner.registerThread(thread); err != nil {
		t.Fatalf("register thread: %v", err)
	}
	if err := owner.registerThread(thread); err != nil {
		t.Fatalf("idempotent thread registration: %v", err)
	}
	if err := owner.registerCoroutine(coroutine); err != nil {
		t.Fatalf("register coroutine: %v", err)
	}
	if got := owner.threadCount(); got != 1 {
		t.Fatalf("thread count = %d, want 1", got)
	}
	if got := owner.coroutineCount(); got != 1 {
		t.Fatalf("coroutine count = %d, want 1", got)
	}
	if err := owner.close(); !errors.Is(err, errRuntimeOwnerActive) {
		t.Fatalf("close with active registrations = %v, want %v", err, errRuntimeOwnerActive)
	}
	owner.unregisterThread(thread)
	owner.unregisterThread(thread)
	owner.unregisterCoroutine(coroutine)
	owner.unregisterCoroutine(coroutine)
	if got := owner.threadCount(); got != 0 {
		t.Fatalf("thread count after unregister = %d, want 0", got)
	}
	if got := owner.coroutineCount(); got != 0 {
		t.Fatalf("coroutine count after unregister = %d, want 0", got)
	}
	if err := owner.close(); err != nil {
		t.Fatalf("close after unregister: %v", err)
	}
	if err := owner.registerThread(thread); !errors.Is(err, errRuntimeOwnerClosed) {
		t.Fatalf("register thread after close = %v, want %v", err, errRuntimeOwnerClosed)
	}
	if err := owner.registerCoroutine(coroutine); !errors.Is(err, errRuntimeOwnerClosed) {
		t.Fatalf("register coroutine after close = %v, want %v", err, errRuntimeOwnerClosed)
	}
}

func TestRuntimeOwnerConcurrentLifecycle(t *testing.T) {
	owner := newRuntimeOwner()
	const workers = 8
	const iterations = 400
	var ready sync.WaitGroup
	ready.Add(workers)
	var workersDone sync.WaitGroup
	workersDone.Add(workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer workersDone.Done()
			ready.Done()
			for j := 0; j < iterations; j++ {
				lease, err := owner.beginRun()
				if err != nil {
					if errors.Is(err, errRuntimeOwnerBusy) {
						continue
					}
					if !errors.Is(err, errRuntimeOwnerClosed) {
						errs <- err
					}
					return
				}
				if j%17 == 0 {
					runtime.Gosched()
				}
				lease.end()
			}
		}()
	}
	ready.Wait()
	closeResult := make(chan error, 1)
	go func() { closeResult <- owner.close() }()
	workersDone.Wait()
	closeErr := <-closeResult
	if closeErr != nil && !errors.Is(closeErr, errRuntimeOwnerActive) {
		t.Fatalf("concurrent close = %v, want nil or active", closeErr)
	}
	for {
		select {
		case err := <-errs:
			t.Fatalf("unexpected concurrent lifecycle error: %v", err)
		default:
			goto checked
		}
	}
checked:
	if err := owner.close(); err != nil {
		t.Fatalf("final close: %v", err)
	}
}
