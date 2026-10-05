# Anchor Style Plugins

主播风格插件只扩展“怎么说”，不承载商品事实、互动内容策略、打断策略或审核绕过。

开发入口：

1. 阅读 `docs/主播风格插件开发规范.md`；
2. 复制 `self-correction`（声明式）或 `numeric-repair`（可信执行器）目录；
3. 按 `shared/schemas` 中的 JSON Schema 编写 `plugin.json` 和实例；
4. 使用 `management-service/pkg/styleplugin` 做 Go 侧校验和注册；
5. 运行 `go test ./pkg/styleplugin ./internal/styleplugins/...`。

仓库内置示例：

- `self-correction`：自然改口；
- `natural-hesitation`：低频口结与犹豫停顿；
- `natural-reduplication`：低频叠词或轻重复；
- `thinking-tempo`：思考时的局部分句与慢节奏；
- `numeric-repair`：可信计算后产生原子纠错单元。

前四类声明式能力按时间扩写引擎的口播单元应用：同一单元不叠加多个偶发效果，并尽量避开相邻单元。`thinking-tempo` 当前控制文字分句与停顿；如果要精确改变局部声音速度，必须另接分段 TTS 速率执行链，不能用提示词冒充已完成声音变速。

禁止在插件目录中放置并动态执行 DLL、SO、脚本或任意二进制。需要强保证的插件必须在 Go 服务中注册可信执行器。
