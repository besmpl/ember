package ember

import (
	"errors"
	"testing"
)

func TestVMProtectedRecoveryUsesNearestBoundaryMarker(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	parent := newVMFrame(proto, nil, nil)
	child := newVMFrame(proto, nil, nil)
	thread.pushFrame(parent)
	thread.installPendingCall(parent, vmPendingCall{
		destination: vmResultDestination{register: 0, count: 1},
		protected:   &vmProtectedCall{},
	})
	thread.pushFrame(child)
	thread.installPendingCall(child, vmPendingCall{
		destination: vmResultDestination{register: 0, count: 1},
		protected:   &vmProtectedCall{},
	})

	if got, want := thread.nearestProtectedFrame, child.depth; got != want {
		t.Fatalf("nearest protected frame is %d, want child depth %d", got, want)
	}
	if !thread.recoverProtectedError(errors.New("child failure")) {
		t.Fatal("recoverProtectedError did not recover the nearest protected call")
	}
	if got, want := thread.nearestProtectedFrame, parent.depth; got != want {
		t.Fatalf("nearest protected frame after child recovery is %d, want parent depth %d", got, want)
	}
	if child.hasPendingCall {
		t.Fatal("child retained a pending protected call after recovery")
	}
	if got, want := thread.protectedRecoveryLookups, uint64(1); got != want {
		t.Fatalf("protected recovery lookups = %d, want %d", got, want)
	}
	if got := thread.protectedRecoveryScans; got != 0 {
		t.Fatalf("protected recovery performed %d frame scans, want 0", got)
	}

	if !thread.recoverProtectedError(errors.New("parent failure")) {
		t.Fatal("recoverProtectedError did not recover the prior protected call")
	}
	if got := thread.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("nearest protected frame after parent recovery is %d, want %d", got, noProtectedFrame)
	}
	if got, want := thread.protectedRecoveryLookups, uint64(2); got != want {
		t.Fatalf("protected recovery lookups after nested recovery = %d, want %d", got, want)
	}
}

func TestVMProtectedBoundaryMarkerKeepsSuspendedChildNearest(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	parent := newVMFrame(proto, nil, nil)
	child := newVMFrame(proto, nil, nil)
	thread.pushFrame(parent)
	thread.pushFrame(child)
	childProtected := &vmProtectedCall{}
	parentProtected := &vmProtectedCall{}
	thread.installPendingCall(child, vmPendingCall{protected: childProtected})
	thread.installPendingCall(parent, vmPendingCall{protected: parentProtected})

	if got, want := thread.nearestProtectedFrame, child.depth; got != want {
		t.Fatalf("nearest protected frame after propagated parent install is %d, want child depth %d", got, want)
	}
	if got := parent.pendingCall.protectedBoundary; !got {
		t.Fatalf("parent protected pending call boundary bit is false, want true")
	}
	if !thread.recoverProtectedError(errors.New("child failure")) {
		t.Fatal("recoverProtectedError did not recover the child boundary")
	}
	if got, want := thread.nearestProtectedFrame, parent.depth; got != want {
		t.Fatalf("nearest protected frame after child recovery is %d, want parent depth %d", got, want)
	}
	if !thread.recoverProtectedError(errors.New("parent failure")) {
		t.Fatal("recoverProtectedError did not recover the parent boundary")
	}
	if got := thread.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("nearest protected frame after parent recovery is %d, want %d", got, noProtectedFrame)
	}
}

func TestVMProtectedBoundaryMarkerSkipsPropagatedDuplicate(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	parent := newVMFrame(proto, nil, nil)
	child := newVMFrame(proto, nil, nil)
	thread.pushFrame(parent)
	thread.pushFrame(child)
	protected := &vmProtectedCall{}
	thread.installPendingCall(child, vmPendingCall{protected: protected})
	thread.installPendingCall(parent, vmPendingCall{protected: protected})

	if got, want := thread.nearestProtectedFrame, child.depth; got != want {
		t.Fatalf("nearest protected frame after duplicate propagation is %d, want child depth %d", got, want)
	}
	if parent.pendingCall.protectedBoundary {
		t.Fatal("propagated duplicate unexpectedly became a second recovery boundary")
	}
	thread.clearPendingCall(parent)
	thread.clearPendingCall(child)
	if got := thread.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("nearest protected frame after duplicate cleanup is %d, want %d", got, noProtectedFrame)
	}
	if got := thread.protectedRecoveryErrors; got != 0 {
		t.Fatalf("protected recovery invariant errors after duplicate cleanup = %d, want 0", got)
	}
}

func TestVMProtectedBoundaryMarkerSurvivesCoroutineFrameSuspension(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := newVMFrame(proto, nil, nil)
	thread.pushFrame(frame)
	thread.installPendingCall(frame, vmPendingCall{
		destination: vmResultDestination{register: 0, count: 1},
		protected:   &vmProtectedCall{},
	})

	suspended := thread.suspendFrames()
	if got := thread.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("suspended thread retained nearest protected frame %d, want %d", got, noProtectedFrame)
	}
	if got, want := suspended.nearestProtectedFrame, frame.depth; got != want {
		t.Fatalf("suspended nearest protected frame is %d, want %d", got, want)
	}

	resumed := newVMThread(runtimeGlobals(nil))
	resumed.resumeFrames(suspended)
	if got, want := resumed.nearestProtectedFrame, frame.depth; got != want {
		t.Fatalf("resumed nearest protected frame is %d, want %d", got, want)
	}
	resumed.clearPendingCall(frame)
	if got := resumed.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("cleared resumed protected frame marker is %d, want %d", got, noProtectedFrame)
	}
}

func TestVMProtectedBoundaryMarkerClearsOnPoolReset(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturn, a: 0, b: 1}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := newVMFrame(proto, nil, nil)
	thread.pushFrame(frame)
	thread.installPendingCall(frame, vmPendingCall{protected: &vmProtectedCall{}})
	thread.resetForPool()

	if got := thread.nearestProtectedFrame; got != noProtectedFrame {
		t.Fatalf("pooled thread retained nearest protected frame %d, want %d", got, noProtectedFrame)
	}
	if got := frame.hasPendingCall; got {
		t.Fatal("pooled frame retained protected pending-call state")
	}
}
