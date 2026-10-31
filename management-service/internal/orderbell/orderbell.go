// Package orderbell 判定一次互动算不算真下单，并给出打铃所需的决定。
//
// 只有观众原话通过与Core一致的完成短语核对才确认下单，模型标签不能
// 直接证明或否决下单。已验证的程序行为模板另走DecideConfirmed。
//   - 下单、催单、闲聊等标签均核对原话；
//   - 同一用户同一口味 5 分钟内只打一次铃。
package orderbell

import (
	"strings"
	"sync"
	"time"
	"unicode"
)

// Kind 是可识别的下单类型。
const (
	TypeOrder   = "下单意向"
	TypeUrging  = "催单"
	TypeComplai = "投诉"
)

// DedupeWindow 是同一用户同一口味的去重窗口。
const DedupeWindow = 5 * time.Minute

// Decision 是一次下单判定的结果。
type Decision struct {
	IsOrder bool   // 是否真下单
	Ring    bool   // 本次是否打铃
	Flavor  string // 从原话里取到的口味，取不到为空
	Phrase  string // 程序短语，打铃后播这一句
}

// orderWords 是“已经下单”的原话证据。类型判成催单也照样要看原话。
var orderWords = []string{
	"已下单", "已经下单", "下单了", "下好单", "下单成功",
	"已拍", "已经拍", "拍了", "拍下了", "拍好了", "拍好啦", "拍好咯",
	"已买", "已经买", "买了", "买好了", "买好啦", "买好咯", "买下了",
	"已购买", "已经购买", "购买了", "购买成功",
	"已付款", "已经付款", "付款了", "付好了", "已支付", "已经支付", "支付成功",
	"刚付完钱", "刚付完款", "刚结完账", "付完钱了", "付完款了", "结完账了", "已结账", "已经结账", "结账了",
}

// negateWords 表示原话并不是“已经下单”，不能据此打铃。
var negateWords = []string{
	"不买", "不拍", "不下单", "不付款", "不想买", "不想拍", "不想下单",
	"不能买", "不能拍", "不会买", "不会拍", "不敢买", "不敢拍",
	"拍卖", "投诉", "售后", "破损", "坏了", "坏的", "发臭", "变质", "过期", "少发", "发错", "漏发", "难吃", "不好", "差评", "被骗", "假货", "翻车",
	"买不", "拍不", "会不会", "好不好", "是不是", "要不要", "有没有",
	"如果", "假如", "要是", "准备", "打算", "想买", "想拍", "想下单",
	"等会", "待会", "下次", "上次", "之前", "以前", "昨天", "前天", "去年",
	"买过", "拍过", "取消", "退款", "退货", "退了", "失败",
	"照片", "视频", "拍照", "截图", "你买", "你拍", "他买", "他拍", "她买", "她拍",
	"别人", "朋友买", "朋友拍", "有人", "不是我",
	"你付", "他付", "她付", "朋友付", "你结", "他结", "她结", "朋友结",
}

// Ordered 判断原话是否明确表达了“已经下单”。类型判成催单也要看这一条。
func Ordered(text string) bool {
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, strings.ToLower(text))
	if text == "" || strings.ContainsAny(text, "没未吗么?？") {
		return false
	}
	for _, word := range negateWords {
		if strings.Contains(text, word) {
			return false
		}
	}
	for _, word := range orderWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// Detect只核对原话；保留类型参数以兼容调用者，但类型本身不构成证据。
func Detect(_ string, text string) bool {
	return Ordered(text)
}

// flavorWords 是能从弹幕里认出的口味词，用于拼短语与去重。
var flavorWords = []string{"五香", "麻辣", "香辣", "原味", "微辣", "特辣"}

// Flavor 从原话里取口味，取不到返回空。
func Flavor(text string) string {
	text = strings.TrimSpace(text)
	for _, word := range flavorWords {
		if strings.Contains(text, word) {
			return word
		}
	}
	return ""
}

// Phrase 生成打铃后的程序短语，不调模型。
func Phrase(flavor, spec string) string {
	flavor = strings.TrimSpace(flavor)
	spec = strings.TrimSpace(spec)
	switch {
	case flavor != "" && spec != "":
		return "收到，" + flavor + spec + "，感谢支持。"
	case flavor != "":
		return "收到，" + flavor + "，感谢支持。"
	case spec != "":
		return "收到，" + spec + "，感谢支持。"
	}
	return "收到订单，感谢支持。"
}

// Guard 负责去重与全局打铃间隔。
type Guard struct {
	mu       sync.Mutex
	seen     map[string]time.Time
	lastRing time.Time
	now      func() time.Time
}

// NewGuard 创建一个去重器。
func NewGuard() *Guard {
	return &Guard{seen: map[string]time.Time{}, now: time.Now}
}

// RingGap 是两次打铃之间的最小间隔，避免连环响。
const RingGap = 8 * time.Second

// Decide 综合类型、原话与去重状态，给出本次是否打铃、播什么。
// key 用来区分用户（一般是 user id 或昵称）。
func (g *Guard) Decide(intentType, text, key string) Decision {
	if !Detect(intentType, text) {
		return Decision{}
	}
	return g.decideConfirmed(text, key)
}

// DecideConfirmed仅供已经核验场次、人数与标准正文的程序下单模板使用。
// 群体数量补报可能不再携带个人原话，不能用模型标签冒充这一证明。
func (g *Guard) DecideConfirmed(text, key string) Decision {
	return g.decideConfirmed(text, key)
}

func (g *Guard) decideConfirmed(text, key string) Decision {
	flavor := Flavor(text)
	if g == nil {
		return Decision{IsOrder: true, Ring: true, Flavor: flavor, Phrase: Phrase(flavor, "")}
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, at := range g.seen {
		if now.Sub(at) > DedupeWindow {
			delete(g.seen, k)
		}
	}
	dedupeKey := strings.TrimSpace(key) + "|" + flavor
	if at, ok := g.seen[dedupeKey]; ok && now.Sub(at) <= DedupeWindow {
		// 同一个人同一口味重复说，只播短语不打铃。
		return Decision{IsOrder: true, Ring: false, Flavor: flavor, Phrase: Phrase(flavor, "")}
	}
	g.seen[dedupeKey] = now
	ring := g.lastRing.IsZero() || now.Sub(g.lastRing) >= RingGap
	if ring {
		g.lastRing = now
	}
	return Decision{IsOrder: true, Ring: ring, Flavor: flavor, Phrase: Phrase(flavor, "")}
}
