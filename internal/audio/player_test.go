package audio

import (
	"os"
	"testing"
	"time"
)

func TestNewPlayer(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer() error: %v", err)
	}
	defer p.Close()

	if p.chimePath == "" {
		t.Error("expected chimePath to be set")
	}
	if _, err := os.Stat(p.chimePath); err != nil {
		t.Errorf("chime file should exist: %v", err)
	}
}

func TestPlayer_Close_RemovesFile(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer() error: %v", err)
	}
	path := p.chimePath
	p.Close()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected chime file to be removed after Close")
	}
}

func TestPlayer_Debounce(t *testing.T) {
	p, err := NewPlayer()
	if err != nil {
		t.Fatalf("NewPlayer() error: %v", err)
	}
	defer p.Close()

	// Override chimePath to a nonexistent file so afplay fails silently
	// (we only care about debounce logic, not actual playback).
	p.chimePath = "/dev/null"

	// First call should update lastPlayed.
	p.Play()
	first := p.LastPlayedTime()

	if first.IsZero() {
		t.Fatal("expected lastPlayed to be set after first Play()")
	}

	// Second call within debounce window should be suppressed.
	time.Sleep(10 * time.Millisecond)
	p.Play()
	second := p.LastPlayedTime()

	if !second.Equal(first) {
		t.Errorf("expected debounce to suppress second Play(); lastPlayed changed from %v to %v", first, second)
	}
}
