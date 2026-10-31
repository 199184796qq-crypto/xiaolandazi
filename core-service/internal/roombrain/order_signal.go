package roombrain

import (
	"strings"
	"unicode"
)

// Deliberately conservative, punctuation-independent viewer claims. This is
// not an intent-model request or proof of a paid platform order.
func completedOrderClaim(text string) bool {
	v := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(text))
	if v == "" || strings.ContainsAny(v, "没未吗么?？") {
		return false
	}
	if containsAny(v,
		"不买", "不拍", "不下单", "不付款", "不想买", "不想拍", "不想下单", "不能买", "不能拍", "不会买", "不会拍", "不敢买", "不敢拍",
		"买不", "拍不", "拍卖", "投诉", "售后", "破损", "坏了", "坏的", "发臭", "变质", "过期", "少发", "发错", "漏发", "难吃", "不好", "差评", "被骗", "假货", "翻车",
		"会不会", "好不好", "是不是", "要不要", "有没有", "如果", "假如", "要是", "准备", "打算", "想买", "想拍", "想下单", "等会", "待会", "下次",
		"上次", "之前", "以前", "昨天", "前天", "去年", "买过", "拍过", "取消", "退款", "退货", "退了", "失败",
		"照片", "视频", "拍照", "截图", "你买", "你拍", "他买", "他拍", "她买", "她拍", "别人", "朋友买", "朋友拍", "有人", "不是我",
		"你付", "他付", "她付", "朋友付", "你结", "他结", "她结", "朋友结") {
		return false
	}
	return containsAny(v,
		"已下单", "已经下单", "下单了", "下好单", "下单成功",
		"已拍", "已经拍", "拍了", "拍下了", "拍好了", "拍好啦", "拍好咯",
		"已买", "已经买", "买了", "买好了", "买好啦", "买好咯", "买下了", "已购买", "已经购买", "购买了", "购买成功",
		"已付款", "已经付款", "付款了", "付好了", "已支付", "已经支付", "支付成功",
		"刚付完钱", "刚付完款", "刚结完账", "付完钱了", "付完款了", "结完账了", "已结账", "已经结账", "结账了")
}
