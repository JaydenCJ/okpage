// Command okpage probes your services and renders a static status page you
// can host anywhere. See `okpage help` and the README for usage.
package main

import (
	"os"

	"github.com/JaydenCJ/okpage/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
