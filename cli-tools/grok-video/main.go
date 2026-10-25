package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	cli "github.com/measix/measix-tuck/cli-tools/common"
)

var version = "0.1.0"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	if err == nil {
		return
	}
	code := cli.ExitCode(err)
	if ctx.Err() != nil {
		code = 130
	}
	fmt.Fprintln(os.Stderr, "grok-video:", err)
	os.Exit(code)
}
