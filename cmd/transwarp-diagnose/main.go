package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/charliewilco/transwarp/internal/clienv"
	"github.com/charliewilco/transwarp/internal/diagnose"
)

func main() {
	request := diagnose.Request{}
	minCPUCount, err := clienv.NonNegativeInt("TRANSWARP_MIN_CPU_COUNT", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	minMemoryBytes, err := clienv.Uint64("TRANSWARP_MIN_MEMORY_BYTES", 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	timeout := flag.Duration("timeout", 30*time.Second, "overall diagnostic timeout")

	flag.StringVar(&request.BaseURL, "url", os.Getenv("TRANSWARP_URL"), "Transwarp public base URL")
	flag.StringVar(&request.CoordinatorURL, "coordinator-url", os.Getenv("TRANSWARP_COORDINATOR_URL"), "optional Transwarp coordinator base URL")
	flag.StringVar(&request.Token, "token", os.Getenv("TRANSWARP_TOKEN"), "Transwarp bearer token")
	flag.StringVar(&request.CoordinatorToken, "coordinator-token", os.Getenv("TRANSWARP_COORDINATOR_TOKEN"), "Transwarp coordinator bearer token")
	flag.StringVar(&request.MachineID, "machine-id", os.Getenv("TRANSWARP_MACHINE_ID"), "optional registered machine ID when diagnosing through the coordinator")
	flag.StringVar(&request.AccessClientID, "access-client-id", os.Getenv("TRANSWARP_ACCESS_CLIENT_ID"), "optional Cloudflare Access service token client ID")
	flag.StringVar(&request.AccessClientSecret, "access-client-secret", os.Getenv("TRANSWARP_ACCESS_CLIENT_SECRET"), "optional Cloudflare Access service token client secret")
	flag.StringVar(&request.JobID, "job", os.Getenv("TRANSWARP_JOB"), "optional configured job ID that must be advertised")
	flag.IntVar(&request.MinCPUCount, "min-cpu-count", minCPUCount, "optional minimum CPU core count")
	flag.Uint64Var(&request.MinMemoryBytes, "min-memory-bytes", minMemoryBytes, "optional minimum memory bytes")
	flag.StringVar(&request.MinXcodeVersion, "min-xcode-version", os.Getenv("TRANSWARP_MIN_XCODE_VERSION"), "optional minimum Xcode version")
	flag.BoolVar(&request.AllowHTTP, "allow-http", os.Getenv("TRANSWARP_DIAGNOSE_ALLOW_HTTP") == "1", "allow http URLs for local diagnostics")
	flag.Parse()

	request.Timeout = time.Duration(*timeout)
	if err := diagnose.Run(context.Background(), nil, request, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
