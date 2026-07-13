package ember

import (
	"errors"
	"fmt"
	"strings"
)

// ScriptFrame identifies one active script invocation at the instruction that
// caused a runtime error (or at the call site for a caller frame).
type ScriptFrame struct {
	Source   string
	Function string
	Line     int
}

// RuntimeError is an execution error annotated with the active script stack.
// Frames[0] is the innermost script invocation. Cause remains available to
// errors.Is/errors.As callers through Unwrap.
type RuntimeError struct {
	Message string
	Frames  []ScriptFrame
	Cause   error
}

func (e *RuntimeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Message
	if message == "" && e.Cause != nil {
		message = e.Cause.Error()
	}
	if len(e.Frames) == 0 {
		return message
	}
	var builder strings.Builder
	builder.WriteString(message)
	for _, frame := range e.Frames {
		builder.WriteString("\n  at ")
		if frame.Source == "" {
			builder.WriteString("<unknown>")
		} else {
			builder.WriteString(frame.Source)
		}
		if frame.Line > 0 {
			builder.WriteByte(':')
			builder.WriteString(fmt.Sprint(frame.Line))
		}
		if frame.Function != "" {
			builder.WriteString(" in ")
			builder.WriteString(frame.Function)
		}
	}
	return builder.String()
}

func (e *RuntimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func runtimeErrorAlreadyWrapped(err error) bool {
	if err == nil {
		return false
	}
	var runtimeErr *RuntimeError
	return errors.As(err, &runtimeErr)
}

func newRuntimeError(err error, frames []ScriptFrame) error {
	if err == nil || runtimeErrorAlreadyWrapped(err) {
		return err
	}
	copyFrames := append([]ScriptFrame(nil), frames...)
	return &RuntimeError{Message: err.Error(), Frames: copyFrames, Cause: err}
}

func newRuntimeErrorWithController(err error, frames []ScriptFrame, controller *executionController) error {
	if controller == nil || len(controller.inheritedScriptFrames) == 0 {
		return newRuntimeError(err, frames)
	}
	combined := make([]ScriptFrame, 0, len(frames)+len(controller.inheritedScriptFrames))
	combined = append(combined, frames...)
	combined = append(combined, controller.inheritedScriptFrames...)
	return newRuntimeError(err, combined)
}

func runtimeScriptFrame(proto *Proto, pc int) (ScriptFrame, bool) {
	if proto == nil {
		return ScriptFrame{}, false
	}
	frame := ScriptFrame{Line: protoLineAt(proto, pc)}
	if proto.debugInfo != nil {
		frame.Source = proto.debugInfo.sourceName
		frame.Function = proto.debugInfo.functionName
	}
	return frame, true
}

func protoLineAt(proto *Proto, pc int) int {
	if proto == nil || pc < 0 {
		return 0
	}
	if len(proto.wordLines) != 0 {
		if pc >= len(proto.wordLines) {
			return 0
		}
		if line := proto.wordLines[pc]; line > 0 {
			return line
		}
		// Compiler-generated words can lack a direct source entry (for
		// example, the primary of an AUX-bearing call). Attribute them to
		// the preceding source instruction, never to a following line.
		for index := pc - 1; index >= 0; index-- {
			if line := proto.wordLines[index]; line > 0 {
				return line
			}
		}
		return 0
	}
	if pc >= len(proto.lines) {
		return 0
	}
	return proto.lines[pc]
}

// previousWordcodeInstruction returns the primary word immediately before pc.
// It deliberately walks instruction widths instead of looking only at pc-1:
// an AUX payload occupies a physical word but has no source line of its own.
func previousWordcodeInstruction(proto *Proto, pc int) int {
	if proto == nil || pc <= 0 {
		return pc
	}
	words := proto.words
	if pc > len(words) {
		pc = len(words)
	}
	previous := 0
	for word := 0; word < pc; {
		previous = word
		if word >= len(words) {
			break
		}
		next := word + 1
		op := opcode(uint8(words[word]) & uint8(wordcodeOpcodeMask))
		if meta, ok := opcodeMetadata(op); ok && meta.wordcode.aux != wordcodeAuxNone && words[word]&wordcodeAuxBit != 0 {
			next++
		}
		if next > pc {
			break
		}
		word = next
	}
	return previous
}

func (thread *vmThread) captureRuntimeError(err error, current *vmFrame, baseDepth int) error {
	if err == nil || runtimeErrorAlreadyWrapped(err) {
		return err
	}
	frames := thread.captureScriptFrames(current, baseDepth)
	return newRuntimeErrorWithController(err, frames, thread.controller)
}

func (thread *vmThread) captureScriptFrames(current *vmFrame, baseDepth int) []ScriptFrame {
	if thread == nil {
		return nil
	}
	if baseDepth < 0 {
		baseDepth = 0
	}
	frames := make([]ScriptFrame, 0, len(thread.frames)+len(thread.frameRecords))
	appendFrame := func(proto *Proto, pc int) {
		if frame, ok := runtimeScriptFrame(proto, pc); ok {
			frames = append(frames, frame)
		}
	}
	active := current
	if active == nil && len(thread.frames) > baseDepth {
		active = thread.frames[len(thread.frames)-1]
	}
	if active != nil {
		appendFrame(active.proto, active.pc)
	}
	activeDepth := -1
	if active != nil {
		activeDepth = active.depth
	}
	appendRecords := func(depth int) {
		for index := len(thread.frameRecords) - 1; index >= 0; index-- {
			record := thread.frameRecords[index]
			if record.flags&vmFrameRecordFlagRecordOnly == 0 || int(record.frameDepth) != depth || record.closure == nil || record.closure.proto == nil {
				continue
			}
			returnPC, ok := vmFrameRecordUint32ToInt(record.returnPC)
			if !ok {
				continue
			}
			appendFrame(record.closure.proto, previousWordcodeInstruction(record.closure.proto, returnPC))
		}
	}
	if activeDepth >= 0 {
		appendRecords(activeDepth)
	}
	for index := len(thread.frames) - 2; index >= baseDepth; index-- {
		frame := thread.frames[index]
		if frame == nil {
			continue
		}
		appendFrame(frame.proto, previousWordcodeInstruction(frame.proto, frame.pc))
		appendRecords(frame.depth)
	}
	return frames
}
