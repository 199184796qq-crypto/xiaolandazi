import test from 'node:test'
import assert from 'node:assert/strict'
import { parseAnchorStyleDisplay, styleDisplayLabel, styleInlineTokens } from '../src/anchorStyleDisplay.ts'

const returned = `【口语风格】
1. 语气词：高频使用句尾助词“哟”、“啊”、“嘛”、“不”。
2. 称呼：主力称呼为“哥哥姐姐们”、“叔叔阿姨们”、“新粉丝/老粉丝”、“老乡”。**严禁使用**：“您”。
3. 自指：常用“我们家”代表店铺和产品。
4. 句式：短句为主，节奏快。
5. 数字：习惯计算节省金额，重量9斤2两、价格69块9。
6. 方言/口头禅：带有明显的西南官话色彩。
7. 回环：禁止每段都以“哥哥姐姐们”开头。

【情绪】
基调：高亢、急切、热情、接地气。
依据：通过语气词营造热闹氛围。

【禁用词】
1. 禁用称呼：严禁使用“亲亲”。
2. 禁用书面语：要说“划算”。
3. 禁用过渡词：减少使用“首先”。

【范文锚定】
*   *结构示范1（算账逼单）*：“[商品名]平时卖[高价]一斤。”
*   *结构示范2（信任背书+售后）*：“我们[品牌/产地]做了[年限]年。”

【全局规则】
1. **卖点轮换**：每次侧重点不同。
2. **逼单落点**：紧跟价格优势之后。
3. **情绪保持**：保持高昂语调。
4. **拒绝说明书**：不要罗列成分表。
5. **回环防僵**：结尾的行动号召要变化。`

test('严格按返回的【大板块】和编号小点展示，不合并成预设四大类', () => {
  const sections = parseAnchorStyleDisplay(returned)
  assert.deepEqual(sections.map(x => x.title), ['口语风格', '情绪', '禁用词', '范文锚定', '全局规则'])
  assert.deepEqual(sections.map(x => x.points.length), [7, 1, 3, 2, 5])
  assert.deepEqual(sections[0]!.points.map(x => x.marker), ['1.', '2.', '3.', '4.', '5.', '6.', '7.'])
  assert.ok(sections[0]!.points[1]!.text.includes('**严禁使用**：“您”'))
  assert.ok(sections[3]!.points[1]!.text.includes('[品牌/产地]做了[年限]年'))
  assert.equal(styleDisplayLabel(sections[4]!.points[0]!.text).label, '**卖点轮换**')
})

test('支持大标题同一行带正文、中文编号标点和多行小点', () => {
  const sections = parseAnchorStyleDisplay('【情绪】基调：热情\r\n【句式】\r\n1.短句为主，\r\n继续原来的说明。\r\n2、长短交替。\r\n3．保持节奏。')
  assert.equal(sections[0]!.points[0]!.text, '基调：热情')
  assert.equal(sections[1]!.points[0]!.text, '短句为主，\n继续原来的说明。')
  assert.deepEqual(sections[1]!.points.map(x => x.marker), ['1.', '2、', '3．'])
})

test('非标准原文、额外板块、重复板块、空板块均不静默丢弃', () => {
  const sections = parseAnchorStyleDisplay('原文前言\n【额外说明】\n没有编号的说明\n【额外说明】\n【口头习惯】\n1. 其他内容')
  assert.deepEqual(sections.map(x => x.title), ['', '额外说明', '额外说明', '口头习惯'])
  assert.equal(sections[0]!.points[0]!.text, '原文前言')
  assert.equal(sections[1]!.points[0]!.text, '没有编号的说明')
  assert.deepEqual(sections[2]!.points, [])
  assert.deepEqual(parseAnchorStyleDisplay(' \n\t'), [])
})

test('价格小数不是编号，句中【占位符】不是大标题', () => {
  const sections = parseAnchorStyleDisplay('【数字】\n69.9元\n9.2斤\n1. 价格：【现价】，规格【规格】。')
  assert.equal(sections.length, 1)
  assert.equal(sections[0]!.points[0]!.text, '69.9元\n9.2斤')
  assert.equal(sections[0]!.points[0]!.marker, '')
  assert.equal(sections[0]!.points[1]!.text, '价格：【现价】，规格【规格】。')
})

test('标签只拆开排版，不改冒号、内容或原文', () => {
  const source = '语气词：原文 **严禁使用** 这个词。'
  const split = styleDisplayLabel(source)
  assert.equal(split.label + split.separator + split.body, source)
  assert.equal(styleDisplayLabel('短句为主，没有冒号。').body, '短句为主，没有冒号。')
  const long = '这是一整句话并不代表一个很短的小点标签而是一整段原文：继续说明。'
  assert.equal(styleDisplayLabel(long).body, long)
})

test('Markdown强调安全地作为文本节点展示，不生成或执行HTML', () => {
  const tokens = styleInlineTokens('不要 **严禁使用**，*范文示例* 与 `参数`。<script>alert(1)</script>')
  assert.deepEqual(tokens.filter(t => t.kind !== 'text').map(t => t.kind), ['strong', 'em', 'code'])
  assert.equal(tokens.map(t => t.text).join(''), '不要 严禁使用，范文示例 与 参数。<script>alert(1)</script>')
  assert.equal(styleInlineTokens('普通原文')[0]!.text, '普通原文')
  assert.deepEqual(styleInlineTokens(''), [])
})

test('大分段和小点内容由当前输入决定，不固定主播词库', () => {
  const a = parseAnchorStyleDisplay('【开场习惯】\n1. 称呼：各位老师。')
  const b = parseAnchorStyleDisplay('【节奏】\n1. 停顿：每句之间留白。\n2. 语气：平静。')
  assert.equal(a[0]!.title, '开场习惯')
  assert.ok(a[0]!.points[0]!.text.includes('各位老师'))
  assert.equal(b[0]!.title, '节奏')
  assert.equal(b[0]!.points.length, 2)
  assert.equal(returned.includes('【范文锚定】'), true)
})
