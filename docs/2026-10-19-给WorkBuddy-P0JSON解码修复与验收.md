# P0 JSON 解码修复与验收（给 WorkBuddy）

状态：Codex 已完成只读定位和仓库外验证；请求 WorkBuddy 交接重叠源码，或由 WorkBuddy 按本文修复。尚未修改业务代码、部署、重启或调用模型/TTS。

## 已证实的原因

`E:\直播伴播\configs\prompts\anchor-style\compliance\L1\p0-generation.json` 本身完整：8 条规则、119 个禁词条目、8 条改说。`P0Rule` 的 `红线`、`不提`、`改说` 是未导出字段，Go JSON 解码忽略这些字段，json 标签不能使字段导出。外层 Rules 数组仍有 8 项，现有只检查长度的代码因此误判成功，并把空字段缓存到进程内。

同一份当前 JSON 的仓库外复现：

```text
原字段：json_error=<nil> rules=8 decoded_terms=0 decoded_rewrites=0 first_field_exported=false
导出字段：rules=8 decoded_terms=119 decoded_rewrites=8 all_rules_valid=true
```

证据：`E:\直播伴播-local\last-live-no-audio-20261019\p0-json-readonly-probe.go`、`E:\直播伴播-local\p0-json-export-fix-20261019\exported-fields-probe.go`。这些是独立复现，不冒充生产二进制已经修复。

## 最小修复

1. 修改 `E:\直播伴播\management-service\internal\speechbase\render.go`：使用导出字段，保留中文 JSON 键。

```go
type P0Rule struct {
    Redline   string   `json:"红线"`
    Forbidden []string `json:"不提"`
    Rewrite   string   `json:"改说"`
}
```

同步更新 `RenderP0ForbiddenWordList`、`RenderP0RewriteGuidance` 中的字段引用，以及实际存在的结构体字面量/测试。不能只改 struct 而遗漏渲染调用。

2. 修改 `E:\直播伴播\management-service\internal\httpapi\text_content_views.go` 的加载校验：除 rules 非空外，每条红线名、禁词数组、改说均须非空，数组内不能有空白禁词。报错要带规则序号和字段名。任何失败均不写 generationP0Rules 缓存，不能静默按空词表继续生成。

3. 本轮可保留进程缓存策略，不扩成热加载改造；修复发布必须重启 Management，重新加载规则。仅修改 JSON 或把文件再上传一次不能修复未导出字段。

4. 核对 `E:\直播伴播\management-service\internal\httpapi\speech_library.go` 的规则身份：修复改变了实际生成约束，不能仅因旧 JSON 文件字节没变就继续复用旧约束下的正文/声音。发布前确认修复后的生成规则契约进入缓存身份；必要时增加明确契约版本和生成 P0 内容指纹，只隔离旧条目，不删除客户缓存或资料。

## 验证

- 用真实 JSON 验证解析后的 8 条规则、119 个条目、8 条改说均保留；统计值用于当前文件验收，生产代码不能写死数量。
- 验证渲染的禁词和改说字符串非空，并检查每条规则实际进入对应字段；不能只测试解析数组长度。
- 验证缺字段、空数组、空白字符串、畸形 JSON 都返回可定位错误，失败不会污染缓存。
- 验证主线组装后的真实请求中“不提红线词”和“改写”确有内容；不得只凭文件存在或 healthz 成功验收。
- 先跑不付费的 speechbase/httpapi 相关回归，再构建管理程序。确切源码和并行改动交接后才能发布，不拿旧的干净 c665542 覆盖 WorkBuddy 现有功能。

## 发布边界

发布前重新核对服务器实际二进制/母版及共享发布锁、直播空闲状态，保留 WorkBuddy 线上改动；仅更新本次已整合验证的 Management，重启后检查规则加载和模型请求组装。不得顺带改变 skip_review、商品/公共权重、四层结构、客户事实或声音配置。真实模型/TTS 验收需沿用用户明确授权，本文未执行付费调用。

## 同文件交接

上述 render.go、text_content_views.go 已有 WorkBuddy 未提交差异，按 AGENTS.md 第 62–67 行先交接再编辑。Codex 本次仅新增本文和登记，不覆盖、撤销或代提交对方源码。请交出方注明已完成/未完成、未提交差异和验证情况，并明确“已交接”；接手方重新读取最新文件和 diff。
