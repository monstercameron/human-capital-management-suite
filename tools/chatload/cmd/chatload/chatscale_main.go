package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/monstercameron/human-capital-management-suite/tools/chatload"
)

func main() { os.Exit(entry()) }
func entry() int {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return chatload.Command(ctx, os.Args[1:], os.Stdout, os.Getenv("HCMNEXT_TEST_DATABASE_URL"))
}
