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
