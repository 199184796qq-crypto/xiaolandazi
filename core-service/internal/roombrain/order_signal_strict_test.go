package roombrain

import "testing"

func TestCompletedOrderStrictProgramRules(t *testing.T) {
	for _, v := range []string{"已拍", "我已下单", "下单了", "下好单啦", "下单成功", "我买了两桶", "已经购买", "刚刚拍下了", "买好啦", "我已付款", "支付成功", "我拍了不知道多久到", "已经买了不用介绍了", "已 买", "我买了不要今天发"} {
		t.Run("yes/"+v, func(t *testing.T) {
			if !IsOrderSignalText(v) {
				t.Fatal("missed completed claim", v)
			}
		})
	}
	for _, v := range []string{"拍", "买", "冲了", "安排了", "支持了", "我下单", "怎么买", "怎么下单", "我不买了", "不拍了", "不下单", "我还没下单", "未付款", "买了会不会不好吃", "你买了吗", "已拍?", "有人拍了", "上次买了", "昨天拍了", "准备买了", "如果拍了", "买了已经退了", "已拍照", "拍了视频", "截图拍好了", "我朋友买了", "不是我买了", "购买失败", "已经买不起", "已经拍不了", "买了有破损我要投诉", "买了不好吃"} {
		t.Run("no/"+v, func(t *testing.T) {
			if IsOrderSignalText(v) {
				t.Fatal("false order claim", v)
			}
		})
	}
}
