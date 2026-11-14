// Command gh-actions-version is the `gh actions-version` extension: it validates and repairs the
// semantic version tags, branches and releases of an action repository.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/jessehouwing/gh-actions-version/internal/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cmd.Run(ctx, os.Args[1:], cmd.OSEnv())
	stop()
	os.Exit(code)
}
