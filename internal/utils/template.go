package utils

import (
	"regexp"
	"strings"
)

// TemplateProcessor processes text templates with variable replacement and conditional fields.
type TemplateProcessor struct {
	replacements map[string]string
	template     string
}

// NewTemplateProcessor creates a new template processor with the given template string.
func NewTemplateProcessor(template string) *TemplateProcessor {
	return &TemplateProcessor{
		replacements: make(map[string]string),
		template:     template,
	}
}

// Set adds a key-value pair for template replacement.
// Returns the processor for method chaining.
func (tp *TemplateProcessor) Set(key, value string) *TemplateProcessor {
	tp.replacements[key] = value
	return tp
}

// Process applies all replacements and returns the processed template string.
func (tp *TemplateProcessor) Process() string {
	result := tp.template

	for key, value := range tp.replacements {
		result = strings.ReplaceAll(result, key, value)
		result = processConditionalField(result, "{"+key+"}", value)
	}

	return cleanupTemplate(result)
}

func processConditionalField(template, fieldName, value string) string {
	pattern := regexp.MustCompile(`\{([^{}]+)\}`)
	result := template

	for {
		matches := pattern.FindAllStringSubmatch(result, -1)
		if len(matches) == 0 {
			break
		}

		changed := false
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}

			fullMatch := match[0]
			content := match[1]

			if strings.Contains(content, fieldName) {
				replacedContent := strings.ReplaceAll(content, fieldName, value)
				replacedContent = removeEmptyBrackets(replacedContent)
				result = strings.ReplaceAll(result, fullMatch, replacedContent)
				changed = true
			}
		}

		if !changed {
			break
		}
	}

	return result
}

func removeEmptyBrackets(s string) string {
	patterns := []string{
		`\{\s*\[\s*\]\s*\}`,
		`\{\s*\(\s*\)\s*\}`,
		`\{\s*\{\s*\}\s*\}`,
		`\(\s*\)`,
		`\[\s*\]`,
		`\{\s*\}`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		s = re.ReplaceAllString(s, "")
	}

	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func cleanupTemplate(s string) string {
	emptyPatterns := []string{
		"{[]}", "{()}", "{{}}", "{}", "()", "[]",
		"{ }", "( )", "[ ]",
	}

	for _, pattern := range emptyPatterns {
		s = strings.ReplaceAll(s, pattern, "")
	}

	bracketReplacements := map[string]string{
		"{[": "[", "]}": "]",
		"{(": "(", ")}": ")",
		"{{": "{", "}}": "}",
		"[{": "[", "}]": "]",
		"({": "(", "})": ")",
	}

	for old, new := range bracketReplacements {
		s = strings.ReplaceAll(s, old, new)
	}

	s = strings.ReplaceAll(s, "  ", " ")
	return strings.TrimSpace(s)
}
