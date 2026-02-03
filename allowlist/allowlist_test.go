// ABOUTME: Tests for app allowlist filtering.
// ABOUTME: Covers file loading, comments, case-insensitivity, and hot-reload.

package allowlist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Helper to create a log record with app name
func makeLogRecord(appName string) *logspb.LogRecord {
	return &logspb.LogRecord{
		Attributes: []*commonpb.KeyValue{
			{
				Key: "cf_app_name",
				Value: &commonpb.AnyValue{
					Value: &commonpb.AnyValue_StringValue{StringValue: appName},
				},
			},
		},
	}
}

func TestAllowlist_SpecificAppsOnly(t *testing.T) {
	al := NewAllowlist([]string{"my-app", "other-app"})

	if !al.IsAllowed(makeLogRecord("my-app")) {
		t.Error("my-app should be allowed")
	}
	if !al.IsAllowed(makeLogRecord("other-app")) {
		t.Error("other-app should be allowed")
	}
	if al.IsAllowed(makeLogRecord("unknown-app")) {
		t.Error("unknown-app should NOT be allowed")
	}
}

func TestAllowlist_EmptyAllowsAll(t *testing.T) {
	al := NewAllowlist([]string{})

	if !al.IsAllowed(makeLogRecord("any-app")) {
		t.Error("empty allowlist should allow all apps")
	}
	if !al.IsAllowed(makeLogRecord("another-app")) {
		t.Error("empty allowlist should allow all apps")
	}
}

func TestAllowlist_NilAllowsAll(t *testing.T) {
	al := NewAllowlist(nil)

	if !al.IsAllowed(makeLogRecord("any-app")) {
		t.Error("nil allowlist should allow all apps")
	}
}

func TestAllowlist_CaseInsensitive(t *testing.T) {
	al := NewAllowlist([]string{"My-App"})

	if !al.IsAllowed(makeLogRecord("my-app")) {
		t.Error("should match lowercase")
	}
	if !al.IsAllowed(makeLogRecord("MY-APP")) {
		t.Error("should match uppercase")
	}
	if !al.IsAllowed(makeLogRecord("My-App")) {
		t.Error("should match exact case")
	}
}

func TestLoadFromFile_BasicFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")

	content := "app-one\napp-two\napp-three\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	al, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !al.IsAllowed(makeLogRecord("app-one")) {
		t.Error("app-one should be allowed")
	}
	if !al.IsAllowed(makeLogRecord("app-two")) {
		t.Error("app-two should be allowed")
	}
	if al.IsAllowed(makeLogRecord("app-four")) {
		t.Error("app-four should NOT be allowed")
	}
}

func TestLoadFromFile_CommentsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")

	content := "# This is a comment\nmy-app\n# Another comment\nother-app\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	al, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !al.IsAllowed(makeLogRecord("my-app")) {
		t.Error("my-app should be allowed")
	}
	if !al.IsAllowed(makeLogRecord("other-app")) {
		t.Error("other-app should be allowed")
	}
	// Comments should not be treated as app names
	if al.IsAllowed(makeLogRecord("# This is a comment")) {
		t.Error("comment line should NOT be an allowed app")
	}
}

func TestLoadFromFile_EmptyLinesIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")

	content := "app-one\n\n\napp-two\n   \napp-three\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	al, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	apps := al.Apps()
	if len(apps) != 3 {
		t.Errorf("expected 3 apps, got %d: %v", len(apps), apps)
	}
}

func TestLoadFromFile_EmptyFileAllowsAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	al, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if !al.IsAllowed(makeLogRecord("any-app")) {
		t.Error("empty file should allow all apps")
	}
}

func TestHotReload_UpdatesAllowlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")

	// Initial content
	if err := os.WriteFile(path, []byte("app-one\n"), 0644); err != nil {
		t.Fatal(err)
	}

	al, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Start watching with reload signal channel
	stop := make(chan struct{})
	reloaded := make(chan struct{}, 1)
	ready := make(chan struct{})
	defer close(stop)
	go al.WatchFile(path, stop, reloaded, ready)

	// Wait for watcher to be ready
	<-ready

	// Verify initial state
	if !al.IsAllowed(makeLogRecord("app-one")) {
		t.Error("app-one should be allowed initially")
	}
	if al.IsAllowed(makeLogRecord("app-two")) {
		t.Error("app-two should NOT be allowed initially")
	}

	// Update file
	if err := os.WriteFile(path, []byte("app-one\napp-two\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for reload signal
	select {
	case <-reloaded:
		// Reload completed
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reload")
	}

	// Verify updated state
	if !al.IsAllowed(makeLogRecord("app-two")) {
		t.Error("app-two should be allowed after reload")
	}
}
