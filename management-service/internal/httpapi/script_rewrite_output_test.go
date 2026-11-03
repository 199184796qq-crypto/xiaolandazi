package httpapi

import "testing"

func TestParseRewriteOutputAcceptsUnifiedScriptKey(t *testing.T) {
	skel, body := parseRewriteOutput("话术骨架：甲→乙\n一句话上下文：从价格切入，收在按需选择。\n话术：朋友们，这一条只说价格。")
	if skel != "甲→乙" || body != "朋友们，这一条只说价格。" {
		t.Fatalf("统一话术字段解析错误: %q %q", skel, body)
	}
	oldSkel, oldBody := parseRewriteOutput("话术骨架：甲\n一句话上下文：乙\n改写内容：旧字段正文。")
	if oldSkel != "甲" || oldBody != "旧字段正文。" {
		t.Fatalf("旧字段应继续兼容: %q %q", oldSkel, oldBody)
	}
}