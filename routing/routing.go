// ABOUTME: Log routing logic for determining Splunk index destination.
// ABOUTME: Uses configurable rules with regex matching and priority ordering.

package routing

import (
	"regexp"
	"sort"

	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
)

// RoutingRule defines a single routing rule (for configuration)
type RoutingRule struct {
	Name       string            // Rule name for logging
	Conditions map[string]string // Attribute name → regex pattern
	Index      string            // Target Splunk index
	Priority   int               // Lower = higher priority
}

// compiledRule is a routing rule with pre-compiled regexes
type compiledRule struct {
	Name       string
	Conditions map[string]*regexp.Regexp // Pre-compiled patterns
	Index      string
	Priority   int
}

// Router holds routing rules and applies them to logs
type Router struct {
	rules        []compiledRule
	defaultIndex string
}

// NewRouter creates a router with custom rules, pre-compiling regex patterns
func NewRouter(rules []RoutingRule) *Router {
	// Sort rules by priority (lower = higher priority)
	sorted := make([]RoutingRule, len(rules))
	copy(sorted, rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	// Compile all regex patterns
	compiled := make([]compiledRule, len(sorted))
	for i, rule := range sorted {
		compiled[i] = compiledRule{
			Name:       rule.Name,
			Conditions: make(map[string]*regexp.Regexp),
			Index:      rule.Index,
			Priority:   rule.Priority,
		}
		for attr, pattern := range rule.Conditions {
			// Severity patterns are not regexes, store nil
			if attr == "_severity" {
				compiled[i].Conditions[attr] = nil
				continue
			}
			compiled[i].Conditions[attr] = regexp.MustCompile(pattern)
		}
	}

	return &Router{
		rules:        compiled,
		defaultIndex: "tas_logs",
	}
}

// DefaultRouter creates a router with the default TAS routing rules
func DefaultRouter() *Router {
	return NewRouter([]RoutingRule{
		{
			Name:       "error-severity",
			Conditions: map[string]string{"_severity": "error"},
			Index:      "tas_errors",
			Priority:   1,
		},
		{
			Name:       "security-app",
			Conditions: map[string]string{"cf_app_name": "^security-"},
			Index:      "tas_security",
			Priority:   2,
		},
		{
			Name:       "audit-app",
			Conditions: map[string]string{"cf_app_name": "^audit-"},
			Index:      "tas_audit",
			Priority:   3,
		},
		{
			Name:       "production-space",
			Conditions: map[string]string{"cf_space_name": "^production$"},
			Index:      "tas_prod",
			Priority:   4,
		},
	})
}

// Route determines which index a log should be sent to.
// Returns the index name and the rule name that matched.
func (r *Router) Route(lr *logspb.LogRecord) (index string, ruleName string) {
	for _, rule := range r.rules {
		if r.matchesRule(lr, rule) {
			return rule.Index, rule.Name
		}
	}
	return r.defaultIndex, "default"
}

// matchesRule checks if a log matches all conditions of a rule
func (r *Router) matchesRule(lr *logspb.LogRecord, rule compiledRule) bool {
	for attrName, compiledPattern := range rule.Conditions {
		// Special handling for severity (stored as nil pattern)
		if attrName == "_severity" {
			if !r.matchesSeverity(lr, "error") { // severity rules always check for error+
				return false
			}
			continue
		}

		// Regular attribute matching with pre-compiled regex
		value := getAttributeValue(lr, attrName)
		if value == "" {
			return false
		}

		if !compiledPattern.MatchString(value) {
			return false
		}
	}
	return true
}

// matchesSeverity checks if the log severity matches the pattern
func (r *Router) matchesSeverity(lr *logspb.LogRecord, pattern string) bool {
	severity := lr.GetSeverityNumber()

	switch pattern {
	case "error":
		return severity >= logspb.SeverityNumber_SEVERITY_NUMBER_ERROR
	case "warn":
		return severity >= logspb.SeverityNumber_SEVERITY_NUMBER_WARN
	case "info":
		return severity >= logspb.SeverityNumber_SEVERITY_NUMBER_INFO
	case "debug":
		return severity >= logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG
	default:
		return false
	}
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
