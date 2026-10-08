package httpapi

import (
	"strings"
	"testing"
)

func TestPromptCacheLayoutStablePrefixAndNoRoleFlattening(t *testing.T) {
	template := "# 生成规则\n\n本次任务：{{task}}\n\n只说主播话术。\n\n历史：{{history}}"
	a := renderStablePrefixTemplate(template, "{{task}}", "卖点一 × 角度一", "{{history}}", "旧段一")
	b := renderStablePrefixTemplate(template, "{{task}}", "卖点二 × 角度二", "{{history}}", "旧段二")
	prefix := "# 生成规则\n\n只说主播话术。\n\n"
	if !strings.HasPrefix(a, prefix) || !strings.HasPrefix(b, prefix) {
		t.Fatal("dynamic data entered stable prefix")
	}
	if !strings.Contains(a, "卖点一 × 角度一") || !strings.Contains(b, "旧段二") || strings.Contains(a, "{{") {
		t.Fatal("lost bindings")
	}
}
