// Command logvert converts logfmt, syslog, Apache/nginx, and JSON logs
// into one normalized JSONL stream. All logic lives in internal/cli so it
// can be tested in-process; main only wires up the real streams.
package main

import (
	"os"

	"github.com/JaydenCJ/logvert/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
