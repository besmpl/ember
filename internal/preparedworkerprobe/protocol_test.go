package preparedworkerprobe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
)

func TestClientTransactUsesOneRequestAndOneResponseFrame(t *testing.T) {
	request := preparedworkerfixture.TurnRequest{
		Sequence:   7,
		Revision:   6,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 9},
		Events: []preparedworkerfixture.DamageEvent{{
			Route: preparedworkerfixture.RouteDamage, Entity: 4, Amount: 5,
		}},
	}
	want := preparedworkerfixture.TurnResult{
		Sequence: 7,
		Revision: 7,
		State:    preparedworkerfixture.StateSnapshot{Tick: 9, Total: 11},
		Commands: []preparedworkerfixture.Command{{Kind: preparedworkerfixture.CommandDraw, A: 9, B: 11}},
	}
	response, err := encodeTurnResponse(want, nil)
	if err != nil {
		t.Fatal(err)
	}
	var scripted bytes.Buffer
	scripted.WriteString(protocolMagic)
	responseWriter := bufio.NewWriter(&scripted)
	if err := writeProtocolFrame(responseWriter, response); err != nil {
		t.Fatal(err)
	}
	var requests bytes.Buffer
	client, err := NewClient(
		context.Background(),
		io.NopCloser(&scripted),
		protocolNopWriteCloser{Writer: &requests},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Transact(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn result = %#v, want %#v", got, want)
	}
	payload, err := readProtocolFrame(&requests)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeTurnRequest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, request) {
		t.Fatalf("request = %#v, want %#v", decoded, request)
	}
	if _, err := readProtocolFrame(&requests); !errors.Is(err, io.EOF) {
		t.Fatalf("second request frame error = %v, want EOF", err)
	}
}

func TestTurnProtocolRejectsUnboundedApplicationRecords(t *testing.T) {
	request := preparedworkerfixture.TurnRequest{
		Events: make([]preparedworkerfixture.DamageEvent, maxTurnItems+1),
	}
	if _, err := encodeTurnRequest(request); err == nil {
		t.Fatal("encoded an unbounded request")
	}
	result := preparedworkerfixture.TurnResult{
		Commands: make([]preparedworkerfixture.Command, maxTurnItems+1),
	}
	if _, err := encodeTurnResponse(result, nil); err == nil {
		t.Fatal("encoded an unbounded response")
	}
	request = preparedworkerfixture.TurnRequest{
		Projection: preparedworkerfixture.Projection{Work: 1},
		Events:     []preparedworkerfixture.DamageEvent{{Route: 99}},
	}
	if _, err := encodeTurnRequest(request); err == nil {
		t.Fatal("encoded an unknown event route")
	}
	result = preparedworkerfixture.TurnResult{
		Commands: []preparedworkerfixture.Command{{Kind: 99}},
	}
	if _, err := encodeTurnResponse(result, nil); err == nil {
		t.Fatal("encoded an unknown command kind")
	}
}

func TestClientRejectsResponseForAnotherTransaction(t *testing.T) {
	request := preparedworkerfixture.TurnRequest{
		Sequence:   7,
		Revision:   6,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
	}
	response, err := encodeTurnResponse(preparedworkerfixture.TurnResult{
		Sequence: 8,
		Revision: 7,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var scripted bytes.Buffer
	scripted.WriteString(protocolMagic)
	if err := writeProtocolFrame(bufio.NewWriter(&scripted), response); err != nil {
		t.Fatal(err)
	}
	aborted := false
	client, err := NewClient(
		context.Background(),
		io.NopCloser(&scripted),
		protocolNopWriteCloser{Writer: io.Discard},
		func() error {
			aborted = true
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Transact(context.Background(), request); err == nil || !strings.Contains(err.Error(), "does not advance") {
		t.Fatalf("position mismatch error = %v", err)
	}
	if !aborted || !client.WasAborted() {
		t.Fatal("position mismatch did not abort its uncertain generation")
	}
	if _, err := client.Transact(context.Background(), request); err == nil || !strings.Contains(err.Error(), "closed client") {
		t.Fatalf("reuse after position mismatch error = %v, want closed client", err)
	}
}

func TestClientReadFailureMakesGenerationTerminal(t *testing.T) {
	request := preparedworkerfixture.TurnRequest{
		Sequence:   7,
		Revision:   6,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
	}
	reader := io.NopCloser(strings.NewReader(protocolMagic))
	aborted := false
	client, err := NewClient(
		context.Background(),
		reader,
		protocolNopWriteCloser{Writer: io.Discard},
		func() error {
			aborted = true
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Transact(context.Background(), request); err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("read failure error = %v", err)
	}
	if !aborted || !client.WasAborted() {
		t.Fatal("read failure did not abort its uncertain generation")
	}
	if _, err := client.Transact(context.Background(), request); err == nil || !strings.Contains(err.Error(), "closed client") {
		t.Fatalf("reuse after read failure error = %v, want closed client", err)
	}
}

func TestNewClientDeadlineAbortsBlockedReady(t *testing.T) {
	reader := newProtocolBlockingReadCloser()
	aborted := false
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := NewClient(
		ctx,
		reader,
		protocolNopWriteCloser{Writer: io.Discard},
		func() error {
			aborted = true
			return nil
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("READY error = %v, want deadline exceeded", err)
	}
	if !aborted {
		t.Fatal("blocked READY did not abort its generation")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocked READY took %s to abort", elapsed)
	}
}

type protocolNopWriteCloser struct {
	io.Writer
}

func (protocolNopWriteCloser) Close() error { return nil }

type protocolBlockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func newProtocolBlockingReadCloser() *protocolBlockingReadCloser {
	return &protocolBlockingReadCloser{closed: make(chan struct{})}
}

func (reader *protocolBlockingReadCloser) Read([]byte) (int, error) {
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *protocolBlockingReadCloser) Close() error {
	reader.once.Do(func() { close(reader.closed) })
	return nil
}
