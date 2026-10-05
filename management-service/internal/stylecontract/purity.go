package stylecontract

import (
	"regexp"
	"strings"

	"livecompanion/management/internal/model"
)

// PurityCategory explains why a phrase cannot be part of the reusable voice
// layer. These are semantic classes, not merchant-specific forbidden words.
type PurityCategory string

const (
	PurityBusinessFact    PurityCategory = "business_fact"
	PurityBusinessAction  PurityCategory = "business_action"
	PuritySocialProof     PurityCategory = "social_proof"
	PurityPromise         PurityCategory = "promise_or_outcome"
	PuritySyntheticState  PurityCategory = "synthetic_runtime_state"
	PurityConversionLogic PurityCategory = "conversion_logic"
	PurityContentExample  PurityCategory = "source_content_example"
)

type PurityIssue struct {
	Location string         `json:"location"`
	Category PurityCategory `json:"category"`
	Text     string         `json:"text"`
	Reason   string         `json:"reason"`
}

type PurityReport struct {
	Passed bool          `json:"passed"`
	Issues []PurityIssue `json:"issues"`
}

var (
	styleBusinessFactPattern    = regexp.MustCompile(`(?:价格|售价|优惠|活动|折扣|赠品|规格|重量|净含量|产地|库存|销量|快递|物流|时效|发货|售后|退款|退货|功效|适用人群|保质期|链接|商品)`)
	styleBusinessActionPattern  = regexp.MustCompile(`(?:下单|购买|拍下|开拍|抢购|催拍|催单|买好|拍到|付款|成交|装车|打包|试吃|开盖|申请退货)`)
	styleSocialProofPattern     = regexp.MustCompile(`(?:老粉|新粉跟|跟着老|好评|回购|口碑|大家都|别人都|人家买过|参照群体)`)
	stylePromisePattern         = regexp.MustCompile(`(?:一定不会让.{0,6}失望|不会让.{0,6}失望|保证满意|绝对满意|放心购买|买得安心|吃得放心|假一赔|必然有效)`)
	styleSyntheticStatePattern  = regexp.MustCompile(`(?:假装|模拟).{0,8}(?:弹幕|后台|库存|订单|发货)|(?:我看一下|看一下后台).{0,8}(?:库存|订单|数量)|(?:弹幕|后台).{0,8}(?:说|显示|备注)`)
	styleConversionLogicPattern = regexp.MustCompile(`(?:紧迫理由|虚假紧迫|逼单|促单|催促(?:句|模板|购买)?|行动指令|行动句|购买状态|观众状态|获得回应|转化链路|劝说段|成交路径|倒计时|即将售罄|卖完|断货)`)
	styleContentExamplePattern  = regexp.MustCompile(`(?:例如|比如|如)[：:]?[“"][^<>”"]{2,}[”"]`)
)

func classifyStyleContent(text string) (PurityCategory, string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	if commercial.MatchString(text) || styleBusinessFactPattern.MatchString(text) {
		return PurityBusinessFact, "包含商品、交易、履约或经营事实", true
	}
	if styleBusinessActionPattern.MatchString(text) {
		return PurityBusinessAction, "包含购买或履约动作，不是语言风格", true
	}
	if styleSocialProofPattern.MatchString(text) {
		return PuritySocialProof, "包含社会证明或人群跟随策略", true
	}
	if stylePromisePattern.MatchString(text) {
		return PurityPromise, "包含结果保证或安抚承诺", true
	}
	if styleSyntheticStatePattern.MatchString(text) {
		return PuritySyntheticState, "包含假定的后台、弹幕或执行状态", true
	}
	if styleConversionLogicPattern.MatchString(text) {
		return PurityConversionLogic, "包含促单或转化内容逻辑", true
	}
	if styleContentExamplePattern.MatchString(text) {
		return PurityContentExample, "包含样本内容示例，规则只能使用<内容>占位符", true
	}
	return "", "", false
}

// AssessPurity is deliberately independent of the source product. It checks
// whether the model placed business semantics inside the reusable voice layer.
// Evidence quotes are excluded because they are never rendered downstream.
func AssessPurity(profile model.LiveAgentPlanAnchorStyleProfile) PurityReport {
	report := PurityReport{Passed: true, Issues: []PurityIssue{}}
	add := func(location, text string) {
		if category, reason, risky := classifyStyleContent(text); risky {
			report.Passed = false
			report.Issues = append(report.Issues, PurityIssue{Location: location, Category: category, Text: strings.TrimSpace(text), Reason: reason})
		}
	}
	if profile.Delivery != nil {
		for index, rule := range profile.Delivery.Instructions {
			add("delivery_spec.instructions["+itoa(index)+"]", rule)
		}
		for index, habit := range profile.Delivery.Habits {
			// Addresses, self-address, particles and connectors are lexical form.
			// A catchphrase may carry a hidden claim or business action and must be
			// assessed semantically before it can enter the reusable vocabulary.
			if habit.Kind == "catchphrase" {
				add("delivery_spec.literal_habits["+itoa(index)+"]", habit.Text)
			}
			add("delivery_spec.literal_habits["+itoa(index)+"].when", habit.When)
			add("delivery_spec.literal_habits["+itoa(index)+"].avoid", habit.Avoid)
		}
	}
	for index, rule := range profile.ReusableRules {
		add("reusable_rules["+itoa(index)+"]", rule)
	}
	return report
}

func pureStyleText(text string) bool {
	_, _, risky := classifyStyleContent(text)
	return strings.TrimSpace(text) != "" && !risky
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}
