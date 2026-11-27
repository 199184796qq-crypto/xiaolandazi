package interactionplan

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
)

// 点赞/关注的播报文案：每个模板 ID 下多条变体，每次随机取一条，避免同场重复同一句。
var likeBodies = []string{
	"谢谢点赞支持。",
	"感谢家人们的赞，收到啦。",
	"赞都看到了，谢谢捧场。",
	"谢谢点赞，喜欢的点个赞。",
	"感谢点赞，手别停。",
	"点赞的都记着呢，谢谢。",
	"谢谢大家的小红心。",
	"感谢支持，赞我都收下了。",
	"谢谢点赞，接着给大家讲。",
	"收到的赞不少，谢谢家人们。",
}

var followBodies = []string{
	"谢谢关注，欢迎常来。",
	"关注了就别走，一会儿还有好货。",
	"感谢关注，明天同一时间还在。",
	"谢谢关注，新朋友先听两句。",
	"关注一下，后面讲得更细。",
	"谢谢关注，回头不迷路。",
	"欢迎新关注的家人们。",
	"感谢关注，有需要随时问。",
	"关注了就是自己人。",
	"谢谢关注，接着往下讲。",
}

func pickBody(list []string) string {
	if len(list) == 0 {
		return ""
	}
	if len(list) == 1 {
		return list[0]
	}
	return list[rand.Intn(len(list))]
}

// BehaviorBody accepts only program-owned identifiers, never arbitrary template
// text from a request. Counts are distinct viewers, not verified paid orders.
func BehaviorBody(kind, template string, count int) (string, error) {
	if count < 1 || count > 8192 {
		return "", fmt.Errorf("行为播报人数无效")
	}
	switch {
	case kind == "like" && template == "like_v1":
		return pickBody(likeBodies), nil
	case kind == "follow" && template == "follow_v1":
		return pickBody(followBodies), nil
	case kind == "order" && template == "order_single_v1" && count == 1:
		return "谢谢下单支持。", nil
	case kind == "order" && template == "order_count_v1":
		return fmt.Sprintf("刚刚又有%d位朋友说已经下单，感谢支持。", count), nil
	default:
		return "", fmt.Errorf("行为模板与事件不匹配")
	}
}

func generateBehaviorBatch(ctx context.Context, s Snapshot, items []BatchItem) (BatchResult, bool, error) {
	has := false
	for _, in := range items {
		has = has || in.Template != ""
	}
	if !has {
		return BatchResult{}, false, nil
	}
	if err := ctx.Err(); err != nil {
		return BatchResult{}, true, err
	}
	if strings.TrimSpace(s.Prompt) == "" || strings.TrimSpace(s.P0) == "" || strings.TrimSpace(s.P0Markdown) == "" {
		return BatchResult{}, true, fmt.Errorf("行为播报方案或完整P0不可用")
	}
	result := BatchResult{Items: make([]ItemResult, len(items))}
	for i, in := range items {
		body, err := BehaviorBody(in.Kind, in.Template, in.EventCount)
		if err != nil {
			return BatchResult{}, true, err
		}
		if err = Validate(s, body); err != nil {
			return BatchResult{}, true, err
		}
		result.Items[i] = ItemResult{ID: in.ID, Body: body, Intent: Intent{Relevant: true, Type: "闲聊", Priority: "普通", Meaning: "程序确认的行为统计播报"}}
	}
	return result, true, nil
}
