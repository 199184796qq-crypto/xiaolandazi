package httpapi

import "strings"

// atomicFactsScopeValid rejects model-produced atomic facts whose explicit
// product scope is malformed. A malformed prefix must fall back to the
// deterministic splitter instead of failing plan initialization later.
func atomicFactsScopeValid(text string) bool {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, explicitCommonFactPrefix) && !strings.HasPrefix(line, explicitProductFactPrefix) {
			continue
		}
		if _, explicit, err := explicitPlanFactScope(line); !explicit || err != nil {
			return false
		}
	}
	return true
}
