package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestPipedInputDoesNotBlockAPrompt(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	start := time.Now()
	got, err := addPipedInput("How are you today?", r, 50*time.Millisecond)
	if err != nil || got != "How are you today?" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if time.Second < time.Since(start) {
		t.Fatal("an open stdin with no data must not block the -p prompt")
	}
}

func TestPipedInputJoinsThePrompt(t *testing.T) {
	got, err := addPipedInput("Explain this:", strings.NewReader("package main\n"), time.Second)
	if err != nil || got != "Explain this:\n\npackage main" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = addPipedInput("Just this", strings.NewReader(""), time.Second)
	if err != nil || got != "Just this" {
		t.Fatalf("empty stdin keeps the prompt: %q err=%v", got, err)
	}
}

func TestPipedInputReadsSlowDataToTheEnd(t *testing.T) {
	r, w := io.Pipe()
	go func() {
		w.Write([]byte("first part "))
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte("second part"))
		w.Close()
	}()
	got, err := addPipedInput("Summarize:", r, 50*time.Millisecond)
	if err != nil || got != "Summarize:\n\nfirst part second part" {
		t.Fatalf("once data starts, all of it is read: %q err=%v", got, err)
	}
}

func TestPipedInputWaitsWhenItIsThePrompt(t *testing.T) {
	r, w := io.Pipe()
	go func() {
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte("late prompt"))
		w.Close()
	}()
	got, err := addPipedInput("", r, 10*time.Millisecond)
	if err != nil || got != "late prompt" {
		t.Fatalf("without -p, mga waits for stdin: %q err=%v", got, err)
	}
}
