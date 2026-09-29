//go:build !windows

package daemon

import (
	"os"
	"os/signal"
	"syscall"
)

func notifyStopSignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
}
