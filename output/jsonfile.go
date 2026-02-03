// ABOUTME: JSON file output writer for transformed logs.
// ABOUTME: Supports JSONL format with buffered writes and file rotation.

package output

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Format specifies the JSON output format
type Format string

const (
	FormatJSONL Format = "jsonl" // Line-delimited JSON
	FormatJSON  Format = "json"  // JSON array
)

// RoutingInfo contains routing decision details
type RoutingInfo struct {
	Index string `json:"index"`
	Rule  string `json:"rule"`
}

// LogEntry represents a transformed log record for JSON output
type LogEntry struct {
	Timestamp      string            `json:"timestamp"`
	Severity       string            `json:"severity"`
	SeverityNumber int32             `json:"severity_number"`
	Body           string            `json:"body"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	ResourceAttrs  map[string]string `json:"resource_attributes,omitempty"`
	Routing        RoutingInfo       `json:"routing"`
	Transforms     []string          `json:"transforms_applied,omitempty"`
}

// JSONWriter writes log entries to a JSON file with buffering and rotation
type JSONWriter struct {
	mu            sync.Mutex
	path          string
	format        Format
	bufferSize    int
	flushInterval time.Duration
	maxFileSize   int64

	buffer []*LogEntry
	file   *os.File
	stop   chan struct{}
	done   chan struct{}
}

// NewJSONWriter creates a new JSON file writer
func NewJSONWriter(path string, format Format, bufferSize int, flushInterval time.Duration, maxFileSize int64) (*JSONWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	w := &JSONWriter{
		path:          path,
		format:        format,
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		maxFileSize:   maxFileSize,
		buffer:        make([]*LogEntry, 0, bufferSize),
		file:          file,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}

	go w.flushLoop()

	return w, nil
}

// Write adds a log entry to the buffer
func (w *JSONWriter) Write(entry *LogEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buffer = append(w.buffer, entry)

	if len(w.buffer) >= w.bufferSize {
		w.flushLocked()
	}
}

// Close flushes remaining entries and closes the file
func (w *JSONWriter) Close() error {
	close(w.stop)
	<-w.done

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buffer) > 0 {
		w.flushLocked()
	}

	return w.file.Close()
}

// flushLoop periodically flushes the buffer
func (w *JSONWriter) flushLoop() {
	defer close(w.done)

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.mu.Lock()
			if len(w.buffer) > 0 {
				w.flushLocked()
			}
			w.mu.Unlock()
		}
	}
}

// flushLocked writes buffered entries to file. Caller must hold mu.
func (w *JSONWriter) flushLocked() {
	if len(w.buffer) == 0 {
		return
	}

	// Check for rotation before writing
	w.rotateIfNeeded()

	for _, entry := range w.buffer {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		w.file.Write(data)
		w.file.Write([]byte("\n"))
	}

	w.file.Sync()
	w.buffer = w.buffer[:0]
}

// rotateIfNeeded rotates the log file if it exceeds maxFileSize
func (w *JSONWriter) rotateIfNeeded() {
	info, err := w.file.Stat()
	if err != nil {
		return
	}

	if info.Size() < w.maxFileSize {
		return
	}

	// Close current file
	w.file.Close()

	// Rotate: rename current to .1
	rotatedPath := w.path + ".1"
	os.Remove(rotatedPath) // Remove old rotated file if exists
	os.Rename(w.path, rotatedPath)

	// Open new file
	w.file, _ = os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}
