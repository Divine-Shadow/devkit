package main

import (
	"context"
	"os"

	"devkit/cli/devctl/internal/productsourceacquire"
)

func main() {
	os.Exit(productsourceacquire.Execute(context.Background(), os.Args[1:], os.Stdout))
}
