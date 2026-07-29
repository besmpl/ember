package main

import (
	"context"
	"fmt"
	"os"

	"github.com/besmpl/ember/internal/preparedworkerprobe"
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
		preparedworkerprobe.TransactionRunnerContract(),
		preparedworkerprobe.NewTransactionHandler,
	)
}
