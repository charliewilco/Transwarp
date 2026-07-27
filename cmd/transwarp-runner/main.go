package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/charliewilco/transwarp/internal/agent"
)

func main() {
	configPath := flag.String("config", "", "path to agent configuration JSON")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "missing -config")
		os.Exit(2)
	}

	var configuration agent.Config
	var err error
	if *configPath == "-" {
		var data []byte
		data, err = io.ReadAll(os.Stdin)
		if err == nil {
			configuration, err = agent.LoadConfigData(data)
		}
	} else {
		configuration, err = agent.LoadConfig(*configPath)
	}
	if err != nil {
		agent.Emit(agent.Event{Kind: "error", Message: err.Error()})
		os.Exit(1)
	}
	capabilities := agent.DefaultCapabilities()
	if err := agent.ValidateSupportedHost(capabilities); err != nil {
		agent.Emit(agent.Event{Kind: "error", Message: err.Error()})
		os.Exit(1)
	}
	configuration.Capabilities = capabilities

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if parentPID, ok := parentProcessID(); ok {
		go stopWhenParentExits(ctx, stop, parentPID)
	}

	runner := agent.New(configuration)
	if err := runner.Run(ctx); err != nil {
		agent.Emit(agent.Event{Kind: "error", Message: err.Error()})
		os.Exit(1)
	}
}

func parentProcessID() (int, bool) {
	value := os.Getenv("TRANSWARP_PARENT_PID")
	if value == "" {
		return 0, false
	}
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func stopWhenParentExits(ctx context.Context, stop context.CancelFunc, parentPID int) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if os.Getppid() == 1 || !processExists(parentPID) {
				stop()
				return
			}
		}
	}
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
