package main

import (
	"fmt"
	"os"

	"github.com/Felipyan19/tunneldesk/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "TunnelDesk:", err)
		os.Exit(1)
	}
}
