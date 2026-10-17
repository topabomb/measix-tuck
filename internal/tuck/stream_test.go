package tuck

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestTransferIdleBudgetResetsOnProgress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	body := watchIdle(reader, 100*time.Millisecond, cancel)
	defer body.Close()
	defer writer.Close()
	for range 6 {
		go writer.Write([]byte("x"))
		if _, err := io.ReadFull(body, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("progress was treated as an overall deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stalled transfer was not canceled")
	}
}

func TestCompletedRequestBodyDoesNotCancelConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := watchRequestIdle(io.NopCloser(strings.NewReader("sent")), 20*time.Millisecond, cancel)
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("completed request canceled its pending response")
	case <-time.After(50 * time.Millisecond):
	}
	response := watchIdle(io.NopCloser(strings.NewReader("confirmed")), time.Second, cancel)
	response.Close()
	if ctx.Err() != context.Canceled {
		t.Fatal("closing the response did not release its operation")
	}
}
