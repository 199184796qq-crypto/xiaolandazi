package anchorcycle

import "testing"

func TestLinkOrdinalDoesNotConfuseAddressWithProduct(t *testing.T) {
	for _, text := range []string{"总店在红花街一号开了几十年", "地址为示例路1号", "位于一号楼", "本月一号开始营业", "店址在示例巷 二号"} {
		for _, key := range []string{"1", "2"} {
			if MentionsLink(text, key) {
				t.Fatalf("address/date mistaken for product %s: %s", key, text)
			}
		}
	}
	for _, text := range []string{"一号链接是整只", "1号链接更合适", "一号给一家人吃", "1号是主推", "链接1的规格", "红花街一号，另选2号链接", "第1号链接"} {
		key := "1"
		if text == "红花街一号，另选2号链接" {
			key = "2"
		}
		if !MentionsLink(text, key) {
			t.Fatalf("real product reference lost: %s", text)
		}
	}
}
