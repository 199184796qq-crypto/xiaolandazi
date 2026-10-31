package interactionplan

import (
	"context"
	"fmt"
	"strings"
)

// BehaviorBody accepts only program-owned identifiers, never arbitrary template
// text from a request. Counts are distinct viewers, not verified paid orders.
func BehaviorBody(kind, template string, count int) (string, error) {
	if count < 1 || count > 8192 {
		return "", fmt.Errorf("行为播报人数无效")
	}
	switch {
	case kind == "like" && template == "like_v1":
		return "谢谢点赞支持。", nil
	case kind == "follow" && template == "follow_v1":
		return "谢谢关注，欢迎常来。", nil
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
