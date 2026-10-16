package main

import (
	"fmt"
	"os"

	"github.com/measix/measix-tuck/internal/tuck"
)

func main() {
	if err := tuck.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
