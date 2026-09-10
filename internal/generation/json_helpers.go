package generation

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeJSONDocument(raw string) ([]byte, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, fmt.Errorf("response was empty")
	}
	text = strings.TrimPrefix(text, "\ufeff")
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 2 {
			start := 1
			end := len(lines)
			if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
				end--
			}
			text = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		}
	}
	start := strings.IndexAny(text, "{[")
	if start > 0 {
		text = text[start:]
	}
	lastObject := strings.LastIndex(text, "}")
	lastArray := strings.LastIndex(text, "]")
	end := lastObject
	if lastArray > end {
		end = lastArray
	}
	if end >= 0 && end+1 < len(text) {
		text = text[:end+1]
	}
	if !json.Valid([]byte(text)) {
		return nil, fmt.Errorf("response does not contain a complete valid JSON document")
	}
	return []byte(text), nil
}

func explainJSONDecodeError(err error) string {
	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "unexpected end of json input"), strings.Contains(lower, "unexpected eof"):
		return "the model response appears to have been truncated before the JSON was complete"
	case strings.Contains(lower, "invalid character"):
		return "the model response contained characters that do not form valid JSON"
	default:
		return message
	}
}
