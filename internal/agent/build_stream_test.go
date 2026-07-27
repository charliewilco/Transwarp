package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildStreamSerializesConcurrentWrites(t *testing.T) {
	stream := newBuildStream("build-123", "job", Redactor{})

	var wg sync.WaitGroup
	for worker := 0; worker < 10; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for index := 0; index < 50; index++ {
				stream.write(Event{Kind: "log", Message: "line"})
			}
		}(worker)
	}
	wg.Wait()

	seen := map[int]bool{}
	events := stream.eventsAfter(0)
	for _, event := range events {
		if event.Sequence == 0 {
			t.Fatal("expected sequence")
		}
		if event.BuildID != "build-123" {
			t.Fatalf("unexpected build id: %s", event.BuildID)
		}
		if seen[event.Sequence] {
			t.Fatalf("duplicate sequence %d", event.Sequence)
		}
		seen[event.Sequence] = true
	}
	if len(events) != 500 {
		t.Fatalf("expected 500 events, got %d", len(events))
	}
}

func TestBuildStreamMirrorsRedactedEventsToRunnerLog(t *testing.T) {
	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer

	stream := newBuildStream("build-123", "job", Redactor{values: []string{"super-secret"}}).mirroringToRunnerLog()
	stream.write(Event{Kind: "log", Message: "token=super-secret"})

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = originalStdout
	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("mirrored event was not JSON: %s", string(data))
	}
	if event.Message != "token=[redacted]" {
		t.Fatalf("unexpected mirrored message: %s", event.Message)
	}
	if event.BuildID != "build-123" || event.JobID != "job" || event.Sequence != 1 {
		t.Fatalf("mirrored event lost metadata: %+v", event)
	}
}

func TestBuildStreamCapsRetainedEventsAndReportsTruncation(t *testing.T) {
	withMaxRetainedBuildEvents(t, 3)

	stream := newBuildStream("build-123", "job", Redactor{})
	for _, message := range []string{"one", "two", "three", "four", "five"} {
		stream.write(Event{Kind: "log", Message: message})
	}

	events := stream.eventsAfter(0)
	if len(events) != 4 {
		t.Fatalf("expected truncation notice plus 3 retained events, got %d: %+v", len(events), events)
	}
	if events[0].Kind != "info" || !strings.Contains(events[0].Message, "truncated before sequence 3") {
		t.Fatalf("missing truncation notice: %+v", events[0])
	}
	if events[0].Sequence != 2 {
		t.Fatalf("unexpected truncation sequence: %d", events[0].Sequence)
	}
	if events[1].Message != "three" || events[1].Sequence != 3 {
		t.Fatalf("unexpected first retained event: %+v", events[1])
	}

	recent := stream.eventsAfter(3)
	if len(recent) != 2 || recent[0].Message != "four" || recent[1].Message != "five" {
		t.Fatalf("recent replay should not include truncation notice: %+v", recent)
	}
}

func TestBuildStreamWritesTransientKeepalive(t *testing.T) {
	withBuildStreamKeepaliveInterval(t, 10*time.Millisecond)

	stream := newBuildStream("build-123", "job", Redactor{})
	reader, writer := io.Pipe()
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go stream.writeTo(writer, nil, 0, true, done, ctx)

	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(reader).ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		lineCh <- line
	}()

	var line string
	select {
	case line = <-lineCh:
	case err := <-errCh:
		t.Fatalf("read keepalive: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for keepalive")
	}

	cancel()
	_ = reader.Close()
	_ = writer.Close()

	var event Event
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("keepalive was not JSON: %s", line)
	}
	if event.Kind != "info" || event.Message != "build stream keepalive" {
		t.Fatalf("unexpected keepalive event: %+v", event)
	}
	if event.BuildID != "build-123" || event.JobID != "job" {
		t.Fatalf("keepalive lost stream metadata: %+v", event)
	}
	if event.Sequence != 0 {
		t.Fatalf("keepalive should not consume retained sequence, got %d", event.Sequence)
	}
	if events := stream.eventsAfter(0); len(events) != 0 {
		t.Fatalf("keepalive should not be retained, got %+v", events)
	}
}

func withMaxRetainedBuildEvents(t *testing.T, limit int) {
	t.Helper()

	original := maxRetainedBuildEvents
	maxRetainedBuildEvents = limit
	t.Cleanup(func() {
		maxRetainedBuildEvents = original
	})
}

func withBuildStreamKeepaliveInterval(t *testing.T, interval time.Duration) {
	t.Helper()

	original := buildStreamKeepaliveInterval
	buildStreamKeepaliveInterval = interval
	t.Cleanup(func() {
		buildStreamKeepaliveInterval = original
	})
}
