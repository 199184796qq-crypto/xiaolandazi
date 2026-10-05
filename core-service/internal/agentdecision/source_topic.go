package agentdecision

import "strings"

// SourceTopic keeps the semantic identity of an interaction across sources.
// Manual answers, agent suggestions and future vector recall should converge
// on the same topic instead of creating duplicate questions.
type SourceTopic struct {
	Canonical string
	Source    Source
	Original  string
}

func ResolveSourceTopic(source Source, topic, question string) SourceTopic {
	original := strings.TrimSpace(topic)
	if original == "" {
		original = strings.TrimSpace(question)
	}
	return SourceTopic{
		Canonical: canonicalSourceTopic(original),
		Source: source,
		Original: original,
	}
}

func canonicalSourceTopic(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ToLower(value)
	if value == "" {
		return "unknown"
	}
	return value
}
