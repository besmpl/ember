package main

import (
	"context"
	"fmt"
	"os"

	"github.com/besmpl/ember/internal/preparedworkerparity"
	"github.com/besmpl/ember/preparedworker"
)

func main() {
	if err := preparedworker.ServeWorker(
		context.Background(),
		preparedworkerparity.Contract(),
		preparedworkerparity.NewHandler,
	); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
