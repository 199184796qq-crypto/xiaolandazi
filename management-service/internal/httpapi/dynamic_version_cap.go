package httpapi

import (
	"log"

	"livecompanion/management/internal/model"
)

// 动态版本上限：按整轮内容时长算，而不是固定 6 个。
//
// 目标：同一条音频至少隔 2 小时才重复；片段越少、一轮越短，版本就越多。
// 三层护栏：下限 3、上限 20、单音色总数 80（成本熔断）。
const (
	scriptVersionTargetGapMin = 120
	scriptVersionCapMin       = 3
	scriptVersionCapMax       = 20
	scriptVersionTotalMax     = 80
	scriptSpeechRunesPerMin   = 250
)

// scriptVersionCapFromUnits 按整轮时长推算版本上限。
func scriptVersionCapFromUnits(units []volcengineScriptUnit) int {
	total := 0
	pieces := 0
	for _, unit := range units {
		chars := hanCount(unit.Text)
		if chars == 0 {
			continue
		}
		total += chars
		pieces++
	}
	if pieces == 0 || total == 0 {
		return scriptVersionCapMin
	}
	roundMin := float64(total) / scriptSpeechRunesPerMin
	want := int(float64(scriptVersionTargetGapMin)/roundMin + 0.999)
	if want > scriptVersionCapMax {
		want = scriptVersionCapMax
	}
	if want < scriptVersionCapMin {
		want = scriptVersionCapMin
	}
	if limit := max(1, scriptVersionTotalMax/pieces); want > limit {
		want = limit
	}
	return want
}

// rememberScriptVersionCap 开播时算一次并缓存，供后台补版本使用。
func rememberScriptVersionCap(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) {
	cap := scriptVersionCapFromUnits(units)
	log.Printf("script version cap plan=%d pieces=%d chars=%d cap=%d", v.PlanID, len(units), totalUnitRunes(units), cap)
}

// cachedScriptVersionCap 读开播时算出的上限；没有就退回 6。
func cachedScriptVersionCap(v model.LiveAgentPlanVersion) int {
	units, err := scriptFirstUnits(v, "")
	if err != nil || len(units) == 0 {
		return 6
	}
	return scriptVersionCapFromUnits(units)
}

func totalUnitRunes(units []volcengineScriptUnit) int {
	total := 0
	for _, unit := range units {
		total += hanCount(unit.Text)
	}
	return total
}
