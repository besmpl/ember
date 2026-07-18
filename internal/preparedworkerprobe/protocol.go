package preparedworkerprobe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
)

const (
	protocolMagic    = "EPW1"
	protocolVersion  = 1
	maxProtocolFrame = 64 << 10
	maxProtocolError = 4 << 10
	responseOK       = 0
	responseError    = 1
)

type TurnTransactor interface {
	Transact(context.Context, preparedworkerfixture.TurnRequest) (preparedworkerfixture.TurnResult, error)
}

type Client struct {
	mu          sync.Mutex
	reader      *bufio.Reader
	writer      *bufio.Writer
	readCloser  io.ReadCloser
	writeCloser io.WriteCloser
	abort       func() error
	writeOnce   sync.Once
	writeErr    error
	abortOnce   sync.Once
	abortErr    error
	closed      bool
	aborted     bool
}

// NewClient waits for READY within ctx. Canceling READY or any later exchange
// aborts the generation and closes both inherited pipes; the client is then
// terminal because transaction commit state is unknown.
func NewClient(
	ctx context.Context,
	reader io.ReadCloser,
	writer io.WriteCloser,
	abort func() error,
) (*Client, error) {
	if reader == nil || writer == nil {
		return nil, fmt.Errorf("prepared worker protocol: nil client pipe")
	}
	client := &Client{
		reader:      bufio.NewReader(reader),
		writer:      bufio.NewWriter(writer),
		readCloser:  reader,
		writeCloser: writer,
		abort:       abort,
	}
	magic := make([]byte, len(protocolMagic))
	err := client.performIO(ctx, func() error {
		if _, err := io.ReadFull(client.reader, magic); err != nil {
			return fmt.Errorf("prepared worker protocol: read READY: %w", err)
		}
		if string(magic) != protocolMagic {
			return fmt.Errorf("prepared worker protocol: READY %q, want %q", magic, protocolMagic)
		}
		return nil
	})
	if err != nil {
		_ = client.abortIO()
		return nil, err
	}
	return client, nil
}

func (client *Client) Transact(
	ctx context.Context,
	request preparedworkerfixture.TurnRequest,
) (preparedworkerfixture.TurnResult, error) {
	if client == nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf("prepared worker protocol: closed client")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closed || client.reader == nil || client.writer == nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf("prepared worker protocol: closed client")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	payload, err := encodeTurnRequest(request)
	if err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	var response []byte
	err = client.performIO(ctx, func() error {
		if err := writeProtocolFrame(client.writer, payload); err != nil {
			return fmt.Errorf("prepared worker protocol: write request: %w", err)
		}
		var readErr error
		response, readErr = readProtocolFrame(client.reader)
		if readErr != nil {
			return fmt.Errorf("prepared worker protocol: read response: %w", readErr)
		}
		return nil
	})
	if err != nil {
		return preparedworkerfixture.TurnResult{}, client.failTerminal(err)
	}
	result, remoteError, err := decodeTurnResponse(response)
	if err != nil {
		return preparedworkerfixture.TurnResult{}, client.failTerminal(err)
	}
	if remoteError != "" {
		return preparedworkerfixture.TurnResult{}, errors.New(remoteError)
	}
	if request.Revision == ^uint64(0) ||
		result.Sequence != request.Sequence || result.Revision != request.Revision+1 {
		return preparedworkerfixture.TurnResult{}, client.failTerminal(fmt.Errorf(
			"prepared worker protocol: response position %d/%d does not advance request %d/%d",
			result.Sequence,
			result.Revision,
			request.Sequence,
			request.Revision,
		))
	}
	if err := ctx.Err(); err != nil {
		return preparedworkerfixture.TurnResult{}, client.failTerminal(err)
	}
	return result, nil
}

func (client *Client) failTerminal(err error) error {
	client.closed = true
	return errors.Join(err, client.abortIO())
}

