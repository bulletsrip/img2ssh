//go:build windows

package daemon

import (
	"os"
	"os/signal"
)

func notifyStopSignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
