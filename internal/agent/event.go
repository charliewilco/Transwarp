package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

var emitMu sync.Mutex

type Event struct {
	Kind     string    `json:"kind"`
	Message  string    `json:"message"`
	BuildID  string    `json:"build_id,omitempty"`
	JobID    string    `json:"job_id,omitempty"`
	Sequence int       `json:"sequence,omitempty"`
	Time     time.Time `json:"time"`
}

func Emit(event Event) {
	emitMu.Lock()
	defer emitMu.Unlock()

	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	data, err := json.Marshal(event)
	if err != nil {
		fmt.Fprintf(os.Stdout, "{\"kind\":\"error\",\"message\":\"encode event: %s\",\"time\":\"%s\"}\n", err.Error(), time.Now().Format(time.RFC3339Nano))
		return
	}

	fmt.Fprintln(os.Stdout, string(data))
}
