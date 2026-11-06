package planprompt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// RuntimePrompts contains only explicitly saved runtime overrides. ContextKey
// binds them to the unchanged persona, facts and red lines of a live snapshot.
type RuntimePrompts struct {
	Sections   Sections
	Planning   string
	Revision   string
	Enabled    bool
	Structured bool
}

func (p RuntimePrompts) ContextKey() string {
	s := p.Sections.Normalize()
	raw, _ := json.Marshal([]string{s.PersonaMD, s.FactsMD, s.RedLinesMD})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// RewriteFor preserves a live version's exact business context. Arbitrary old
// templates cannot be reverse parsed or silently replaced with current facts.
func (p RuntimePrompts) RewriteFor(frozen string) (string, bool) {
	prefix := rewriteContext(p.Sections) + "\n\n【样本话术】\n"
	if !p.Enabled || !strings.HasPrefix(strings.ReplaceAll(frozen, "\r\n", "\n"), prefix) {
		return frozen, false
	}
	return CompileRewritePlanning(p.Sections, p.Planning), true
}
