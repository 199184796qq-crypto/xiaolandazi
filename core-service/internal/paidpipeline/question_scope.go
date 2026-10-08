package paidpipeline

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var questionQualifier = regexp.MustCompile(`[0-9一二两三四五六七八九十]+\s*(件|份|包|盒|斤|克|个|套|瓶|袋|公斤)|但是|那如果|另外|不一样|另一款|这个型号|那个型号`)

func qualifiedQuestionTopic(topic, text string) string {
	qualifiers := questionQualifier.FindAllString(strings.TrimSpace(text), -1)
	if len(qualifiers) == 0 {
		return topic
	}
	return fmt.Sprintf("%s:SCOPE:%x", topic, sha256.Sum256([]byte(strings.Join(qualifiers, "|"))))
}
