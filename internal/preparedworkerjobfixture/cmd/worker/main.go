package main

import (
	"context"
	"fmt"
	"os"

	"github.com/besmpl/ember/internal/preparedworkerjobfixture"
	"github.com/besmpl/ember/preparedworker"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return preparedworker.ServeWorker(
		context.Background(),
		preparedworkerjobfixture.RunnerContract(),
		preparedworkerjobfixture.NewHandlerV2,
	)
}
