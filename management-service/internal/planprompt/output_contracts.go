package planprompt

import (
	"regexp"
	"strings"
)

// 三份提示词各自的输出契约。
//
// 契约由系统追加，客户在同名段落里写的格式说明会被丢弃后替换成本契约。
// 这样字段名、顺序、格式不用再让客户维护，客户只写业务要求。

// RewriteOutputContract 是话术改写契约：三个键值对，程序按键名解析。
const RewriteOutputContract = `【输出格式（系统保留，不可删除或修改）】
按顺序输出三个键值对，键名必须完全一致，每个键值对独占一行：
话术骨架：本次实际采用的讲述安排，各阶段用“→”连接
一句话上下文：用一句话概括本次的切入角度、主要讲述内容和顺序以及结尾落点
话术：完整的改写后口播正文

不得使用 JSON、花括号或 markdown 代码围栏；
不得输出这三个字段以外的任何内容。`

// SpeechOutputContract 是直播话术契约：直接给可播正文。
const SpeechOutputContract = `【输出格式（系统保留，不可删除或修改）】
一行一个字段，用中文冒号分开。
不要 JSON、不要花括号、不要引号、不要代码块、不要任何解释。

字段只能是下面两个，键名不许自创：
话术要求：这一段的写法要求，讲哪些点、带不带价格、几句话说完
话术：主播要说的口播正文

硬性要求：
1. 键名只能用上面这两个，不许自创。
2. 话术必须放在最后一行且正文内不换行；后面禁止追加说明、字段或其它文字。
3. 话术里只放主播要说的话，不加旁白、动作括号、解释说明，也不复述写作要求。
4. 话术要求只写这一段怎么讲，不写商品参数、不写价格数字，那些放在话术里。
5. 口播符合口语断句，适合直接 TTS 朗读，停顿自然。`

// The five content fields are shared by single and multi-person requests.
// Speaker/task identities, priority bands and expiry remain program-owned.
const interactionFiveFields = `意图：对当前诉求的一句理解
类型：提问、请求、下单意向、催单、投诉、闲聊、主动互动、刷屏之一
歧义：是或否
澄清句：需要确认问题时可直接口播的一句反问，否则留空
话术：可直接口播的回答正文，否则留空
`

const interactionFiveFieldRules = `每项从意图开始；意图只写一条。只返回上面五个字段，不加分析或额外字段。
话术和澄清句只填一个，另一个留空；空字段先写，有值的可播字段放在本项最后。
可以回答时：歧义写否，澄清句留空，以话术收尾。
确实需要确认问题时：歧义写是，话术留空，以澄清句收尾；把待确认的选项直接说在澄清句里。
主动互动或刷屏无需逐人口播时，话术和澄清句均留空。
可播内容不加昵称或称呼；正文允许正常分行，不把其它人的内容写进本项。`

// InteractionOutputContract is the single-person wire format.
const InteractionOutputContract = `【输出格式（系统保留，不可删除或修改）】
本次只有一项，不需要编号。使用中文键值字段，不用JSON、表格或额外解释。
` + interactionFiveFields + interactionFiveFieldRules

// BatchInteractionOutputContract adds only the opaque per-request item label.
const BatchInteractionOutputContract = `【输出格式（系统保留，不可删除或修改）】
每人独立返回一组，使用中文键值字段，不用JSON、表格或额外解释。
每组前加输入中的item_id，编号只用于对应本批任务，原样返回，不能合并或补造身份。
编号：U01
` + interactionFiveFields + interactionFiveFieldRules + `
每组以相同编号收尾：
结束：U01
下一人的编号另起一行；意图是内容开始，最后的话术或澄清句是内容收尾；结束字段只用于核对本组编号。`

var stepHeading = regexp.MustCompile(`^第[一二三四五六七八九十百0-9]+步[：:]`)

func headingTitle(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}
	if strings.HasPrefix(line, "【") && strings.HasSuffix(line, "】") {
		line = strings.TrimSuffix(strings.TrimPrefix(line, "【"), "】")
	}
	return strings.TrimRight(line, "：:")
}

// StripOutputFormatSection 去掉客户自己写的「输出格式」段落。
//
// 从命中行开始删到下一个以【开头的标题之前；后面没有标题就删到结尾。
// 「输出要求」不受影响。
func StripOutputFormatSection(text string) string {
	return stripFormatSections(text, isOutputFormatHeading)
}

// StripSystemOutputContract is for editor readback. Old customer rules,
// including their historical format descriptions, remain their original text.
func StripSystemOutputContract(text string) string {
	return stripFormatSections(text, func(line string) bool {
		return headingTitle(line) == "输出格式（系统保留，不可删除或修改）"
	})
}

func stripFormatSections(text string, match func(string) bool) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	var kept []string
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match(trimmed) {
			skipping = true
			continue
		}
		if skipping {
			if (strings.HasPrefix(trimmed, "【") && strings.HasSuffix(trimmed, "】")) || strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "##") || stepHeading.MatchString(trimmed) {
				skipping = false
			} else {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// WithOutputContract 拼接业务要求与系统契约。
func WithOutputContract(rules, contract string) string {
	body := StripOutputFormatSection(rules)
	if strings.TrimSpace(body) != "" {
		body += "\n\n"
	}
	return body + contract
}

func isOutputFormatHeading(line string) bool {
	switch headingTitle(line) {
	case "输出格式", "输出格式要求", "输出格式（系统保留，不可删除或修改）":
		return true
	}
	return false
}

// WithOutputContractIfPresent 只在客户自己写过「输出格式」时接管。
//
// 话术与互动这两份提示词历史数据没有「输出格式」段，保持原样可以避免
// 空内容被契约填充、也避免已经在用的方案下次保存时被整体改写。
func WithOutputContractIfPresent(rules, contract string) string {
	stripped := StripOutputFormatSection(rules)
	if stripped == strings.TrimSpace(strings.ReplaceAll(rules, "\r\n", "\n")) {
		return rules
	}
	return WithOutputContract(rules, contract)
}

// WithOutputContractUnlessEmpty 内容为空时保持为空，有内容才追加契约。
//
// 互动提示词以前没有格式段，客户删掉自己的格式说明后仍要拿到系统契约；
// 但四页全空的方案必须继续存成空，不能因为契约变成非空。
func WithOutputContractUnlessEmpty(rules, contract string) string {
	if strings.TrimSpace(StripOutputFormatSection(rules)) == "" {
		return ""
	}
	return WithOutputContract(rules, contract)
}
