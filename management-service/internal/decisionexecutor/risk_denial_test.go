package decisionexecutor

import "testing"

func TestRiskDenialPattern(t *testing.T) {
    if m := riskDenialPattern.FindString("没有卡喉的风险"); m == "" {
        t.Fatal("应匹配风险否定句式")
    }
    if m := riskDenialPattern.FindString("毫无安全隐患"); m == "" {
        t.Fatal("应匹配毫无安全隐患")
    }
    if m := riskDenialPattern.FindString("新鲜卫生"); m != "" {
        t.Fatal("不应误匹配正常表达，得到 " + m)
    }
}
