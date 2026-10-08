package httpapi

import "strings"

// Stable paragraphs precede paragraphs containing request-specific bindings.
// The contents of both groups are preserved. Only template layout changes;
// roles, conversation history and provider transport are never flattened.
func renderStablePrefixTemplate(template string, bindings ...string) string {
	replacer := strings.NewReplacer(bindings...)
	stable, variable := []string{}, []string{}
	for _, block := range strings.Split(strings.ReplaceAll(template, "\r\n", "\n"), "\n\n") {
		if strings.Contains(block, "{{") {
			variable = append(variable, replacer.Replace(block))
		} else {
			stable = append(stable, block)
		}
	}
	return strings.Join(append(stable, variable...), "\n\n")
}
