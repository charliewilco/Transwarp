package agent

import "os"

func defaultInterruptSignal() os.Signal {
	return os.Interrupt
}
