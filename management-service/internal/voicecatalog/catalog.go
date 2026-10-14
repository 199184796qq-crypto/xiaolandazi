package voicecatalog

import "strings"

const (
	SystemTTSModel = "qwen-audio-3.0-tts-plus"
	Qwen3TTSModel  = "qwen3-tts-flash"
	DefaultVoiceID = "longanlingxin"
)

type Voice struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Gender      string   `json:"gender"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Model       string   `json:"model"`
	Provider    string   `json:"provider,omitempty"`
}

// Tencent's published realtime premium tier. Super-natural/large-model voices
// have separate pricing and must not be silently substituted into this list.
var tencentPremium = []Voice{
	{ID: "101030", Name: "智柯", Gender: "男声", Description: "中文通用男声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101054", Name: "智友", Gender: "男声", Description: "中文通用男声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101004", Name: "智云", Gender: "男声", Description: "中文通用男声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101055", Name: "智付", Gender: "女声", Description: "中文通用女声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101027", Name: "智梅", Gender: "女声", Description: "中文通用女声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101026", Name: "智希", Gender: "女声", Description: "中文通用女声", Tags: []string{"精品", "通用"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101011", Name: "智燕", Gender: "女声", Description: "中文新闻女声", Tags: []string{"精品", "新闻"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101013", Name: "智辉", Gender: "男声", Description: "中文新闻男声", Tags: []string{"精品", "新闻"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101021", Name: "智瑞", Gender: "男声", Description: "中文新闻男声", Tags: []string{"精品", "新闻"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101001", Name: "智瑜", Gender: "女声", Description: "中文情感女声", Tags: []string{"精品", "情感"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101015", Name: "智萌", Gender: "男声", Description: "中文男童声", Tags: []string{"精品", "童声"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101016", Name: "智甜", Gender: "女声", Description: "中文女童声", Tags: []string{"精品", "童声"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101019", Name: "智彤", Gender: "女声", Description: "粤语女声", Tags: []string{"精品", "粤语"}, Model: "tencent-premium", Provider: "tencent_tts"},
	{ID: "101050", Name: "WeJack", Gender: "男声", Description: "英文男声", Tags: []string{"精品", "英文"}, Model: "tencent-premium", Provider: "tencent_tts"},
}

func TencentPremium() []Voice {
	items := make([]Voice, len(tencentPremium))
	copy(items, tencentPremium)
	return items
}

var official = []Voice{
	{ID: "longanlingxin", Name: "龙安灵心", Gender: "女声", Description: "知心温暖、自然亲切", Tags: []string{"温暖", "亲切", "直播默认"}, Model: SystemTTSModel},
	{ID: "longanlufeng", Name: "龙安鲁风", Gender: "男声", Description: "明亮开朗、自然有活力", Tags: []string{"明亮", "活力"}, Model: SystemTTSModel},
	{ID: "Cherry", Name: "芊悦", Gender: "女声", Description: "阳光积极、亲切自然", Tags: []string{"自然", "亲切", "活力"}, Model: Qwen3TTSModel},
	{ID: "Serena", Name: "苏瑶", Gender: "女声", Description: "温柔、柔和、亲近", Tags: []string{"温柔", "柔和"}, Model: Qwen3TTSModel},
	{ID: "Ethan", Name: "晨煦", Gender: "男声", Description: "阳光、温暖、有活力", Tags: []string{"温暖", "活力"}, Model: Qwen3TTSModel},
	{ID: "Chelsie", Name: "千雪", Gender: "女声", Description: "年轻、轻快、二次元风格", Tags: []string{"年轻", "轻快"}, Model: Qwen3TTSModel},
	{ID: "Momo", Name: "茉兔", Gender: "女声", Description: "活泼、俏皮、有互动感", Tags: []string{"活泼", "俏皮"}, Model: Qwen3TTSModel},
}

func Official() []Voice {
	items := make([]Voice, len(official))
	copy(items, official)
	return items
}

func Find(id string) (Voice, bool) {
	id = strings.TrimSpace(id)
	for _, item := range official {
		if item.ID == id {
			return item, true
		}
	}
	for _, item := range tencentPremium {
		if item.ID == id {
			return item, true
		}
	}
	return Voice{}, false
}

func Default() Voice {
	if voice, ok := Find(DefaultVoiceID); ok {
		return voice
	}
	if len(official) > 0 {
		return official[0]
	}
	return Voice{}
}
