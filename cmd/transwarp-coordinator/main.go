package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/charliewilco/transwarp/internal/coordinator"
)

func main() {
	address := flag.String("listen", env("TRANSWARP_COORDINATOR_LISTEN", "127.0.0.1:8288"), "coordinator listen address")
	token := flag.String("token", os.Getenv("TRANSWARP_COORDINATOR_TOKEN"), "coordinator bearer token")
	targetToken := flag.String("target-token", os.Getenv("TRANSWARP_COORDINATOR_TARGET_TOKEN"), "bearer token for Transwarp target registration and result callbacks; defaults to coordinator token when omitted")
	transwarpToken := flag.String("transwarp-token", os.Getenv("TRANSWARP_TOKEN"), "bearer token used to call Transwarp runners")
	accessClientID := flag.String("access-client-id", os.Getenv("TRANSWARP_ACCESS_CLIENT_ID"), "optional Cloudflare Access service token client ID for runner tunnel calls")
	accessClientSecret := flag.String("access-client-secret", os.Getenv("TRANSWARP_ACCESS_CLIENT_SECRET"), "optional Cloudflare Access service token client secret for runner tunnel calls")
	publicURL := flag.String("public-url", env("TRANSWARP_COORDINATOR_PUBLIC_URL", "http://"+env("TRANSWARP_COORDINATOR_LISTEN", "127.0.0.1:8288")), "coordinator URL reachable by Transwarp result callbacks")
	statePath := flag.String("state-path", env("TRANSWARP_COORDINATOR_STATE_PATH", defaultStatePath()), "path to the coordinator target/result state file")
	resultWaitTimeout := flag.Duration("result-wait-timeout", envDuration("TRANSWARP_COORDINATOR_RESULT_WAIT_TIMEOUT", 10*time.Second), "how long coordinator dispatch waits for the terminal result callback after runner logs finish")
	flag.Parse()

	if *transwarpToken == "" {
		fmt.Fprintln(os.Stderr, "missing -transwarp-token or TRANSWARP_TOKEN")
		os.Exit(2)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "missing -token or TRANSWARP_COORDINATOR_TOKEN")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := coordinator.New(coordinator.Options{
		Token:              *token,
		TargetToken:        *targetToken,
		TranswarpToken:     *transwarpToken,
		AccessClientID:     *accessClientID,
		AccessClientSecret: *accessClientSecret,
		PublicURL:          *publicURL,
		StatePath:          *statePath,
		ResultWaitTimeout:  *resultWaitTimeout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("transwarp-coordinator listening on http://%s\n", *address)
	if err := coordinator.Serve(ctx, *address, server); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid %s duration %q\n", key, value)
		os.Exit(2)
	}
	return duration
}

func defaultStatePath() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(directory, "Transwarp", "coordinator-state.json")
}
