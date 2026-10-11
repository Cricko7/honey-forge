//go:build windows

package agent

// Windows pipe writes already return an error without raising SIGPIPE.
func handleStdioPipeSignals() func() { return func() {} }
