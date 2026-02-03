// ABOUTME: App allowlist filtering with file loading and hot-reload.
// ABOUTME: Filters logs based on application name against a configurable list.

package allowlist

import (
	"bufio"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// Allowlist manages a list of allowed application names
type Allowlist struct {
	mu   sync.RWMutex
	apps map[string]bool // lowercase app names for case-insensitive matching
}

// NewAllowlist creates an allowlist from a slice of app names
func NewAllowlist(apps []string) *Allowlist {
	al := &Allowlist{
		apps: make(map[string]bool),
	}
	for _, app := range apps {
		trimmed := strings.TrimSpace(app)
		if trimmed != "" {
			al.apps[strings.ToLower(trimmed)] = true
		}
	}
	return al
}

// LoadFromFile loads an allowlist from a file.
// File format: one app name per line, lines starting with # are comments.
func LoadFromFile(path string) (*Allowlist, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var apps []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		apps = append(apps, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return NewAllowlist(apps), nil
}

// IsAllowed checks if a log record's app is in the allowlist.
// Returns true if the allowlist is empty (allow all) or if the app is in the list.
func (al *Allowlist) IsAllowed(lr *logspb.LogRecord) bool {
	al.mu.RLock()
	defer al.mu.RUnlock()

	// Empty allowlist means allow all
	if len(al.apps) == 0 {
		return true
	}

	appName := getAttributeValue(lr, "cf_app_name")
	if appName == "" {
		appName = getAttributeValue(lr, "application_name")
	}

	return al.apps[strings.ToLower(appName)]
}

// Apps returns a copy of the current allowed apps list
func (al *Allowlist) Apps() []string {
	al.mu.RLock()
	defer al.mu.RUnlock()

	apps := make([]string, 0, len(al.apps))
	for app := range al.apps {
		apps = append(apps, app)
	}
	return apps
}

// WatchFile watches the allowlist file for changes and reloads when modified.
// Runs until stop channel is closed. Accepts optional channels:
//   - reloaded: signals after each successful reload
//   - ready: signals when watcher is initialized and listening
func (al *Allowlist) WatchFile(path string, stop <-chan struct{}, reloaded chan<- struct{}, ready chan<- struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		return
	}

	// Signal that watcher is ready
	if ready != nil {
		close(ready)
	}

	for {
		select {
		case <-stop:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				al.reload(path)
				if reloaded != nil {
					select {
					case reloaded <- struct{}{}:
					default:
					}
				}
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// reload reads the file and updates the allowlist
func (al *Allowlist) reload(path string) {
	newList, err := LoadFromFile(path)
	if err != nil {
		return // Keep existing list on error
	}

	al.mu.Lock()
	al.apps = newList.apps
	al.mu.Unlock()
}

// getAttributeValue retrieves a string attribute value by key
func getAttributeValue(lr *logspb.LogRecord, key string) string {
	for _, attr := range lr.GetAttributes() {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}
