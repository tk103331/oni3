package agent

import (
	"strings"
	"unicode"
)

// toolDisplayName formats a presentation label without changing the tool's
// identity. Keep namespaces visible so tools from different sources stay distinct.
func toolDisplayName(name string) string {
	parts := strings.Split(name, "__")
	for i, part := range parts {
		var label strings.Builder
		for _, word := range strings.FieldsFunc(part, func(r rune) bool {
			return r == '_' || r == '-' || unicode.IsSpace(r)
		}) {
			switch word {
			case "http", "https", "api", "js", "mcp", "url", "json", "sql":
				label.WriteString(strings.ToUpper(word))
			case "javascript":
				label.WriteString("JavaScript")
			case "duckduckgo":
				label.WriteString("DuckDuckGo")
			default:
				for j, r := range word {
					if j == 0 {
						r = unicode.ToUpper(r)
					}
					label.WriteRune(r)
				}
			}
		}
		parts[i] = label.String()
	}
	return strings.Join(parts, " · ")
}
