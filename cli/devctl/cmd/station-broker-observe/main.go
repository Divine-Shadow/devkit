package main

import (
	"devkit/cli/devctl/internal/runtime/broker"
	"fmt"
	"os"
)

func main() {
	if err := broker.ObserveSelectedStationProcess(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
