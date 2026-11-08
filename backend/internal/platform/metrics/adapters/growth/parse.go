package growth

import "strings"

func splitLines(text string) []string {
	return strings.Split(text, "\n")
}

func splitKV(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, found := strings.Cut(line, ":")
	if !found {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}
