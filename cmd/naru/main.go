// Command naru verifies that an AI agent's work is actually done.
package main

import (
	"os"

	"github.com/Chaebin-Park/naru/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