func (client *Client) Close() error {
	if client == nil {
		return nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.closed = true
	return client.closeWrite()
}

func (client *Client) WasAborted() bool {
	if client == nil {
		return false
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.aborted
}

func (client *Client) performIO(ctx context.Context, operation func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	completed := make(chan error, 1)
	go func() { completed <- operation() }()
	select {
	case err := <-completed:
		return err
	case <-ctx.Done():
		client.closed = true
		return errors.Join(ctx.Err(), client.abortIO())
	}
}

func (client *Client) abortIO() error {
	client.aborted = true
	client.abortOnce.Do(func() {
		var failures []error
		if client.abort != nil {
			if err := client.abort(); err != nil {
				failures = append(failures, err)
			}
		}
		if err := client.closeWrite(); err != nil {
			failures = append(failures, err)
		}
		if client.readCloser != nil {
			if err := client.readCloser.Close(); err != nil {
				failures = append(failures, err)
			}
		}
		client.abortErr = errors.Join(failures...)
	})
	return client.abortErr
}

func (client *Client) closeWrite() error {
	client.writeOnce.Do(func() {
		if client.writeCloser != nil {
			client.writeErr = client.writeCloser.Close()
		}
	})
	return client.writeErr
}

func Serve(reader io.Reader, writer io.Writer, transactor TurnTransactor) error {
	if reader == nil || writer == nil || transactor == nil {
		return fmt.Errorf("prepared worker protocol: nil server input")
	}
	bufferedReader := bufio.NewReader(reader)
	bufferedWriter := bufio.NewWriter(writer)
	if _, err := bufferedWriter.WriteString(protocolMagic); err != nil {
		return fmt.Errorf("prepared worker protocol: write READY: %w", err)
	}
	if err := bufferedWriter.Flush(); err != nil {
		return fmt.Errorf("prepared worker protocol: flush READY: %w", err)
	}
	for {
		payload, err := readProtocolFrame(bufferedReader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("prepared worker protocol: read request: %w", err)
		}
		request, err := decodeTurnRequest(payload)
		if err != nil {
			return err
		}
		result, transactErr := transactor.Transact(context.Background(), request)
		payload, err = encodeTurnResponse(result, transactErr)
		if err != nil {
			responseErrorPayload, responseErr := encodeTurnResponse(
				preparedworkerfixture.TurnResult{},
				fmt.Errorf("prepared worker protocol: invalid transaction response: %w", err),
			)
			if responseErr != nil {
				return errors.Join(err, responseErr)
			}
			if writeErr := writeProtocolFrame(bufferedWriter, responseErrorPayload); writeErr != nil {
				return errors.Join(err, writeErr)
			}
			return err
		}
		if err := writeProtocolFrame(bufferedWriter, payload); err != nil {
			return fmt.Errorf("prepared worker protocol: write response: %w", err)
		}
	}
}

func encodeTurnRequest(request preparedworkerfixture.TurnRequest) ([]byte, error) {
	if err := validateTurnRequestRecord(request); err != nil {
		return nil, fmt.Errorf("prepared worker protocol: invalid request: %w", err)
	}
	encoder := protocolEncoder{}
	encoder.byte(protocolVersion)
	encoder.uint64(request.Sequence)
	encoder.uint64(request.Revision)
	encoder.int64(request.State.Tick)
	encoder.int64(request.State.Total)
	encoder.int64(request.State.Ready)
	encoder.int64(request.State.Entity7)
	encoder.int64(request.State.ModuleCalls)
	encoder.int64(request.Projection.Step)
	encoder.int64(request.Projection.Seed)
	encoder.int64(request.Projection.Work)
	encoder.uint16(uint16(len(request.Events)))
	for _, event := range request.Events {
		encoder.uint16(event.Route)
		encoder.uint32(event.Entity)
		encoder.int64(event.Amount)
	}
	encoder.uint16(uint16(len(request.Completions)))
	for _, completion := range request.Completions {
		encoder.uint64(completion.EffectID)
		encoder.byte(byte(completion.Status))
		encoder.int64(completion.Value)
	}
	return encoder.finish("request")
}

func decodeTurnRequest(payload []byte) (preparedworkerfixture.TurnRequest, error) {
	decoder := newProtocolDecoder(payload)
	if version := decoder.byte(); version != protocolVersion {
		return preparedworkerfixture.TurnRequest{}, fmt.Errorf(
			"prepared worker protocol: request version %d, want %d",
			version,
			protocolVersion,
		)
	}
	request := preparedworkerfixture.TurnRequest{
		Sequence: decoder.uint64(),
		Revision: decoder.uint64(),
		State: preparedworkerfixture.StateSnapshot{
			Tick:        decoder.int64(),
			Total:       decoder.int64(),
			Ready:       decoder.int64(),
			Entity7:     decoder.int64(),
			ModuleCalls: decoder.int64(),
		},
		Projection: preparedworkerfixture.Projection{
			Step: decoder.int64(),
			Seed: decoder.int64(),
			Work: decoder.int64(),
		},
	}
	eventCount := decoder.count("events")
	if eventCount != 0 {
		request.Events = make([]preparedworkerfixture.DamageEvent, eventCount)
	}
	for index := range request.Events {
		request.Events[index] = preparedworkerfixture.DamageEvent{
			Route:  decoder.uint16(),
			Entity: decoder.uint32(),
			Amount: decoder.int64(),
		}
	}
	completionCount := decoder.count("completions")
	if completionCount != 0 {
		request.Completions = make([]preparedworkerfixture.Completion, completionCount)
	}
	for index := range request.Completions {
		request.Completions[index] = preparedworkerfixture.Completion{
			EffectID: decoder.uint64(),
			Status:   preparedworkerfixture.CompletionStatus(decoder.byte()),
			Value:    decoder.int64(),
		}
	}
	if err := decoder.finish("request"); err != nil {
		return preparedworkerfixture.TurnRequest{}, err
	}
	if err := validateTurnRequestRecord(request); err != nil {
		return preparedworkerfixture.TurnRequest{}, fmt.Errorf("prepared worker protocol: invalid request: %w", err)
	}
	return request, nil
}

func encodeTurnResponse(result preparedworkerfixture.TurnResult, resultErr error) ([]byte, error) {
	encoder := protocolEncoder{}
	encoder.byte(protocolVersion)
	if resultErr != nil {
		message := resultErr.Error()
		if len(message) > maxProtocolError {
			return nil, fmt.Errorf("prepared worker protocol: response error exceeds %d bytes", maxProtocolError)
		}
		encoder.byte(responseError)
		encoder.string(message)
		return encoder.finish("error response")
	}
	if err := validateTurnResponseRecord(result); err != nil {
		return nil, fmt.Errorf("prepared worker protocol: invalid response: %w", err)
	}
	encoder.byte(responseOK)
	encoder.uint64(result.Sequence)
	encoder.uint64(result.Revision)
	encoder.int64(result.State.Tick)
	encoder.int64(result.State.Total)
	encoder.int64(result.State.Ready)
	encoder.int64(result.State.Entity7)
	encoder.int64(result.State.ModuleCalls)
	encoder.uint16(uint16(len(result.Commands)))
	for _, command := range result.Commands {
		encoder.byte(byte(command.Kind))
		encoder.uint32(command.Entity)
		encoder.int64(command.A)
		encoder.int64(command.B)
	}
	encoder.uint16(uint16(len(result.Effects)))
	for _, effect := range result.Effects {
		encoder.uint64(effect.ID)
		encoder.byte(byte(effect.Kind))
		encoder.uint32(effect.Entity)
		encoder.int64(effect.Value)
		encoder.boolean(effect.NeedsCompletion)
	}
	encoder.uint16(uint16(len(result.Pending)))
	for _, id := range result.Pending {
		encoder.uint64(id)
	}
	return encoder.finish("response")
}

func decodeTurnResponse(payload []byte) (preparedworkerfixture.TurnResult, string, error) {
	decoder := newProtocolDecoder(payload)
	if version := decoder.byte(); version != protocolVersion {
		return preparedworkerfixture.TurnResult{}, "", fmt.Errorf(
			"prepared worker protocol: response version %d, want %d",
			version,
			protocolVersion,
		)
	}
	switch status := decoder.byte(); status {
	case responseError:
		message := decoder.string(maxProtocolError)
		if err := decoder.finish("error response"); err != nil {
			return preparedworkerfixture.TurnResult{}, "", err
		}
		if message == "" {
			return preparedworkerfixture.TurnResult{}, "", fmt.Errorf("prepared worker protocol: empty error response")
		}
		return preparedworkerfixture.TurnResult{}, message, nil
	case responseOK:
	default:
		return preparedworkerfixture.TurnResult{}, "", fmt.Errorf(
			"prepared worker protocol: response status %d is invalid",
			status,
		)
	}
	result := preparedworkerfixture.TurnResult{
		Sequence: decoder.uint64(),
		Revision: decoder.uint64(),
		State: preparedworkerfixture.StateSnapshot{
			Tick:        decoder.int64(),
			Total:       decoder.int64(),
			Ready:       decoder.int64(),
			Entity7:     decoder.int64(),
			ModuleCalls: decoder.int64(),
		},
	}
	commandCount := decoder.count("commands")
	if commandCount != 0 {
		result.Commands = make([]preparedworkerfixture.Command, commandCount)
	}
	for index := range result.Commands {
		result.Commands[index] = preparedworkerfixture.Command{
			Kind:   preparedworkerfixture.CommandKind(decoder.byte()),
			Entity: decoder.uint32(),
			A:      decoder.int64(),
			B:      decoder.int64(),
		}
	}
	effectCount := decoder.count("effects")
	if effectCount != 0 {
		result.Effects = make([]preparedworkerfixture.Effect, effectCount)
	}
	for index := range result.Effects {
		result.Effects[index] = preparedworkerfixture.Effect{
			ID:              decoder.uint64(),
			Kind:            preparedworkerfixture.EffectKind(decoder.byte()),
			Entity:          decoder.uint32(),
			Value:           decoder.int64(),
			NeedsCompletion: decoder.boolean(),
		}
	}
	pendingCount := decoder.count("pending")
	if pendingCount != 0 {
		result.Pending = make([]uint64, pendingCount)
	}
	for index := range result.Pending {
		result.Pending[index] = decoder.uint64()
	}
	if err := decoder.finish("response"); err != nil {
		return preparedworkerfixture.TurnResult{}, "", err
	}
	if err := validateTurnResponseRecord(result); err != nil {
		return preparedworkerfixture.TurnResult{}, "", fmt.Errorf("prepared worker protocol: invalid response: %w", err)
	}
	return result, "", nil
}

func validateTurnRequestRecord(request preparedworkerfixture.TurnRequest) error {
	if len(request.Events) > maxTurnItems || len(request.Completions) > maxTurnItems {
		return fmt.Errorf("item count exceeds %d", maxTurnItems)
	}
	if request.Sequence == 0 || request.Revision == ^uint64(0) {
		return fmt.Errorf("request position %d/%d is invalid", request.Sequence, request.Revision)
	}
	if err := validateState(request.State, "state"); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value int64
	}{
		{name: "projection step", value: request.Projection.Step},
		{name: "projection seed", value: request.Projection.Seed},
		{name: "projection work", value: request.Projection.Work},
	} {
		if err := exactGuestInteger(field.value, field.name); err != nil {
			return err
		}
	}
	for _, event := range request.Events {
		if event.Route != preparedworkerfixture.RouteDamage {
			return fmt.Errorf("event route %d is unknown", event.Route)
		}
		if err := exactGuestInteger(event.Amount, "event amount"); err != nil {
			return err
		}
	}
	seenCompletions := make(map[uint64]bool, len(request.Completions))
	for _, completion := range request.Completions {
		if completion.EffectID == 0 {
			return fmt.Errorf("completion effect ID is zero")
		}
		if seenCompletions[completion.EffectID] {
			return fmt.Errorf("completion effect ID %d is duplicated", completion.EffectID)
		}
		seenCompletions[completion.EffectID] = true
		if completion.Status != preparedworkerfixture.CompletionOK {
			return fmt.Errorf("completion status %d is unknown", completion.Status)
		}
		if err := exactGuestInteger(completion.Value, "completion value"); err != nil {
			return err
		}
	}
	return nil
}

func validateTurnResponseRecord(result preparedworkerfixture.TurnResult) error {
	if len(result.Commands) > maxTurnItems || len(result.Effects) > maxTurnItems || len(result.Pending) > maxTurnItems {
		return fmt.Errorf("item count exceeds %d", maxTurnItems)
	}
	if err := validateState(result.State, "state"); err != nil {
		return err
	}
	if result.Sequence == 0 || result.Revision == 0 {
		return fmt.Errorf("response position %d/%d is invalid", result.Sequence, result.Revision)
	}
	for _, command := range result.Commands {
		if command.Kind != preparedworkerfixture.CommandDraw && command.Kind != preparedworkerfixture.CommandFlash {
			return fmt.Errorf("command kind %d is unknown", command.Kind)
		}
		if err := exactGuestInteger(command.A, "command a"); err != nil {
			return err
		}
		if err := exactGuestInteger(command.B, "command b"); err != nil {
			return err
		}
	}
	seenEffects := make(map[uint64]bool, len(result.Effects))
	for _, effect := range result.Effects {
		if effect.ID == 0 {
			return fmt.Errorf("effect ID is zero")
		}
		if seenEffects[effect.ID] {
			return fmt.Errorf("effect ID %d is duplicated", effect.ID)
		}
		seenEffects[effect.ID] = true
		if effect.Kind != preparedworkerfixture.EffectLoad && effect.Kind != preparedworkerfixture.EffectAudio {
			return fmt.Errorf("effect kind %d is unknown", effect.Kind)
		}
		if err := exactGuestInteger(effect.Value, "effect value"); err != nil {
			return err
		}
	}
	seenPending := make(map[uint64]bool, len(result.Pending))
	var previousPending uint64
	for index, id := range result.Pending {
		if id == 0 {
			return fmt.Errorf("pending effect ID is zero")
		}
		if index != 0 && id <= previousPending {
			return fmt.Errorf("pending effect IDs are not strictly ordered")
		}
		if seenPending[id] {
			return fmt.Errorf("pending effect ID %d is duplicated", id)
		}
		seenPending[id] = true
		previousPending = id
	}
	for _, effect := range result.Effects {
		if effect.NeedsCompletion && !seenPending[effect.ID] {
			return fmt.Errorf("completion-bearing effect %d is not pending", effect.ID)
		}
		if !effect.NeedsCompletion && seenPending[effect.ID] {
			return fmt.Errorf("non-completion effect %d is pending", effect.ID)
		}
	}
	return nil
}

func readProtocolFrame(reader io.Reader) ([]byte, error) {
	var header [4]byte
	read, err := io.ReadFull(reader, header[:])
	if err != nil {
		if errors.Is(err, io.EOF) && read == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxProtocolFrame {
		return nil, fmt.Errorf("frame size %d is out of bounds", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeProtocolFrame(writer *bufio.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxProtocolFrame {
		return fmt.Errorf("frame size %d is out of bounds", len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

type protocolEncoder struct {
	bytes.Buffer
}

func (encoder *protocolEncoder) byte(value byte) {
	_ = encoder.WriteByte(value)
}

func (encoder *protocolEncoder) boolean(value bool) {
	if value {
		encoder.byte(1)
		return
	}
	encoder.byte(0)
}

func (encoder *protocolEncoder) uint16(value uint16) {
	var encoded [2]byte
	binary.BigEndian.PutUint16(encoded[:], value)
	_, _ = encoder.Write(encoded[:])
}

func (encoder *protocolEncoder) uint32(value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	_, _ = encoder.Write(encoded[:])
}

func (encoder *protocolEncoder) uint64(value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = encoder.Write(encoded[:])
}

func (encoder *protocolEncoder) int64(value int64) {
	encoder.uint64(uint64(value))
}

func (encoder *protocolEncoder) string(value string) {
	encoder.uint16(uint16(len(value)))
	_, _ = encoder.WriteString(value)
}

func (encoder *protocolEncoder) finish(kind string) ([]byte, error) {
	if encoder.Len() == 0 || encoder.Len() > maxProtocolFrame {
		return nil, fmt.Errorf("prepared worker protocol: %s is %d bytes", kind, encoder.Len())
	}
	return append([]byte(nil), encoder.Bytes()...), nil
}

type protocolDecoder struct {
	reader *bytes.Reader
	err    error
}

func newProtocolDecoder(payload []byte) *protocolDecoder {
	return &protocolDecoder{reader: bytes.NewReader(payload)}
}

func (decoder *protocolDecoder) byte() byte {
	if decoder.err != nil {
		return 0
	}
	value, err := decoder.reader.ReadByte()
	decoder.err = err
	return value
}

func (decoder *protocolDecoder) boolean() bool {
	value := decoder.byte()
	if decoder.err == nil && value > 1 {
		decoder.err = fmt.Errorf("boolean %d is invalid", value)
	}
	return value == 1
}

func (decoder *protocolDecoder) uint16() uint16 {
	var encoded [2]byte
	decoder.read(encoded[:])
	return binary.BigEndian.Uint16(encoded[:])
}

func (decoder *protocolDecoder) uint32() uint32 {
	var encoded [4]byte
	decoder.read(encoded[:])
	return binary.BigEndian.Uint32(encoded[:])
}

func (decoder *protocolDecoder) uint64() uint64 {
	var encoded [8]byte
	decoder.read(encoded[:])
	return binary.BigEndian.Uint64(encoded[:])
}

func (decoder *protocolDecoder) int64() int64 {
	return int64(decoder.uint64())
}

func (decoder *protocolDecoder) string(limit int) string {
	length := int(decoder.uint16())
	if decoder.err != nil {
		return ""
	}
	if length > limit {
		decoder.err = fmt.Errorf("string size %d exceeds %d", length, limit)
		return ""
	}
	value := make([]byte, length)
	decoder.read(value)
	return string(value)
}

func (decoder *protocolDecoder) count(kind string) int {
	count := int(decoder.uint16())
	if decoder.err == nil && count > maxTurnItems {
		decoder.err = fmt.Errorf("%s count %d exceeds %d", kind, count, maxTurnItems)
	}
	if decoder.err != nil {
		return 0
	}
	return count
}

func (decoder *protocolDecoder) read(destination []byte) {
	if decoder.err != nil {
		return
	}
	_, decoder.err = io.ReadFull(decoder.reader, destination)
}

func (decoder *protocolDecoder) finish(kind string) error {
	if decoder.err != nil {
		return fmt.Errorf("prepared worker protocol: decode %s: %w", kind, decoder.err)
	}
	if decoder.reader.Len() != 0 {
		return fmt.Errorf("prepared worker protocol: decode %s: %d trailing bytes", kind, decoder.reader.Len())
	}
	return nil
}
