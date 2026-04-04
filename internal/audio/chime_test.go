package audio

import (
	"encoding/binary"
	"os"
	"testing"
)

func TestGenerateChime_WAVHeader(t *testing.T) {
	path, err := GenerateChime()
	if err != nil {
		t.Fatalf("GenerateChime() error: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading chime file: %v", err)
	}

	if len(data) < 44 {
		t.Fatalf("WAV file too short: %d bytes", len(data))
	}

	// RIFF header
	if string(data[0:4]) != "RIFF" {
		t.Errorf("expected RIFF, got %q", string(data[0:4]))
	}

	// WAVE format
	if string(data[8:12]) != "WAVE" {
		t.Errorf("expected WAVE, got %q", string(data[8:12]))
	}

	// fmt subchunk
	if string(data[12:16]) != "fmt " {
		t.Errorf("expected 'fmt ', got %q", string(data[12:16]))
	}

	// Audio format: PCM = 1
	audioFormat := binary.LittleEndian.Uint16(data[20:22])
	if audioFormat != 1 {
		t.Errorf("expected PCM format (1), got %d", audioFormat)
	}

	// Channels: mono = 1
	channels := binary.LittleEndian.Uint16(data[22:24])
	if channels != 1 {
		t.Errorf("expected 1 channel, got %d", channels)
	}

	// Sample rate: 44100
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	if sampleRate != 44100 {
		t.Errorf("expected sample rate 44100, got %d", sampleRate)
	}

	// Bits per sample: 16
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])
	if bitsPerSample != 16 {
		t.Errorf("expected 16 bits per sample, got %d", bitsPerSample)
	}

	// data subchunk
	if string(data[36:40]) != "data" {
		t.Errorf("expected 'data', got %q", string(data[36:40]))
	}

	// Data size should match remaining bytes
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	if int(dataSize) != len(data)-44 {
		t.Errorf("data size %d doesn't match actual %d", dataSize, len(data)-44)
	}

	// RIFF chunk size = file size - 8
	chunkSize := binary.LittleEndian.Uint32(data[4:8])
	if int(chunkSize) != len(data)-8 {
		t.Errorf("RIFF chunk size %d doesn't match expected %d", chunkSize, len(data)-8)
	}

	// Sanity: ~200ms at 44100Hz mono 16-bit = ~17640 bytes of sample data
	if dataSize < 15000 || dataSize > 20000 {
		t.Errorf("unexpected data size %d (expected ~17640 for 200ms)", dataSize)
	}
}
