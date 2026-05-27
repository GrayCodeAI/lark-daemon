package main

import (
	"fmt"
	"os"

	"lark-daemon/internal/server"
)

func main() {
	cfg := server.LoadConfig()

	srv, err := server.New(*cfg)

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
