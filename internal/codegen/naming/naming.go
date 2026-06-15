// Package naming converts schema names into Go identifiers.
package naming

import "strings"

var initialisms = map[string]string{
	"ac":    "AC",
	"dc":    "DC",
	"ev":    "EV",
	"evse":  "EVSE",
	"http":  "HTTP",
	"iccid": "ICCID",
	"id":    "ID",
	"imsi":  "IMSI",
	"json":  "JSON",
	"ocpp":  "OCPP",
	"soc":   "SOC",
	"uri":   "URI",
	"url":   "URL",
}

// Export converts a JSON property name to an exported Go identifier, preserving
// common protocol initialisms at camel-case word boundaries.
func Export(name string) string {
	if name == "" {
		return ""
	}
	words := splitCamel(name)
	var b strings.Builder
	for _, w := range words {
		lw := strings.ToLower(w)
		if up, ok := initialisms[lw]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	return b.String()
}

func splitCamel(s string) []string {
	runes := []rune(s)
	words := make([]string, 0, len(runes))
	start := 0
	for i := 1; i < len(runes); i++ {
		if isLower(runes[i-1]) && isUpper(runes[i]) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	return append(words, string(runes[start:]))
}

func isLower(r rune) bool { return r >= 'a' && r <= 'z' }
func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
