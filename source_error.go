package ember

import (
	"errors"
	"fmt"
)

const (
	sourceErrorSyntax  = "syntax-error"
	sourceErrorCompile = "compile-error"
)

// SourceError describes a source-loading failure without requiring callers to
// parse an error message. Start and End are byte offsets into Source.Text.
type SourceError struct {
	Source  Source
	Code    string
	Message string
	Start   int
	End     int
	Cause   error
}

func (e *SourceError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Message
}

// Unwrap preserves errors.Is and errors.As for the underlying failure.
func (e *SourceError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type sourcePositionError struct {
	stage string
	start int
	end   int
	err   error
}

func (e *sourcePositionError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *sourcePositionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func positionedSourceError(stage string, start, end int, err error) error {
	if err == nil {
		return nil
	}
	return &sourcePositionError{
		stage: stage,
		start: start,
		end:   end,
		err:   err,
	}
}

func sourceErrorFor(source Source, err error) error {
	if err == nil {
		return nil
	}
	var existing *SourceError
	if errors.As(err, &existing) {
		return err
	}
	var positioned *sourcePositionError
	if !errors.As(err, &positioned) {
		return &SourceError{
			Source:  source,
			Code:    sourceErrorCompile,
			Message: err.Error(),
			Cause:   err,
		}
	}
	start := clampSourceOffset(positioned.start, len(source.Text))
	end := clampSourceOffset(positioned.end, len(source.Text))
	if end < start {
		end = start
	}
	code := sourceErrorCompile
	if positioned.stage == "lex" || positioned.stage == "parse" {
		code = sourceErrorSyntax
	}
	return &SourceError{
		Source:  source,
		Code:    code,
		Message: err.Error(),
		Start:   start,
		End:     end,
		Cause:   err,
	}
}

func clampSourceOffset(offset, length int) int {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}

func sourceTokenRange(tokens []sourceToken, tokenIndex, fallback, sourceLength int) (int, int) {
	if tokenIndex >= 0 && tokenIndex < len(tokens) {
		return tokens[tokenIndex].startOffset(), tokens[tokenIndex].endOffset()
	}
	start := clampSourceOffset(fallback, sourceLength)
	end := start
	if start < sourceLength {
		end++
	}
	return start, end
}

func formatSourceStageError(stage string, offset int, format string, args ...any) error {
	return fmt.Errorf("%s: byte %d: %s", stage, offset, fmt.Sprintf(format, args...))
}
