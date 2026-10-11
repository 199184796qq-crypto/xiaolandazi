package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Prevent another live/preview handler from silently restoring legacy policy
// reads. Only explicit legacy policy editing/adoption/sandbox tools may read
// that catalog; none supplies facts to a runtime worker.
func TestGenerationCannotReadLegacyPolicyCatalog(t *testing.T) {
	for _, dir := range []string{".", "../decisionexecutor"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			if dir == "." && (name == "live_policy_agent.go" || name == "live_policy_learning.go" || name == "live_policy_sandbox.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "LoadLivePolicyLayers" {
					t.Errorf("%s reads legacy policy during generation", name)
				}
				return true
			})
		}
	}
}
