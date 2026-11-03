package httpapi

import "testing"

func TestAtomicFactsScopeValidationRejectsMalformedExplicitPrefix(t *testing.T) {
	malformed := `- [用户指定商品=["1号链接","2号链接"] 1号链接比2号链接多7道菜`
	if atomicFactsScopeValid(malformed) {
		t.Fatal("malformed multi-link scope accepted")
	}
	if !atomicFactsScopeValid(`- [用户指定商品=["1号链接","2号链接"]；variant/对比] 1号链接比2号链接多7道菜`) {
		t.Fatal("valid multi-link scope rejected")
	}
	if !atomicFactsScopeValid(`- [用户指定公共；other/说明] 公共事实`) {
		t.Fatal("valid common scope rejected")
	}
}
