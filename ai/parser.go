/*
Package ai provides structured output extraction from raw LLM responses.
*/
package ai

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var jsonCodeBlockRegex = regexp.MustCompile("(?s)```(?:json)?\\s*([\\{\\[].*?[\\}\\]])\\s*```")

// ExtractJSON parses structured JSON output from an LLM response string.
// It handles raw JSON payloads as well as markdown code fences (```json ... ```)
// and trims extraneous conversational preambles before unmarshaling into target.
func ExtractJSON(raw string, target any) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("ai: cannot extract JSON from empty response")
	}

	// 1. Check for markdown code fences: ```json { ... } ``` or ``` [ ... ] ```
	if match := jsonCodeBlockRegex.FindStringSubmatch(trimmed); len(match) > 1 {
		jsonText := strings.TrimSpace(match[1])
		if err := json.Unmarshal([]byte(jsonText), target); err == nil {
			return nil
		}
	}

	// 2. Direct JSON attempt
	if err := json.Unmarshal([]byte(trimmed), target); err == nil {
		return nil
	}

	// 3. Fallback: Locate outermost braces '{' ... '}' or '[' ... ']'
	startBrace := strings.IndexAny(trimmed, "{[")
	if startBrace != -1 {
		var closeChar byte = '}'
		if trimmed[startBrace] == '[' {
			closeChar = ']'
		}
		endBrace := strings.LastIndexByte(trimmed, closeChar)
		if endBrace > startBrace {
			slice := trimmed[startBrace : endBrace+1]
			if err := json.Unmarshal([]byte(slice), target); err == nil {
				return nil
			}
		}
	}

	return fmt.Errorf("ai: failed to extract valid JSON from response:\n%s", raw)
}
