package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/projectboard/projectboard/internal/runnercli"
)

func main() {
	app := runnercli.New(runnercli.Options{Out: os.Stdout, Err: os.Stderr})
	if err := app.Run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var usageErr *runnercli.UsageError
		if errors.As(err, &usageErr) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
