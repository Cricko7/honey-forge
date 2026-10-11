//go:build !windows

package agent

import (
	"os"
	"os/signal"
	"syscall"
)

func handleStdioPipeSignals() func() {
	// Stdout is a TLS transport in worker mode. A closed parent pipe must
	// return EPIPE instead of terminating the worker before deferred cleanup.
	// Notify drops excess signals; the write error drives connection shutdown.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGPIPE)
	return func() { signal.Stop(signals) }
}
