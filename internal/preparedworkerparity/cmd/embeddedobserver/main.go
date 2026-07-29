// Command embeddedobserver is the production-compiled embedded-release timing
// observer used by the prepared-worker admission harness. Its line protocol is
// evidence tooling; the measured operation is the public OpenEmbedded Runner.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerparity"
	"github.com/besmpl/ember/preparedworker"
)

const embeddedObserverReady = "EMBER-EMBEDDED-OBSERVER-1"

func main() {
	if err := runMain(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMain() error {
	stateRoot, err := os.MkdirTemp("", "ember-embedded-observer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stateRoot)
	return serve(os.Stdin, os.Stdout, filepath.Join(stateRoot, "state.journal"))
}

func serve(input io.Reader, output io.Writer, statePath string) error {
	if input == nil || output == nil || statePath == "" {
		return fmt.Errorf("embedded observer: incomplete transport or state path")
	}
	runner, err := preparedworker.OpenEmbedded(
		context.Background(),
		preparedworker.Options[
			preparedworkerparity.Request,
			preparedworkerparity.Result,
			preparedworkerparity.Checkpoint,
			preparedworkerparity.Effect,
		]{
			Stream:    "embedded-release-all37-admission",
			Contract:  preparedworkerparity.Contract(),
			StatePath: statePath, MaxStateBytes: 64 << 20,
			ShutdownTimeout: 5 * time.Second,
			Deliver: func(context.Context, preparedworker.Delivery[preparedworkerparity.Effect]) error {
				return nil
			},
		},
		preparedworkerparity.NewHandler,
	)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = runner.Close(ctx)
		}
	}()

	writer := bufio.NewWriter(output)
	if _, err := fmt.Fprintln(writer, embeddedObserverReady); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 256), 1024)
	var position preparedworker.Position
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 1 && fields[0] == "close" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := runner.Close(ctx)
			cancel()
			if err != nil {
				return err
			}
			closed = true
			if _, err := fmt.Fprintln(writer, "bye"); err != nil {
				return err
			}
			return writer.Flush()
		}
		if len(fields) != 4 || fields[0] != "call" {
			return fmt.Errorf("embedded observer: invalid request frame")
		}
		caseValue, err := strconv.ParseUint(fields[1], 10, 16)
		if err != nil || caseValue >= preparedworkerparity.CaseCount {
			return fmt.Errorf("embedded observer: invalid case")
		}
		iterations, err := strconv.ParseUint(fields[2], 10, 32)
		if err != nil || iterations == 0 || iterations > preparedworkerparity.MaxIterations {
			return fmt.Errorf("embedded observer: invalid iterations")
		}
		seed, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return fmt.Errorf("embedded observer: invalid seed")
		}
		operation := preparedworker.Operation[preparedworkerparity.Request]{
			Sequence:     position.Sequence + 1,
			BaseRevision: position.Revision,
			Request: preparedworkerparity.Request{
				Case: uint16(caseValue), Iterations: uint32(iterations), Seed: seed,
			},
		}
		start := time.Now()
		completion, err := runner.Apply(context.Background(), operation)
		elapsed := time.Since(start)
		if err != nil {
			return err
		}
		position = completion.Position
		if _, err := fmt.Fprintf(writer, "ok %d %d\n", elapsed.Nanoseconds(), completion.Result.Checksum); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("embedded observer: parent closed without close frame")
}
