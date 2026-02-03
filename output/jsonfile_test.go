// ABOUTME: Tests for JSON file output writer.
// ABOUTME: Covers JSON serialization, buffering, flushing, and file rotation.

package output

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogEntry_JSONSerialization(t *testing.T) {
	entry := &LogEntry{
		Timestamp:      "2024-01-15T10:30:00.000Z",
		Severity:       "INFO",
		SeverityNumber: 9,
		Body:           "test message",
		Attributes:     map[string]string{"key": "value"},
		ResourceAttrs:  map[string]string{"app_name": "my-app"},
		Routing:        RoutingInfo{Index: "tas_logs", Rule: "default"},
		Transforms:     []string{"Renamed: application_name -> cf_app_name"},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Failed to marshal LogEntry: %v", err)
	}

	// Verify it can be unmarshaled back
	var decoded LogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal LogEntry: %v", err)
	}

	if decoded.Severity != "INFO" {
		t.Errorf("Severity = %q, want %q", decoded.Severity, "INFO")
	}
	if decoded.Body != "test message" {
		t.Errorf("Body = %q, want %q", decoded.Body, "test message")
	}
	if decoded.Routing.Index != "tas_logs" {
		t.Errorf("Routing.Index = %q, want %q", decoded.Routing.Index, "tas_logs")
	}
}

func TestJSONLWriter_WritesOneJSONPerLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	w, err := NewJSONWriter(path, FormatJSONL, 10, 5*time.Second, 100*1024*1024)
	if err != nil {
		t.Fatalf("NewJSONWriter failed: %v", err)
	}

	entry1 := &LogEntry{Timestamp: "t1", Severity: "INFO", Body: "msg1"}
	entry2 := &LogEntry{Timestamp: "t2", Severity: "ERROR", Body: "msg2"}

	w.Write(entry1)
	w.Write(entry2)
	w.Close()

	// Read and verify
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Failed to open output: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	var e1, e2 LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &e1); err != nil {
		t.Errorf("Line 1 is not valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &e2); err != nil {
		t.Errorf("Line 2 is not valid JSON: %v", err)
	}

	if e1.Body != "msg1" {
		t.Errorf("Line 1 body = %q, want %q", e1.Body, "msg1")
	}
	if e2.Body != "msg2" {
		t.Errorf("Line 2 body = %q, want %q", e2.Body, "msg2")
	}
}

func TestJSONWriter_FlushesAtBufferSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	// Buffer size of 3
	w, err := NewJSONWriter(path, FormatJSONL, 3, 1*time.Hour, 100*1024*1024)
	if err != nil {
		t.Fatalf("NewJSONWriter failed: %v", err)
	}
	defer w.Close()

	// Write 2 entries - should not flush yet
	w.Write(&LogEntry{Body: "msg1"})
	w.Write(&LogEntry{Body: "msg2"})

	info, _ := os.Stat(path)
	if info != nil && info.Size() > 0 {
		t.Error("File should be empty before buffer is full")
	}

	// Write 3rd entry - should trigger flush
	w.Write(&LogEntry{Body: "msg3"})

	// Give a moment for async flush
	time.Sleep(10 * time.Millisecond)

	info, err = os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Error("File should have content after buffer fills")
	}
}

func TestJSONWriter_FlushesAtInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	// Large buffer, short interval
	w, err := NewJSONWriter(path, FormatJSONL, 1000, 50*time.Millisecond, 100*1024*1024)
	if err != nil {
		t.Fatalf("NewJSONWriter failed: %v", err)
	}
	defer w.Close()

	// Write 1 entry - below buffer threshold
	w.Write(&LogEntry{Body: "msg1"})

	// Wait for interval flush
	time.Sleep(100 * time.Millisecond)

	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Error("File should have content after flush interval")
	}
}

func TestJSONWriter_RotatesAtSizeThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	// Tiny rotation size (100 bytes) for testing
	w, err := NewJSONWriter(path, FormatJSONL, 1, 1*time.Hour, 100)
	if err != nil {
		t.Fatalf("NewJSONWriter failed: %v", err)
	}
	defer w.Close()

	// Write enough to trigger rotation
	for i := 0; i < 5; i++ {
		w.Write(&LogEntry{Body: "this is a long message to fill the file quickly for rotation test"})
	}

	// Check for rotated file
	rotated := path + ".1"
	if _, err := os.Stat(rotated); os.IsNotExist(err) {
		t.Error("Rotated file should exist")
	}
}

func TestJSONWriter_GracefulShutdownFlushesBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.jsonl")

	// Large buffer, long interval - only Close should flush
	w, err := NewJSONWriter(path, FormatJSONL, 1000, 1*time.Hour, 100*1024*1024)
	if err != nil {
		t.Fatalf("NewJSONWriter failed: %v", err)
	}

	w.Write(&LogEntry{Body: "msg1"})
	w.Write(&LogEntry{Body: "msg2"})

	// File should be empty before close
	info, _ := os.Stat(path)
	if info != nil && info.Size() > 0 {
		t.Error("File should be empty before Close")
	}

	// Close should flush
	w.Close()

	// Now file should have content
	info, err = os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Error("File should have content after Close")
	}

	// Verify content
	data, _ := os.ReadFile(path)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("Expected 2 lines, got %d", lines)
	}
}
