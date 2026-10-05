# 主播话术与实时互动模型连接

## 管理入口和范围

系统设置 → AI 与智能体 → 主播话术与互动模型。
仅平台管理员或具有系统设置权限的超级管理员可读写、测试。客户和普通运维不开放。

最多保存 30 组连接：名称、协议、HTTPS API 地址、模型 ID、API Key、启用状态、超时、输出 Token 上限、可选基础/元提示词。
保存配置与选择默认模型分开：添加一组不自动启用为默认。
默认模型为空时保留原服务器路由；选定后仅覆盖 `speech_generation`。
失败时不静默切换服务商；已有成品音频、正在进行的模型调用、风格规范和智能体配置不改动。

覆盖：主线预览、单稿重生成、审计修复、主播风格文字测试、动态局部更新、实时互动文字生成及其模拟测试。
不覆盖：智能体理解/分类/决策、风格分析、商品素材归位、语义向量、语音合成、最终审查模型。
实时 FAQ 命中旧缓存时仍使用其原版本；切换模型不强制清空缓存或改动更新周期。
额外基础提示词和每次业务请求一起发送，不替代正式事实、主播规范、业务 JSON 契约。

## 协议

| 协议 | 地址及响应 |
|---|---|
| OpenAI Chat Completions | Base URL + `/chat/completions`（也接受完整地址），Bearer Key，读取 `choices[0].message.content`；可选 `max_tokens` / `max_completion_tokens` |
| OpenAI Responses | Base URL + `/responses`，Bearer Key，读取 `output[].content[]` 的 `output_text` |
| Gemini 原生 | 版本根地址 + `/models/{model}:generateContent`，`x-goog-api-key`，读取首个 candidate 的非 thought 文本 |
| Anthropic 原生 | 版本根地址 + `/messages`，`x-api-key`、`anthropic-version`，读取 text content blocks |

模型 ID 可手动填写，服务商的品牌名不等于 API 协议。自定义服务必须符合所选协议；非这些协议需新增适配器，不能仅更换 URL。
模型 ID 也可通过“获取模型列表”下拉选择和搜索：填好协议、API 地址及密钥即可获取，无须先填写模型 ID 或保存。OpenAI 兼容协议、Gemini、Anthropic 的列表接口均有适配；Gemini 仅列出支持 generateContent 的模型。服务商不支持列表接口时保留手填。
模型列表只发送 metadata GET，由服务器附加认证头；共用生成接口的公网 DNS/重定向保护。最多 20 秒、10 页、2000 个模型及每页 4 MiB；分页限额会标记仅返回部分。列表不持久化、不保存配置、不选择默认模型，调用权限仍需要文字测试验证。
管理接口：`POST /api/v1/system/speech-models/models`，使用当前 profile 或相同 URL/协议下的已存凭据；更换 URL/协议必须重填密钥。输入修改后前端旧列表失效，过时请求响应不会覆盖新输入。
本模块面向文字，不执行模型工具调用。业务 JSON 输出保留现有解析/审计；原生协议用对应指令或 JSON MIME 约束。

核对依据：[Gemini Generate Content](https://ai.google.dev/api/generate-content)、[Anthropic Messages](https://platform.claude.com/docs/en/api/messages/create)、[OpenAI Responses](https://platform.openai.com/docs/api-reference/responses)。

## 密钥与网络

服务器须设置 `MODEL_CONFIG_ENCRYPTION_KEY`，至少 32 字符，推荐密码学随机值；不得在浏览器或源码填写该主密钥。
主密钥需要安全备份和所有管理节点一致配置；不要在有已存密钥时直接换值，轮换需重新录入所有模型 API Key。
AES-GCM 使用随机 nonce，并绑定配置 ID。数据库只保存加密凭据；列表返回 `has_api_key`，不返回明文或密文。
编辑留空保留原密钥；地址或协议改变必须重新填写密钥。删除配置同时删除对应凭据。
并发编辑使用 revision 比较，防止静默覆盖。
API 地址不允许 HTTP、URL 用户信息、query/fragment。连接时验证 DNS 解析结果，禁止内网、loopback、link-local、元数据/保留网段，不使用代理环境变量，不跟随重定向。
上游错误不回显请求头、密钥或原始响应体。

## 测试

可以在保存前测试当前表单，或留空密钥使用已存凭据（不能将旧密钥发送到新地址）。
连通性测试实际发送“小输出，只回复 OK”的生成请求，而非仅探测服务器端口；成功意味着所填地址、协议、模型和密钥完成了文字调用。并不保证模型风格效果。
直接提问不携带基础提示词；提示词测试携带基础提示词和完整输入。
测试只回传正文、模型、耗时、Token；不生成音频、不发布、不修改默认模型，也不自动读取客户直播间素材。
所有测试可能按服务商计费；自动化测试只调用本地 fixture，不调用付费远端 API。

## 验证

`go test ./management-service/...`；`pnpm --filter web-console build`。
协议请求/响应、认证头、reasoning 文本排除、加密/身份绑定、stage 隔离、禁用配置、回退、保密错误、内网/重定向阻止均有单元测试。
`TestSpeechModelsMySQL` 需显式提供 `-sales-test-env-file`，使用随机隔离 schema，验证迁移、加密持久化、留空保留、CAS、地址变更和删除；默认测试不访问业务数据库。
