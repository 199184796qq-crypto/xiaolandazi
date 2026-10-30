# 项目协作约定

## 互动业务链路替换上线（2026-10-31）

- 已上线并交接：源码103afab已推送；北京时间2026-10-31 03:14:33—2026-10-31 03:14:40最终发布20261031-interaction-runtime-103afab，生产HTTP适配器启用新自动互动链路，旧窗口不再并行处理。53场景/152检查及3202独立回放；固定实际整合Core 41包/485通过、Management 103包/2645通过，vet/Linux和10空闲守卫通过；42公网及标准生产冒烟通过。2730前端、27保护表/结构/运行配置及3770非目标文件保持；仅后台与三模板增量，无开停直播/串口/客户方案写入/TTS。真实预TTS测试三请求：两个问题均完成新链路，1次供应商空返回保留失败并单独复测；所有8条模型记录含完整P0和方案提示词。新建歧义判断仍会波动，不宣称语义准确率或实机出声已验。初次称呼重复/分析字段混入已修，显式语速对新合成保持。说明docs/2026-10-31-互动链路重构上线.md；最终证据E:/直播伴播-local/interaction-runtime-content-20261031，前两发布证据原样保留。编辑结束并已交接，其它差异保持。


## 原始录音互动缓冲统计修复上线（2026-10-30）

- 用户授权“继续上线”；源码b95aa17已推送，2026-10-30 22:58:29—2026-10-30 22:58:38（北京时间）发布20261030-interaction-buffer-b95aa17。单段原音不再因非Sequential把buffered_mainline_ms报0，按当前已就绪音轨实际剩余时长统计；随机库其它音轨不当未来队列，原15秒、缺料、句末/回归、低流量和顺序逻辑保持。
- 实际Core完整SHA独立重建一致，固定整合仅3源码/测试文件变化、modified=true如实归档；31包442测试事件通过/9显式跳过、vet/Linux、12库存+3实际HTTP领取及10发布守卫通过。旧版5库存/2领取失败复现，修复后通过；42公网/鉴权及标准冒烟通过，真实新场及实机声音未验。
- 共享锁/多次全场与Core空闲后，仅Core更新为2044542/SHA6a541bc3c02e707a31e129b9b0ea0d635011fb0e9e74a3560a0f8754d16e035f，网关依赖重启2044543但程序保持；Management2037382及SHA保持，三健康200。锁内27保护表前后一致；后核25表保持，发布结束后的独立方案保存有创建/更新时间证据，保留客户新版本，不放宽部署守卫或回写。全库列结构、母版/Nginx/unit/env及3770旧非目标文件保持，2730完整前端/备份latest核验保持。
- 初次因方案版本/提示词数据更新被守卫拦截，重采最新基线后保留发布，未修改客户内容或放宽守卫。无开停直播、清队列、预算/串口/固件操作或付费调用。说明docs/2026-10-30-原音互动缓冲修复上线.md，证据E:/直播伴播-local/original-interaction-buffer-release-20261030；回退须共享锁、空闲和actual指纹，保留后继。本轮编辑结束并已交接，其它差异保持。


## 网页静音注销与恢复监听已在线上确认（2026-10-30）

- 用户授权“上线部署”，修复源码291db0c已推送；北京时间2026-10-30 22:45:00核对确认actual 20261030-plan-prompt-editor-f2f9e93已含完整修复。静音取消PCM/排程、停止心跳并注销本网页接收端，恢复使用新身份接当前声音；8秒连接期限、迟到异步隔离、声音上下文恢复及失败可重试。盒子与直播控制保持。
- 首次发布被共享锁内全文件守卫拦截，未激活；并行静态更新已同时带上本修复和方案提示词后继。完整冻结整合源码，100产物与实际字节一致，独立整合候选8组新专项/3条相关脚本/类型构建通过，2条旧页面失败在原版同点。未再次切换生产或重启服务，不把纯291db0c当成完整实际源码；线上依赖单独归档而不代提交。
- 42公网资源/只读鉴权及标准生产冒烟通过，27保护表、母版/Nginx/运行配置、3773文件及三进程完整SHA保持、三健康200；2730完整前端备份/latest更新。未开停直播、客户写入或付费模型TTS；生产登录DOM/实机声卡未验，Core原音互动排队未纳入。
- 说明docs/2026-10-30-网页静音恢复上线.md；证据E:/直播伴播-local/room-audio-recovery-release-20261030。禁止用首次旧候选覆盖后继方案提示词；任何回退需共享锁/实际指纹并重新整合。本轮编辑结束并已交接，其他差异保留。


## 大屏长录音版本读取修复已在线上确认（2026-10-30）

- 用户再次反馈“读取直播间已发布智能体版本失败”；修复源码07ea819已推送。大屏同一SQL先选最大有效ID再取JSON，共享旧指针路径取消整行排序。此前只修本地，前端发布未含后台修复。
- 准备发布期间检测实际后台又被更新；未执行旧候选覆盖。最新Management2037382/SHAff86583e049780d57da2e6d1afb870a97588e8f32c0d186a388fba19644413c1已包含本次修复和DS新模板渲染，独立冻结重建完整SHA一致，VCS07ea8197e511481752b1666ee3f609583379f030/modified=true如实记录；actual current仍20261030-plan-prompt-editor-f2f9e93，不能按目录名推断后台。
- 先前固定整合源62包2612通过/124跳过、vet/Linux，最新变化HTTPAPI包1280通过；新隔离MySQL长录音/共享/并发与真实登录大屏GET通过，10守卫离线检查通过。只读生产旧SQL1038/新取原版本283；标准生产冒烟和公网鉴权通过，未做生产登录DOM或真实开播验收。
- 本次未再次重启；Core2008864/网关2008865、2646完整前端/备份、最新27保护表及配置/Nginx稳定，保留并行更新的母版。无客户写入、付费模型TTS或开停直播；旧候选未上线，不可覆盖最新。说明docs/2026-10-30-大屏原音读取上线.md，证据E:/直播伴播-local/room-original-start-release-20261030。本轮编辑结束并已交接。


## 方案提示词建立与重新生成前端上线（2026-10-30）

- 用户授权“提交上线”；源码 f2f9e93 已推送，2026-10-30 21:45:53—21:45:54（北京时间）发布 20261030-plan-prompt-editor-f2f9e93。空白明确建立、生成后手动保存、重新生成先确认覆盖，失败保留原文；DS已上线导航/API原样继承，读取/保存行为和母版保持。
- 独立复现前驱100个产物一致；固定候选10组真实父页含1440/390及最小16px、类型/构建通过。补充4条旧断言2过2原版同样失败，无新增失败；44项公网资源/鉴权及完整标准生产冒烟通过。初始归档缺shared、换行指纹、只读1038与备份下载超时均留证，修正采集后验证。
- 仅两个入口及42个新哈希资源，三服务无重启，保留实际DS后台2035033/SHA16f317cb7d90c3d8cab60b4ff7677353363083d894de4cadb96a9586a59e5a54；Core/网关保持，三健康200。27保护表、运行配置/Nginx、母版及3644旧非目标文件保持，2646完整前端备份/latest已逐项核对。无客户提示词写入、模型TTS调用或生产登录DOM验收，上一轮待发布大屏查询修复未纳入。
- 说明docs/2026-10-30-方案提示词前端上线.md，证据E:/直播伴播-local/plan-prompt-editor-release-20261030；回退须共享锁、实际指纹和后继保护。本轮编辑结束并已交接，其它差异保留。


## 方案默认声音与长录音回显修复上线（2026-10-30）

- 用户继续反馈官方声音不可用；后台腾讯目录与页面写死阿里默认失配。源码e8cb116已推送，2026-10-30 18:15:47～2026-10-30 18:15:55（北京时间）发布20261030-plan-voice-e8cb116：默认声音跟随后台目录，明确选择失效仍须改选；声音仅随方案显式保存，不写房间。阿里目录provider按既有规范补齐，精确模型/供应商/音色及配置校验保留。
- 完整保存回归另复现长录音JSON排序1038，改为原范围读取后按ID降序排列；原音、语速、251段时间轴和状态/租户/方案隔离保持，无SQL结构或全局排序参数修改。
- 前驱完整Management SHA及100前端独立重建一致；固定整合源码归档/modified=true如实记录，保留线上方案提示词。只重启Management2029924/SHAb3c2b3626ff8567c02d497133b8aea5a909a844b1f3da3e8a3d9a2bfb1d7f0be；Core2008864/网关2008865保持，三健康200。27保护表/结构、母版/Nginx/unit/env和3543旧非目标文件保持，2556完整前端备份/latest核对更新。
- 固定62包2610通过/122跳过、vet/Linux/类型构建、18声音录音页面组及20方案回归、隔离MySQL真实认证HTTP完整保存和刷新两次成功（201/200）、失效克隆409、范围/状态/顺序隔离、10守卫、63公网及完整标准冒烟通过。初始夹具缺商业表、旧读取1038及冷启动浏览器超时如实保留；未代客户保存、生产登录DOM、读取真实OSS或调用付费模型TTS。
- 说明docs/2026-10-30-方案默认声音保存修复.md，证据E:/直播伴播-local/plan-voice-save-20261030；回退须持锁、空闲及最新指纹，保留后继、全部前端和客户数据。编辑结束并已交接，其它差异保持。


## 审计截断方案保存请求修复上线（2026-10-30）

- 用户反馈上一版后仍报格式错误；根因审计中间件将大于64KiB的请求替换为前65537字节。39a75cd已推送，17:40:30～17:40:37（北京时间）上线20261030-audit-body-39a75cd：审计前缀回放后继续读原始流，保留Close/读取错误、鉴权/审计和1MiB严格字段校验。
- 实际前驱完整SHA独立重建一致，固定整合源码归档/modified=true如实记录，保留方案提示词和全部前端。只重启Management2027625/SHA1fdf5115，Core2008864/网关2008865保持、三健康200；27保护表/全库结构、母版/Nginx/unit/env及3544旧非目标文件不变，2508完整前端备份/latest核对更新。
- 固定62包2610通过/121跳过、vet/Linux、10守卫、63公网及完整标准冒烟通过。真实登录HTTP旧代码400复现，新代码完成两种传输解码与字段校验，合成缺失方案刻意404，不称完整保存；未生产登录DOM、保存客户方案、调用付费模型TTS或开停直播。上轮测试漏掉审计路径，已补真实链路回归。
- 说明docs/2026-10-30-审计截断方案保存请求修复.md，证据E:/直播伴播-local/audit-body-save-20261030；回退须持锁、空闲和最新指纹，保留后继及客户数据。编辑结束并已交接，其它差异保持。


## 原音方案保存修复上线（2026-10-30）

- 用户授权“上线发布”；源码d1fb96b已推送，2026-10-30 13:58:13～2026-10-30 13:58:21（北京时间）完成20261030-recording-save-d1fb96b。 录音完成后直接显式保存，首次无版本恢复当前用户/方案ready回执，纠正60KiB旧限制与绑定原音4000字阻塞，保留音色/语速和已保存删除。
- 前驱Management完整SHA及100个网页文件独立重建一致，保留方案提示词/OSS；固定整合源码归档、modified=true如实记录。只重启Management2019890/SHA80af59cb，Core2008864/网关2008865保持，三健康200。
- 27保护表/全库列结构、母版/Nginx/unit/env和3499旧非目标文件保持；2508完整前端备份/latest更新。62包2588通过/120跳过、vet/Linux/类型构建、7页面组和实际ready载荷校验、2隔离MySQL、10空闲守卫、63公网与完整标准冒烟通过。
- 旧原音抽取夹具缺scope在未修改基线复现，如实保留。无客户方案写入、付费模型TTS或开停直播，未验生产登录DOM。说明docs/2026-10-30-原音方案保存修复上线.md，证据E:/直播伴播-local/recording-save-release-20261030；回退需持锁、空闲和最新指纹，保留后继及数据。编辑结束并已交接，其它差异保持。



## 1.40语速与添加商品卡上线（2026-10-30）

- 用户授权“提交上线”；源码 aae61b1 已推送，2026-10-30 13:10:32～2026-10-30 13:10:40（北京时间）完成 20261030-voice-product-aae61b1。两项功能均已发布：0.80～1.40/0.01一档/61档语速，以及商品资料页常驻添加商品卡按钮、六字段填写与正式保存回显。
- 实际前驱后台完整SHA及网页100文件独立重建一致，保留已发布方案提示词及OSS功能。固定整合源码归档/modified=true如实记录；只更新Management2016341/SHA39ddd875，Core2008864/网关2008865不重启。27保护表/全库列结构、母版/Nginx/unit/env与3457旧非目标文件保持。
- 固定源码62包2579通过/119显式跳过、vet/Linux、类型构建、19组新页面与复制、两个MySQL同输入哈希及10空闲守卫通过；61公网/完整标准冒烟通过，2466完整前端备份/latest更新。原商品卡旧夹具6/7计数失败如实保留；无真实客户商品/语速写入、付费模型TTS或生产登录DOM验收。
- 说明docs/2026-10-30-语速与商品卡上线.md，证据E:/直播伴播-local/voice-product-release-20261030。回退需持锁、空闲与最新指纹，仅反向本轮差异并保留后继和客户数据。源码和本轮记录编辑结束并已交接，其它原有差异保持。


## 音频OSS直传与录音下载上线（2026-10-30）

- 用户授权提交上线；源码f1b4e3d/fb4c0e2已推送，最终2026-10-30 10:49:00发布20261030-audio-oss-fb4c0e2。每段2GiB/20段原音浏览器直传私有OSS，录音合并后私有OSS归档、固定24小时保留、302公网直下；实际业务Origin POST CORS已补齐。
- 并行plan-prompt发布被保护拦截后，完整重建Management SHA与线上e71a685f一致，保留其网页/后台和提示词更新；本轮只更新Core，网关随依赖重启且二进制保持，Management2004557、2424前端、27保护表/结构、母版/Nginx/unit/env与3458旧非目标文件保持，三健康200。固定源码归档/modified=true如实记录。
- 真实OSS下载暴露禁用response-content-type参数，已补兼容修复并二次持锁空闲发布。最终Core2008864；现有406MB录音归档ready、公网Range206及WAV/附件/15分钟签名/固定到期通过。真实浏览器OSS POST204、私有ACL/内容校验和测试对象删除404通过；未完整下载406MB或上传真实2GB、无付费ASR/TTS和开停直播。
- 固定Management62包2574测试/119跳过、最终Core31包425测试、vet/Linux、隔离MySQL、31组页面/10守卫、60公网和完整标准冒烟通过；2424完整前端备份/latest已更新。说明docs/2026-10-30-音频OSS整合上线.md，证据E:/直播伴播-local/audio-oss-release-20261030。回退须持锁/空闲复核实际版本，只反向Core差异，保留后继和客户数据。本轮编辑结束并已交接，其他差异保持。


## 图片商品理解解析修复上线（2026-10-30）

- 用户授权上线；源码0f484fb已推送，2026-10-30 09:17:45—09:18:06持共享发布锁及全场/Core空闲守卫发布20261030-image-intent-0f484fb。模型数量数字及纯文字人群列表局部兼容，保持文字API、原始返回与确认流程，解析失败和调用失败分别提示。
- 实际前驱wheel-json-repair-0e99473e独立重建，完整SHA584c0582e7077dcead4126c0f0e0dde58d8c7f4acfadddba15724d42ba69e95d一致；仅在该生产源码上叠加本人4个源码/测试/夹具。固定整合源码已归档，VCS modified=true如实记录；不把主线提交号当成完整整合源码，不代提交他方未发布的称呼改动。
- 仅重启Management1998882/SHAdb65b82528a4d0a5eda8a74375e7e699de27d0f66eabf3190b3e8d692e1a7fd0；Core1955746/网关1955748保持，三健康200。3389旧非目标文件、2356完整前端、25保护表（含模型配置/TTS账户与队列）、母版、Nginx及unit/env保持；无数据库迁移，完整前端备份逐项核验/latest更新。
- 固定Management62包/2561测试事件通过118显式跳过、全量vet/Linux、8项空闲守卫、55公网资源/鉴权及完整桌面/客户/销售/小智标准冒烟通过。未调用真实付费模型TTS、未写真实商品/方案，未验生产登录DOM。说明docs/2026-10-30-图片商品资料理解解析修复.md，证据E:/直播伴播-local/agent-image-understanding-release-20261030。回退须持锁、空闲和最新实际指纹，保留后继及全部客户数据。本轮编辑结束并已交接，其他差异保持。


## TTS账户模型配置上线（2026-10-29）

- 用户授权部署；源码4265b92已推送，2026-10-29 23:28:25—23:28:46持共享发布锁及全场/Core空闲守卫上线20261029-tts-account-models-4265b92，实际前驱welcome-address。当前及备用账户配置多个TTS模型，列表区分可用模型与获准调度的请求模型；精确模型/音色匹配，同账户全部模型共用180 RPM/3 RPS。
- 只重启Management1966113/SHAdea6bcaf77c1cd77f96d808eac1be67e2be14631eafd9c4871af3d2981d408a7、4265b92/dirty=false；Core1955746及网关1955748保持，三健康200。3344旧非目标文件、21保护业务表、母版/Nginx/unit/env和客户/销售端保持；2356完整前端文件备份逐项核对/latest已更新。
- 唯一结构增量为账户表可空models_json，既有1账户及94条任务历史、UID/限额/revision/启用状态/密钥摘要原列哈希保持；旧当前账户继续跟随方案，旧备用账户按音色映射兼容，未替客户配置模型。实际前驱588d57f保留原嵌入SQL换行独立重建，完整SHA与线上一致，继承既有称呼更新。
- 固定Management62包2525测试通过/118显式跳过、全量vet/Linux、7隔离MySQL及共享额度排除FIFO干扰复验、8组真实Vue/类型/构建、8空闲守卫/6回执夹具、54公网/鉴权及完整标准冒烟通过。未调用真实付费模型TTS，未验生产登录DOM、多房间满额实播或实机声音。说明docs/2026-10-29-TTS账户模型配置.md，证据E:/直播伴播-local/tts-account-models-release-20261029；回退须持锁空闲复核实际指纹并保留模型列及后继改动。本轮编辑结束并已交接，其它差异保持。


## TTS账户池与统一调度上线（2026-10-29 22:39）

- 用户授权发布；源码435ffbd已推送，22:39:21—22:39:42持共享锁和全场/Core空闲守卫上线20261029-tts-account-pool-435ffbd，实际前驱p0clean。逐独立账号180 RPM/3 RPS，满额切其他兼容账号、全部满额排队；主线有效缓存续播、互动声音未就绪不打断，当前阿里账号已接入。
- Management1962799/SHA75fd2798fdf43a684ca8d23479bf1ff6a38f1a4f09ccf5bd9ea835304857cc72、435ffbd/dirty=false；Core1955746/网关1955748未重启。3319旧非目标文件、21保护业务表、母版既有状态、Nginx和unit/env保持，三健康200。较旧web-desktop升级为现有web同一固定源码，2314完整前端备份逐项核对/latest更新。
- 旧11列账户/12列请求表仅增补字段及UID/Key唯一索引；1个现有账号/180与3限额/版本/暂停状态及1笔历史完成请求原字段哈希保持，无Key复制。62包2520测试通过/116显式跳过、vet/Linux、隔离MySQL5项、Vue类型/构建/4组页面桩、8空闲守卫和3回执夹具通过；54公网资源/鉴权与完整标准冒烟通过。无真实付费模型/TTS、多房间满额实播或实机声音验收。
- 活动550曾拦截；用户结束并重新授权后完成旧表兼容。首次激活的回执字段KeyError自动回退，核验数据保持后修正发布器并复验再部署，失败回执保留，不为发布停止直播。说明docs/2026-10-29-TTS账户池上线.md，证据E:/直播伴播-local/tts-pool-release-20261029。后续回退须持锁/空闲/actual指纹，保留后继及TTS表/账号/历史。本轮编辑结束并已交接，其它差异保留。


## 方案账号资产前后端上线（2026-10-29 00:27）

- 用户授权上线；本轮源码a606150已推送。方案声音、事实、风格、问题库、目的和读写规则随账号方案，编辑保存无需房间，房间仅选择已保存方案；不提供房间声音覆盖，历史绑定和业务数据保留。
- 发布前守卫拦截主线和current推进；继承79cdfd3互动取消停顿/后继b7aee0e，整合管理源码独立重建与当前完整SHA50d5219a496f9de69a00ea8914d6f058a1767ed55747e8f861d142298afecaf8一致，已包含本轮后台；不回退后继后台。最终只在00:27:21—00:27:35持共享锁叠加20261029-plan-account-assets-a606150，两入口/48新哈希资源，3227旧非目标文件保持，不切current、不重启任何服务。
- actual current保持/opt/xiaolan/releases/20261029-interaction-no-pause-79cdfd3；Management1902504/Core1871273/网关1871275及完整SHA、Nginx和unit/env保持，健康200。新增方案配置表已由后继启动创建，本轮核对6列/主键/0行，21保护表全部保持，无客户数据迁移、真实方案写入或开停直播。
- 最终整合Management60包2472测试事件通过/109跳过、vet/Linux、Core/网关、客户/销售检查构建通过；20组方案与21组问题库真实Vue/API桩、新建弹窗和偏好、类型构建、98前驱产物、51公网资源/鉴权及完整生产冒烟通过。2244完整前端备份逐项核验/latest更新；保留Node及原包体积告警，未做生产登录DOM、真实MySQL持久化业务或实机出声验收。
- 说明docs/2026-10-29-方案账号资产上线.md；证据E:/直播伴播-local/plan-account-assets-release-20261029，服务器同名overlay回执。回退只能持共享锁、核对当时实际current/指纹后反向替换两入口，保留新assets、后台、表和业务数据；不能切旧release。本轮源码及记录编辑结束并已交接，其他未提交差异保持。


## 问题库推荐标签前端发布（2026-10-28 22:47）

- 用户授权上线；源码4ac40e8已推送，22:46:54—22:47:04以共享发布锁原地静态叠加20261028-question-library-tags-4ac40e8。当时actual current为purpose-suggest-6268d37，仅替换web/web-desktop两入口及新增46哈希资源；3181旧非目标文件SHA/大小/权限保持，没有切current或重启服务。
- 问题失焦350ms防抖、前三推荐与最高分默认、自动排除词及手填/手动清空保护、更多全量下拉和不指定；推荐失败静默兜底、稳定行标识及房间/卸载/旧问题响应隔离。原CRUD/右键判型/待回答与16px/20px保持；api.ts仅透传既有静默选项。
- 固定源码类型/构建、21组真实Vue/API本地桩、98项前驱产物核对（CSV保留现场原件）、44项独立公网资源/鉴权及完整含销售/小智生产冒烟通过。2196完整前端备份逐项核验/latest更新；保留原大包告警，未做生产账号登录DOM或真实问题库保存/模型TTS验收。
- 本次静态发布Management1895307/Core1871273/网关1871275与完整二进制、Nginx及unit/env保持，健康200。初始19表核对中17表（含问题库/费用）保持；方案版本/绑定两表在22:43发生独立业务变更，早于本轮激活，完整保留，没有为了指纹相同恢复旧数据。
- 核验中另方发布plan-nobinding-85fab53；最终actual current=/opt/xiaolan/releases/20261028-plan-nobinding-85fab53，Management1898006/完整SHA3cc30de29cf8bf6d1b1f95f892d5acfe946e377732e7b9f463ab7eaaefe4708d。与本轮静态完成时相比仅VERSION/管理二进制变化，2196前端文件逐项完全相同，三健康/44公网/完整冒烟复核通过；本轮未覆盖后继后台或重复部署。
- 说明docs/2026-10-28-问题库推荐标签上线.md；证据E:/直播伴播-local/question-library-suggestions-release-20261028，服务器同名overlay回执。回滚只能在共享锁下按最新实际指纹反向替换两入口，保留新assets和后继后台，禁止切回旧current。本轮源码与发布记录编辑结束并交接，其他既有修改原样保留。


## 当前前端补丁（2026-10-28 11:26）

- 用户截图确认直播页语速仍五档：原RoomDetailView单独数组遗漏，旧夹具替换为公共数组导致漏检；9f36e4b统一真实页面选项，修复前回归五档失败、修复后两页41档/1.01保存刷新等10组及五宽通过，类型/构建和98项线上前驱逐字节核对通过。
- 11:26:15—11:26:26纯静态叠加20261028-room-voice-rate-options-9f36e4b：actual current保持core-identity-5acb027，2入口替换、40哈希资源新增；2795旧非目标文件SHA/大小/权限保持，Management1833784/Core1835450/网关1835452与完整SHA、18保护表、Nginx及unit/env保持，未切current或重启。
- 新入口index-TRqYwXMB.js；9项独立公网检查和健康200，1806完整前端备份逐项核对/latest更新。原声速发布说明已追加勘误。证据E:/直播伴播-local/room-voice-rate-options-fix-20261028，说明docs/2026-10-28-直播页语速档位补齐.md。仅前端补齐，未写真实方案/队列/声音或调用付费模型TTS；未验生产登录DOM/实机声音。当前入口已有补丁，后续后台发布须继承实际完整前端；源码/本轮记录编辑结束并已交接。


## 历史生产（2026-10-28 10:34）

- 用户授权语速0.01档位上线；源码0eaa27a已推送main，北京时间10:34:29—10:34:49发布20261028-voice-rate-hundredths-0eaa27a，实际前驱tts-pricing-ui-61f2321。方案页与直播页支持0.80～1.20、0.01步长/41档，两位回显；独立保存并在下次开播使用，默认1.00。
- 仅替换Management与web/web-desktop两入口、新增40个哈希资源及VOICE-RATE-SOURCE.json；2753旧非目标文件SHA/大小/权限保持。三次锁内全场/Core空闲检查通过；只重启Management，PID1828172/SHA de6d85db；Core1814486、网关1814491及完整程序保持。18保护表、系统费用设置、母版/MANIFEST、Nginx和unit/env保持，无数据库结构迁移、真实方案写入、开停直播或付费模型/TTS。
- 线上旧后台已独立重建逐字节匹配，固定源码Management56包/2432测试通过、109显式跳过，vet/Linux构建及Core/网关全量、两手机检查通过；最终前端98项前驱产物匹配、真实Vue+API桩10组/五宽/类型/构建通过。45项公网资源/鉴权及含销售端和小智OTA握手的标准冒烟通过，三服务健康200；1766完整前端文件备份已核对/latest更新。未做生产登录后DOM、真实MySQL持久化或实机听感验收，保留原chunk体积告警。
- 说明docs/2026-10-28-语速百分位档位上线.md；证据E:/直播伴播-local/voice-rate-hundredths-release-20261028，服务器回执/opt/xiaolan/deployments/20261028-voice-rate-hundredths-0eaa27a.json。回滚必须持共享发布锁、确认无活动直播并复核实际current及保护指纹，不覆盖后继版本；源码与发布记录编辑结束并已交接，其它已有差异保持。


## 历史生产（2026-10-28 08:27）

- 用户授权登录后误报直播间授权修复上线；源码a7deca9已推送main，北京时间08:27:35—08:27:43纯前端发布20261028-agent-login-scope-a7deca9，实际前驱complaint-tone-65618c1。等待登录信息恢复后再挂载全局智能体，仅有效customer自动读方案；合并重复初始化，账号/租户/请求序号隔离旧响应，退出或卸载后失效。
- 仅替换web/web-desktop两个入口、新增40个哈希资源与AGENT-LOGIN-SCOPE-SOURCE.json；2671旧非目标文件SHA/大小/权限保持，1684完整前端文件已备份核对并更新共享latest。实际前驱的完整前端与1644文件旧备份逐字节一致，继承实际后台，未用历史后台覆盖。手机端、销售端、旧assets与CSV保持。
- Management1784617/e5a98a82、Core1778986/b7374758、网关1778988/4d19db61的PID与完整二进制SHA均未变；18保护表、Nginx、unit/env及费用设置保持。未重启服务、修改授权、操作真实方案/直播或调用付费模型/TTS，未请求OTA发令牌。
- 固定干净源码11组真实Vue登录/账号切换浏览器回归、10项诊断/登录测试、授权scope回归、类型检查/生产构建通过；98项前驱产物核对（两份CSV保留线上原件）、42公网资源/鉴权和生产冒烟通过，三服务健康200。保留chunk体积告警及既有Node25与根engine声明差异；未执行生产账号登录DOM测试。
- 证据E:/直播伴播-local/agent-login-plan-scope-release-20261028，服务器回执/opt/xiaolan/deployments/20261028-agent-login-scope-a7deca9.json。回滚须持共享发布锁，核对实际current/完整文件/服务指纹；后继后台变更时只能反向前端叠加，不能回旧目录覆盖。源码与本轮记录编辑结束并已交接，其它已有差异保留。

## 历史生产（2026-10-27 13:49）

- 用户授权智能体对话框顶部拖拽上线；源码f719fc4已推送main，北京时间13:49:07—13:49:15纯前端发布20261027-agent-header-drag-f719fc4，实际前驱device-reconnect-eb99cb4。按住顶部空白、标题或副标题可拖动，方案下拉/标签/模式按钮和关闭按钮不误拖；保持原把手、尺寸、屏幕边界与输入草稿。
- 仅更新web/web-desktop两个入口，新增40个哈希资源与AGENT-HEADER-DRAG-SOURCE.json；2627旧非目标文件SHA/大小/权限保持，1644完整前端文件已备份核对并更新共享latest。手机端、销售端、旧assets与CSV保持；18保护表、Nginx和实际unit/env保持，未清缓存或更改业务参数。
- 三服务PID/完整SHA均未变：Management1703451、Core1558558、网关1703460；没有重启服务、开停直播、刷盒子或新增付费模型/TTS调用。固定源码类型/构建、98项前驱产物核对（两份源CSV差异继续保留线上原件）、13项真实Vue鼠标/触摸交互、42公网资源/鉴权及完整标准生产冒烟通过，三服务健康200。旧整套夹具事实检查计数和560px回答区高度失败在旧源码也可复现，未称全量通过；保留原chunk体积告警，未做生产登录DOM交互验收。
- 证据E:/直播伴播-local/agent-header-drag-release-20261027，服务器回执/opt/xiaolan/deployments/20261027-agent-header-drag-f719fc4.json。回滚须持同一发布锁并复核实际current/完整文件及服务指纹，若后继后台已变更则做反向前端叠加，不能直接回旧目录覆盖。本轮源码与发布登记编辑结束并已交接，其它已有差异保留。

## 历史生产（2026-10-26 22:12）

- 用户授权设备真实在线与快速恢复修复上线；源码eb99cb4已推送main，北京时间22:11:59—22:12:05上线20261026-device-reconnect-eb99cb4，实际前驱recording-polish-retry-1b7406b。点击网页连接/心跳不再写假在线；真实连接与声音意图区分，锁后20秒真实心跳准入，旧暂停意图保留。网关有界一次性应答窗口、暂停停用取消音频订阅而保持真实连接，原主线/互动/复用/成本及Core保持。
- Management1703451/SHA b11402735e8334bd6b7c2ef4cbda60aee581887fe265cc3f23c61e460f3b2a6b、网关1703460/SHA 4d19db610163b6aabf68b1fa5c179a2d0dd9c84fadaa7c3541cecfc30476184b，均固定eb99cb4/dirty=false；Core1558558及完整SHA保持。旧dirty Management以1b7406b+无害marker独立重建，完整SHA与线上相同，现有功能已继承。
- 两程序与桌面web/web-desktop叠加发布：2入口内容变化、4图标仅0664收紧到0644、新增41文件，2580旧文件元数据保持；1554旧assets、2 CSV及customer/sales保持，完整前端1604文件备份/latest更新。18保护表（含全部费用配置、模型池、方案与媒体）、Nginx及实际unit/env保持；无新迁移、缓存清理、参数修改、固件清单激活或盒子刷机。锁内三次全场/Core空闲守卫通过，未开停直播。
- 固定源码Management51包/2391测试通过109显式跳过、网关134事件、两模块全量vet/Linux构建；前端5组/类型构建、20依赖和无修改树继承通过。隔离MySQL3项与必要测试夹具overlay证据保留；8空闲守卫、21回滚合同夹具、独立包/进程核验通过。42公网/鉴权及完整标准冒烟通过（单个URLError只重试该项后401，初始证据保留）；三服务健康200。权限白名单初始核验过严已仅明确四图标收紧并保存失败，不泛化放宽其它资源。
- 2.2.8固件ZIP与应用已编译交付，需独立安装到ESP32-S3CAM 16MB盒子才启用30秒半开检测、后台快速重连及故障日志。未刷机/启用OTA，不冒称已修复路由器周期断链物理根因或实机声音/60秒内恢复验收。说明docs/2026-10-26-设备真实在线与快速恢复修复.md；证据E:/直播伴播-local/device-reconnect-release-20261026，服务器同名回执及证据目录。回滚须持锁、完整指纹与空闲守卫，固定前驱1b7406b，仅重启Management/网关，保留诊断与OTA状态。源码和本轮记录编辑结束并交接，其它已有差异保留。

## 历史生产（2026-10-26 11:12）

- 用户明确授权修复跨场首轮缓存更新后上线；源码c1815d3已推送main，北京时间11:12:25—11:12:45发布20261026-recording-first-pass-c1815d3，前驱actual current为device-maintenance-6935fff。新场首轮按有效逐句库存参与整轮更新预算，旧场只完成部分基础声音也能继承；真正全冷首轮仍只补基础声音。
- 仅替换Management与新增RECORDING-FIRST-PASS-SOURCE.json；Management1672116/VCSc1815d3/dirty=false/SHA3a83e98e2b2d9837f527f65a770480ba186fdf02c50570e1b7706e334c42c3d4，Core1558558与6935fff网关1666043的PID/完整SHA保持。三次锁内全场及Core空闲检查通过，未开停直播、刷机或请求真实模型/TTS。
- 2586旧非目标文件、1564完整前端文件及母版既有状态、18保护表（含全部费用配置/模型池/方案/媒体）、Nginx和实际unit/env保持。无新增数据库迁移，已有跨场缓存与更新余额不重置、历史账务不回算；必要补声与可选更新仍按真实请求分开记账和准入。
- 固定干净源码Management51包/2380测试事件通过、108环境跳过，新增部分首轮落盘重开/预算失败复用/1版4版后置库存/失效P0缓存回归通过，全量vet/Linux构建及20依赖与无修改树继承、8空闲守卫夹具、42公网资源/鉴权和完整标准冒烟通过。完整前端备份latest已更新；没有真实新场更新率、实付费用或盒子完整出声验收声明。
- 说明docs/2026-10-26-跨场首轮按缓存规划更新.md；证据E:/直播伴播-local/recording-first-pass-release-20261026，服务器回执/opt/xiaolan/deployments/20261026-recording-first-pass-c1815d3.json。回滚须持发布锁并空闲，前驱6935fff；本轮源码与发布记录编辑结束并已交接，其他既有差异原样保留。

## 历史生产（2026-10-26 10:10）

- 用户授权部署ESP32-S3摄像头16MB盒子兼容服务端；源码6935fff已推送main，北京时间10:10:16—10:10:22上线20261026-device-maintenance-6935fff，前驱actual current为recording-budget-fix-75416fe。新增受控OTA协议与用户隔离故障日志服务，升级清单保持缺省关闭，未向盒子发布固件或刷机。
- 仅替换Management与xiaozhi网关及新增DEVICE-MAINTENANCE-SOURCE.json；Management1666033/VCS6935fff/dirty=false/SHAe33561dc61799e7b3d1a9c7ed7213ac49edc1d8fb11d7db6ed4d4756f5b1b78f，网关1666043/VCS6935fff/dirty=false/SHA7ad03a458ecc389a8c856beb4355ea6b8dfa2a29d4a886f5aa084c2bdeb9cded；Core1558558及完整SHA保持。旧dirty网关源码已独立重建，SHA与生产和历史原件完全一致，真实心跳/当前连接/关机指令逻辑保留。
- 锁内三次全场及Core空闲检查；2584旧非目标文件、1564前端文件、母版/MANIFEST既有状态、18保护表（含方案/系统设置/模型池）、Nginx及实际unit/env均保持。MySQL8启动唯一新增device_diagnostic_batches，17列/10条索引明细/0行核验通过；私有根位于MEDIA_ROOT同级发布树外，0700、服务用户所有且可写。无维护初始化禁用/迁移失败日志，catalog与固件bin均未发布；回滚保留新表及私有日志。
- 固定干净源码Management51包/2367测试事件通过、108环境跳过，网关129事件及两模块全量vet/Linux构建通过，20依赖与无修改树继承、56项独立核验及8空闲守卫夹具通过。42公网字节/鉴权、三服务健康及完整标准生产冒烟通过，完整前端备份latest已更新。网关恢复1台设备连接只证连接；无新固件实机音频/日志上传/断电回滚验收，不称掉线物理根因已治愈。
- 说明docs/2026-10-26-小蓝盒子网络升级与故障日志.md；证据E:/直播伴播-local/device-maintenance-release-20261026，服务器回执/opt/xiaolan/deployments/20261026-device-maintenance-6935fff.json。首次USB安装新固件后才具备维护协议；激活未来OTA前须完成实机验收与下载授权日志保护。源码及本轮发布登记编辑结束并交接，其它已有文档/提示词差异原样保留。

## 历史生产（2026-10-26 07:56）

- 用户授权提交部署481预算审计修复；源码75416fe已推送main，北京时间07:56:30—07:56:35上线20261026-recording-budget-fix-75416fe，前驱actual current为live-cost-settings-7f362dc。少版本只作更新排序优先，不再排除全部四版健康句；解意图真实模型调用惰性绑定同一有效session/run及interaction归属，缓存零调用路径保留。
- 仅替换Management及新增RECORDING-BUDGET-FIX-SOURCE.json；Management1660688/VCS75416fe/dirty=false/SHAf4aafa2772660149655574efb99af20bd9fe54d4320745864284e217a6f44ad5，Core1558558与网关1558559的PID/完整SHA保持。三次锁内空闲守卫，未启停客户直播；2584旧非目标文件、1564前端文件、母版/MANIFEST既有状态、18保护表（含全部系统设置）、Nginx及实际unit/env均保持。
- 固定干净源码Management50包/2355测试事件通过、108环境跳过；全量vet/Linux构建及20依赖一致，8个离线发布空闲守卫夹具通过。38公网资源/鉴权、8080/8081/8083健康与完整标准生产冒烟通过，完整前端备份与共享latest指针已更新。无真实模型/TTS、新场效果、供应商实付或实机出声验收声明。
- 481参考费用5.414638元、6小时50分48.122秒；第19—37轮长期低更新缺陷已离线复现并修复，158笔解意图0.038824元仅在报告补核，未改历史账务或预算参数。说明docs/2026-10-26-481场预算审计与库存饥饿修复.md；证据E:/直播伴播-local/recording-budget-fix-release-20261026，服务器回执/opt/xiaolan/deployments/20261026-recording-budget-fix-75416fe.json；回滚前驱/opt/xiaolan/releases/20261025-live-cost-settings-7f362dc，回滚仍须发布锁和空闲守卫。源码与发布登记编辑结束并交接，其它既有差异保留。

## 历史生产（2026-10-26 07:40）

- 用户授权提交部署后台直播计费与小时预算配置；功能76bb202及已上线前端来源继承7f362dc已推送main，07:39:51—07:40:14发布20261025-live-cost-settings-7f362dc，前驱actual P0-exclusions-a6bd676。后台TTS/模型Token/完整输入阶梯与每小时预算可维护，默认0.8元/万计费字符、主线1/可选1.2/总1.3元目标；必要业务可超目标，价格冻结、历史不回算、缓存不清空。
- Management1658274/VCS7f362dc/dirty=false/SHA51da23996cb9d841e14464188256c0ec9f71ac604caff80c0432e654ad0eac54；仅管理服务重启，Core1558558及网关1558559PID/完整SHA保持。启动仅新增live_cost_config默认种子1行，其余37设置及其它保护表、旧assets/CSV、移动端、提示词/MANIFEST、Nginx及实际unit/env保持；2491旧非目标文件保持。
- actual P0二进制与源码完整SHA匹配；原生产桌面49文件由明确源逐字节重建，两份既有页面修改已继承提交。76bb202固定Management50包/全量vet通过，最终后台树/20依赖不变继承其结果并重构建；最终前端类型/构建、费用浏览器、原18组标签夹具默认值适配回归、38公网与标准冒烟通过，原夹具缺pinned失败证据保留，保留Node警告。锁内3次空闲守卫；无真实开播/付费模型TTS，未冒称生产DOM、实付、新场成本或实机验收。
- 说明docs/2026-10-25-后台直播计费与小时预算配置.md；证据E:/直播伴播-local/live-cost-settings-release-20261025；服务器回执/opt/xiaolan/deployments/20261025-live-cost-settings-7f362dc.json。完整前端备份1564文件；本任务源码与发布登记完成交接，其他共享文档/提示词差异保留。

## 历史生产（2026-10-25 23:25）

- 用户授权提交部署动态更新调度；实现2c1d928已推送main，北京时间23:24:53—23:25:12上线20261025-recording-dynamic-refresh-2c1d928，继承actual current recording-incremental-681b2de，仅替换Management及新增来源JSON。
- 按滚动小时账本、有效音轨轮长及报价提前安排整轮更新；版本少/长期未变优先、4版轮换、确定失败有界补选，成功未采用仍收费，重复已有表达不买新声音。1.0主线目标/1.2可选软线/1.3总目标分开，必要业务继续；首轮、事实/声音缓存身份及实时模式保持。
- Management1642440/VCS2c1d928/dirty=false/SHAc09659e6395f57519dc32ff2f9ddab2e8c26baeea18e6e3b6293d66ec4d24702；Core1558558和网关1558559的PID/完整SHA保持。1524完整前端、185母版及既有MANIFEST状态、Nginx、18保护表与实际unit/env指纹保持，2542旧非目标文件保持，最新完整前端备份指针已更新。
- 固定源码Management50包、四包vet/Linux构建、20依赖继承、真实MySQL只读新聚合SQL、35公网后核验和完整标准冒烟通过。锁内三次空闲检查，无新迁移、真实开播、付费模型TTS，不冒称新场更新率/实付/实机验收。未交接P0/前端/提示词差异不代提交。
- 说明docs/2026-10-25-话术优先动态更新调度上线.md；证据E:/直播伴播-local/recording-dynamic-refresh-release-20261025，服务器回执/opt/xiaolan/deployments/20261025-recording-dynamic-refresh-2c1d928.json。本轮源码与发布登记编辑结束并交接；后续从actual current继续。

## 历史生产（2026-10-25 22:16）

- 用户授权跨场文案增量缓存修改、提交部署；实现681b2de已推送main，22:16:36—22:16:55上线20261025-recording-incremental-681b2de，继承实际reuse-cost-424e50e，仅替换Management及新增来源JSON。
- 文案版本与事实/审核/实际声音依赖分开；稳定标签逐句继承未改成品，变化/删除源句全部旧版本从新场可用索引移除，段落润色与明确跨标签指代保守扩大失效。更新余额按可靠源句迁移，不因编辑新增额度。新场不恢复旧游标，首轮不加可选润色，必要补声沿用成功请求计费和现有提前供料；实时模式首次开场规则保持。
- 单活动清单/OS文件锁/原子落盘/场次与所有者CAS，以及V3持久历史屏障防止修改后回退、定向清理或并发迟到恢复已淘汰V2。旧V2仅完整身份相同且源句唯一时一次升级，缺来源时允许补基础库；未物理删除排队声音或客户录音/SRT，不冒称完整自然语言依赖图。
- Management1638868/VCS681b2de/dirty=false/SHA55f101283ececc4a0b59cef3eb6f7053a6a85b56850b9ac6d24979758924a5cd；Core1558558与dirty网关1558559的PID/完整SHA保持。2541原非目标文件、1524完整前端、185母版/MANIFEST旧状态、Nginx、18类客户/媒体/配置表及实际unit/环境指纹保持，最新完整前端备份指针已更新。
- 三次锁内空闲检查、固定Management50包/vet/Linux构建、35公网及完整标准生产冒烟通过；20依赖不变，无变化Core/网关/前端/手机沿用同轮标准验证。跨进程改三句之一只1次新TTS桩/0模型，其余音轨复用；同句跨段/跨标签指代/取消/清理并发/余额回归通过。无新数据库迁移或真实开播/付费模型TTS，不冒称新场实付、完整PCM或实机验收。
- 说明docs/2026-10-25-跨场文案增量缓存更新.md；证据E:/直播伴播-local/recording-incremental-release-20261025，服务器回执/opt/xiaolan/deployments/20261025-recording-incremental-681b2de.json。其它前端/文档/提示词差异保留，不代提交；本次源码与发布登记编辑结束并交接。

## 历史生产（2026-10-25 21:43）

- 用户授权提交部署与统计真实计费；源码424e50e已推送main，21:43:35—21:43:55上线20261025-reuse-cost-424e50e，实际前驱reuse-batch4-29a0e74。继承线上已运行但原未提交的纯文本方案支持，旧batch4十份差异完整重建SHA与原运行程序逐字节一致，未覆盖DeepSeek业务。
- 请求成功返回即计费，不依赖采用/归档/送播；TTS按用户最新每万计费字符0.8元、汉字2/标点及其余非汉字符1，原Unicode数、加权字符数和供应商量独立。Qwen3.7 Flash按完整输入Token分档与实际有效缓存量估算；账单尚未导入，不冒称实付。每轮更新/复用/预算拒绝及请求轮次归属已记录，旧媒体时长不再用于预算；窗口外未知预留保留账务与幂等围栏，另列待核对，不永久锁死新轮更新。
- 仅替换Management及新增来源JSON；Management1637461/VCS424e50e/dirty=false/SHA ac3df46ed3188582f353d61064d046912db95edec876cd96422fb2d4cde75d88。Core1558558/2d0e2e8和dirty网关1558559的PID及完整SHA保持；前端、提示词、Nginx、服务配置保持actual current。
- 锁内三次空闲守卫通过，479人工结束后部署，未开停客户直播。2540原非目标文件、1524前端SHA/大小/权限、185提示词及MANIFEST旧状态、15类商户方案/配置、实际unit/环境文件与运行环境摘要前后保持。完整前端备份latest已指向本次1524文件核验结果，不回退旧1400/4661产物。
- 最终Management50包/vet通过；无变化Core31包/网关/移动端检查/三前端构建沿用同轮510全标准通过结果，差异和20依赖SHA已验证。真实MySQL新增两请求表列/索引与只读滚动SQL、35公网字节/鉴权、健康及完整标准冒烟通过；未主动模型/TTS或真实新场验收，既有Node25.2.1 engine警告保留。
- 说明docs/2026-10-25-话术优先复用与请求费用闭环修复.md；证据E:/直播伴播-local/reuse-cost-release-20261025，服务器回执/opt/xiaolan/deployments/20261025-reuse-cost-424e50e.json。只读report_cost.py按建表时间区分无覆盖，旧479不能报0元；历史成功证据参考约0.741元并非完备供应商账。其它前端/文档/提示词差异不代提交。本次源码与发布登记编辑结束并交接。

## 历史生产（2026-10-23 12:30）

- 用户授权技术错误不中断直播及提交发布部署；实现源码 `2d0e2e81f5921943a03214af4cdf9192b6c46bb6` 已推送main，12:30:19—12:30:27上线 `20261023-live-continuity-2d0e2e8`，继承实际 `20261023-poolvis-e7b3af1`，仅替换Core/Management并新增来源说明。476互动返回旧下标与裁剪导致追加越界已离线复现并修复，交付结果不明保留ID和声音重试，技术供料退出不再直接结束整场；同场条件继续、数据库行版本与Core控制代次及并发恢复任务围栏闭环，人工暂停/停止及真实结束仍优先。
- Management PID1558573/SHA256 `c9e16568fdaa1b0d24e036ce61d1fd567cac0241f56cefdcb82b7619d1f26b63`；Core PID1558558/SHA256 `35d53bec289d2ec09a1dd382252b7d8e59ceb6b0f087b5edd1f75233a9d0e2e6`，运行VCS均为2d0e2e8、dirty=false。网关随Core依赖被动重启至1558559，但实际dirty程序SHA256 `d794e9f0b7fe964116394a8d24427e9b3f20d69310086eae1f5346137b433ffd`保持，不能用本地旧网关覆盖。
- 固定干净提交标准全质量通过：三个Go模块全量、客户/销售检查、三端构建及Linux构建；实际Node25.2.1有仓库engine范围警告，未声称版本合规；竞态检测因无CGO编译器未执行。35项公网指纹/鉴权、三服务健康及完整标准生产冒烟通过，未开真实直播或调用付费模型/TTS，未冒称实机声音验收。
- 发布三次空闲守卫与最终只读核对均无活动直播；2413旧非目标文件、1400前端完整SHA/大小/权限、提示词/MANIFEST、Nginx、15类配置及客户资产指纹保持。两份提示词既有MANIFEST不一致原样保留；共享 `E:/直播伴播-local/frontend-preservation/latest.json`已指向本次实际1400文件核验备份，未知前端源码不冒充旧提交，不能强制恢复历史4661产物。
- 说明 `docs/2026-10-23-直播故障自动恢复与连续供料修复.md`，证据 `E:/直播伴播-local/live-continuity-fix-20261023`，服务器回执 `/opt/xiaolan/deployments/20261023-live-continuity-2d0e2e8.json`。源码与发布记录编辑结束并已交接；其他进行中的前端、提示词和文档差异保持。后续继续按actual current及实际指纹整合，不据本条历史状态覆盖并行更新。

## 历史生产（2026-10-22 20:12）

- 本轮前端源码 `7c25da81a1521701fc83a1907999469dcd308a6e` 已推送main，20:11:49—20:11:54纯前端发布 `20261022-recording-original-7c25da8`；继承实际recording-delete-46da15f，不重启服务。原音/SRT按声音模式及当前标签显式关联显示、配对移除留正文、逐标签归档回显、共享正式音轨引用及旧异步回执隔离，新媒体文字最小16px。
- 另一端20:12:14—20:12:18以本轮发布为前驱上线 `20261022-batch-polish-4ac7ad8`，当前即该后继版。其仅改Management二进制与BATCH-POLISH-SOURCE.json，本轮42份前端变更全部保留；不能直接回切旧release覆盖新后台。当前Management1524923/4ac7ad8，Core1503625/21288b3、网关1503626/eb114a5与Nginx保持；本轮不冒称执行另一端后台发布。
- 两入口及40新资源、入口 `index-CMLXN1lc.js`／样式 `index-C2tdjWSg.css`；本轮5614旧非入口文件、手机／销售／母版保持。21组原音+18组标签本地验证、四宽16px与布局、固定源码类型/构建/98基线匹配通过；8公网指纹、健康/鉴权及标准冒烟通过。4661完整前端文件SHA／大小／权限与后继实际current匹配，共享latest按后继更新并区分后台与前端来源。
- 说明 `docs/2026-10-22-逐标签原音与字幕显示上线.md`；证据 `E:/直播伴播-local/recording-tab-original-media-20261022/release`，含后继并发核对及完整备份。无真实方案保存/删除、开停直播或模型TTS，不冒称生产DOM验收。若移除正式原音需准备替代声音后才可保存；回滚必须逆向前端整合保留最新后台。源码与发布记录编辑结束并已交接，其它进行中内容保持。

## 历史生产（2026-10-22 19:42）

- 北京时间19:42:10—19:42:14上线 `/opt/xiaolan/releases/20261022-recording-delete-46da15f`，前端源码 `46da15fb9490d754dd73b1e6e5a4ae8d1f4bbf0f`。删除模式仅×可删且保持模式，非×点击只退出、不顺带切换／新增／拖动；禁用+可退出、最后一项禁用×不删，监听随关闭／disabled／卸载清理。
- 首次current守卫拦住另一端并行发布，已按实际前驱 `20261022-cache-hit-e60cc9f` 重新整合；Management1523056/VCS e60cc9f/SHA1b8e391b保持，不回退缓存后台。Core1503625/21288b3、网关1503626/eb114a5、三个PID及完整程序哈希和Nginx保持，本次不重启任何服务。
- 两入口及40新资源上线，5573旧非入口文件、手机／销售／母版保持。入口 `index-CMuZj9nL.js`、样式 `index-D0xAXxJE.css`。固定前驱98产物匹配，新源码类型／构建及18组本地真实组件／父函数交互含Touch和三宽通过，8公网指纹／健康／鉴权／标准冒烟通过。4621完整前端备份逐项SHA／大小／权限核验，共享latest指针已更新。
- 说明 `docs/2026-10-22-话术标签管理与页面精简上线.md`第19:42节；证据 `E:/直播伴播-local/recording-tab-delete-dismiss-20261022/release` 含attempt-1／并行变更及重核，服务器回执 `/opt/xiaolan/deployments/20261022-recording-delete-46da15f.json`。专用静态回滚目标cache-hit-e60cc9f已备未用；无真实方案操作／付费模型TTS，未冒称生产DOM验收。源码与发布记录编辑结束并已交接，其它进行中差异保持。

## 历史生产（2026-10-22 19:08）

- 北京时间19:08:01—19:08:06纯前端上线 `/opt/xiaolan/releases/20261022-recording-handle-98031c1`，源码 `98031c1ef035013f3d5438193f7a47d6acdb67da`；前驱 `20261022-recording-tabs-572ba90`。仅六点把手可启动鼠标／触屏拖动，文字区点击切换，选中悬停保持白字、普通项保持灰蓝；保留删除／动画／键盘排序及音文关联，覆盖下文鼠标整标签可拖的历史行为。
- 三服务未重启，Management1518769/b2b67f3、Core1503625/21288b3、网关1503626/eb114a5及程序哈希保持；Nginx／手机／销售／母版不变。两个入口及40新资源上线，5531旧非入口文件保留。入口 `index-Bm3wX174.js`、样式 `index-5CWhOuxv.css`。
- 固定前驱98产物匹配，固定新源码类型／构建、14组本地交互含鼠标／真实Touch文字区不误拖及悬停颜色、三宽度和截图通过；8公网指纹／健康／鉴权／含销售小智的标准检查通过。4581完整前端备份SHA／大小／权限核对通过，`E:/直播伴播-local/frontend-preservation/latest.json`已更新。
- 回执说明 `docs/2026-10-22-话术标签管理与页面精简上线.md`第19:08节，证据 `E:/直播伴播-local/recording-tab-handle-hover-20261022/release`，服务器回执 `/opt/xiaolan/deployments/20261022-recording-handle-98031c1.json`。专用静态回滚已备未用；无真实方案写入／开停直播／付费模型TTS，未冒称生产DOM验收。源码与发布记录编辑结束并已交接，其它进行中差异保持。

## 历史生产（2026-10-22 18:57）

- 北京时间18:57:20—18:57:26上线 `/opt/xiaolan/releases/20261022-recording-tabs-572ba90`，前端源码 `572ba90f02241f77ee380d02185f85ea2b87070f`；实际前驱 `20261022-isolation-b2b67f3`。纯前端发布，三服务未重启，Nginx保持。
- 话术优先页移除保存状态块、快照页脚、小贴士和两句副说明；编辑区单列展开，SRT入口移到播放器下方。标签新增删除管理/任意删除角标、鼠标与触屏拖拽动画/键盘排序，“标签标题”改“标题”；稳定ID保持正文/声音/草稿/当前项关联、保存顺序，至少一项、至多20项，不改变随机播放业务。
- 继承实际Management PID1518769/VCSb2b67f3、Core1503625/VCS21288b3、网关1503626/VCSeb114a5，PID与完整程序哈希保持。两个入口及46新资源上线，5484旧非入口文件保留，手机/销售/母版保持；新入口 `index-BzCgb4m9.js`、样式 `index-sN_7qbh7.css`。
- 固定前驱98产物复现匹配，新版类型/构建、真实组件与父函数11组本地交互含Touch及三宽度、10公网资源/页面指纹、健康/鉴权与含销售小智的标准检查通过。4541完整前端文件的SHA/大小/权限备份与生产一致，共享指针 `E:/直播伴播-local/frontend-preservation/latest.json` 已更新。
- 说明 `docs/2026-10-22-话术标签管理与页面精简上线.md`，证据 `E:/直播伴播-local/recording-script-tabs-20261022/release`，服务器回执 `/opt/xiaolan/deployments/20261022-recording-tabs-572ba90.json`。专用静态回滚已备未执行；无真实方案写入/开停直播/付费模型TTS，不冒称生产DOM点击验收。源码与发布记录编辑结束并已交接，其它未提交历史/方案文档保持。

## 历史生产（2026-10-22 14:37）

- 当前 `/opt/xiaolan/releases/20261022-voice-selector-38bb904`，前端源码 `38bb904`，前驱 `20261022-voice-choice-b62f96a`。北京时间14:37:27—14:37:30纯前端发布，未重启任何服务，Nginx不变。
- 录音主线页脚「原声 / AI 声音」改为图标＋文字的圆角分段选择器，紫色选中，保留原生单选/v-model、键盘切换及保存逻辑；修正单选框继承全局输入框宽高导致的大圆圈和文字外溢，支持窄屏。
- 固定提交重建旧前端98文件与实际生产匹配，再发布web/web-desktop两个入口及40个新资源；5107个旧非入口文件、后端/提示词/手机端保持。Management/Core/网关PID仍1502264/1503625/1503626，二进制与Nginx指纹相同；三服务健康200、两接口未登录401，五项公网页面/新旧资源字节核验通过。
- 新入口 `index-BtIC-WQZ.js`、样式 `index-DnB_i7g_.css`。4175个前端文件完整备份的哈希/大小/权限已核对，权威指针 `E:/直播伴播-local/frontend-preservation/latest.json` 已更新。回滚目标 b62f96a，纯静态、无需重启。
- 证据 `E:/直播伴播-local/recording-voice-choice-style-20261022/release`，服务器发布记录 `/opt/xiaolan/deployments/20261022-voice-selector-38bb904.json`。本地局部预览已验点击互斥、方向键及360px窄屏；未执行真实方案保存/模型/TTS。隔离构建的依赖意外重新解析已恢复十项原运行版本并保留整个意外依赖目录，后续使用显式Node CLI；恢复后旧版98文件匹配，详情见证据内environment-recovery.json。

## 历史生产（2026-10-22 09:33）

- 当前 `/opt/xiaolan/releases/20261022-recording-append-db2d799`，源码 `db2d799`；前驱 `20261022-recording-buffer-cbb64ac`。Management PID1480893/SHA256 `3cf6deaccb8f2206e7c61d3cecf56974f18b26c240fc0ed476cce619cb64f650`；只重启Management，Core/网关/Nginx/前端保持。
- 按交接文档改用Core `program/append`，不再用`RefreshRoomProgram`替换；下一段立即进未来窗口，自动互动可准备/领取。45s缓冲保留。
- 五包测试、8080/8081/8083健康通过；待重开验证自动claim=true并播放。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`、`docs/2026-10-22-给DeepSeek-场次424自动互动未播原因.md`，证据 `E:/直播伴播-local/recording-append-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-append-db2d799.json`；回滚cbb64ac。

## 历史生产（2026-10-22 09:31）

- 当前 `/opt/xiaolan/releases/20261022-recording-buffer-cbb64ac`，源码 `cbb64ac`；前驱 `20261022-recording-direct-670cd78`。Management PID1480243/SHA256 `4cd20b9e273b68ff568c044224b0bbdaa937707d6c695ded10ec5b550996ce90`；只重启Management，Core/网关/Nginx/前端保持。
- 425实测Core队列有3条PENDING/READY_TO_INTERRUPT，但claim `claimed:false`、`buffered_mainline_ms=8983`；系统互动因未来主线不足不领取。录音流式缓冲目标20s→45s。
- 五包测试、8080/8081/8083健康通过；待重开验证系统互动claim=true并播出。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-buffer-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-buffer-cbb64ac.json`；回滚670cd78。

## 历史生产（2026-10-22 09:22）

- 当前 `/opt/xiaolan/releases/20261022-recording-direct-670cd78`，源码 `670cd78`；前驱 `20261022-interaction-pref-c8546f0`。Management PID1478229/SHA256 `b25c1170eedad452a0c3a87251d476dd4f642e9e9a7f6aca4e9ffd34d3c7a584`；只重启Management，Core/网关/Nginx/前端保持。
- 424实测互动决策/TTS正常、Core选A级切点，但`interaction-return` 409 no longer active，BRIDGE返回桥失效；抢答DIRECT可播。录音流式版本下BRIDGE强制改DIRECT，绕开返回桥。
- 缓存不是主因；互动TTS已生成且Core已选切点。五包测试、健康通过；待重开验证系统互动是否播出。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-direct-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-direct-670cd78.json`；回滚c8546f0。

## 历史生产（2026-10-22 09:12）

- 当前 `/opt/xiaolan/releases/20261022-interaction-pref-c8546f0`，源码 `c8546f0`；前驱 `20261022-recording-stream-safe-a6e887b`。Management PID1475731/SHA256 `7334cf9a3b46ca21da517c8e4bf2d4276fee2e3aaf3dfb26d4fae4ad7c6dc83b`；只重启Management，Core/网关/Nginx/前端保持。
- 排查423：102条chat、4次interaction-purpose，但无自动decision；`live_room_interaction_preferences`无行，疑Core用全0配置。已在开播时Push有效偏好（默认50/50/50/50/50，auto_heat=true）到Core；失败只记日志。
- 五包测试、8080/8081/8083健康通过；待重开验证是否产生互动decision与安全切点插入。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/interaction-pref-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-interaction-pref-c8546f0.json`；回滚目标a6e887b。

## 历史生产（2026-10-22 09:02）

- 当前 `/opt/xiaolan/releases/20261022-recording-stream-safe-a6e887b`，源码 `a6e887b`；前驱 `20261022-recording-stream-owner-ad9cfbd`。Management PID1472966/SHA256 `b70ee771f88f593baec2d31145f24455cc552d88944af5898a0f6552852526e8`；只重启Management，Core/网关/Nginx/前端保持。
- 场次422实测：08:56:29—09:01:20，9段；第0轮全部合成/追加，第1-2轮润色正常，无tts失败。流式分段与循环已验证。
- 互动失败根因：track没带timeline/safe_points，Core找不到35秒内安全切点，互动TTS排队不播。已给流式track补timeline/safe_points；润色>80字回退原文。
- 五包测试、8080/8081/8083健康通过；等重启后验证互动插入。说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-stream-safe-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-stream-safe-a6e887b.json`；回滚ad9cfbd。

## 历史生产（2026-10-22 08:55）

- 当前 `/opt/xiaolan/releases/20261022-recording-stream-owner-ad9cfbd`，源码 `ad9cfbd`；前驱 `20261022-recording-stream-logs-80b4e11`。Management PID1471464/SHA256 `b4a536f2d2410fef40f376f68fed57497dc851c220aac7421928be70dacfd65f`；只重启Management，Core/网关/Nginx/前端保持。
- 场次421启动2秒报“声音合成失败/context canceled”：owner检查只允许`ai_dynamic`，录音流式是`user_audio`，2秒检查取消TTS。已允许`user_audio + recording_voice_mode=stream_clone`。
- 版本192无需重存，直接重新开播；日志应出现`recording stream start/segment synthesized/appended`。五包测试、8080/8081/8083健康通过。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-stream-owner-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-stream-owner-ad9cfbd.json`；回滚目标80b4e11。

## 历史生产（2026-10-22 00:53）

- 当前 `/opt/xiaolan/releases/20261022-recording-stream-logs-80b4e11`，源码 `80b4e11`；前驱 `20261022-recording-stream-throttle-3aa0b04`。Management PID1470209/SHA256 `6b5c2780d5157c101055a841720a2e735787fd2345c948fa3e9068aabe313985`；只重启Management，Core/网关/Nginx/前端保持。
- 只加流式录音日志：开场分段数、每段合成/缓存命中、追加Core、润色P0拒绝，不改业务。用于验证 `stream_clone` 分段/缓存/前4轮润色/30-70轮换。
- 线上仍无 `stream_clone` 场次；最新版本191仍ai_dynamic，需用户重新选AI声音保存新版本后开播。
- 8080/8081/8083健康通过；未真实开播验证。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-stream-logs-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-stream-logs-80b4e11.json`；回滚目标3aa0b04。

## 历史生产（2026-10-22 00:49）

- 当前 `/opt/xiaolan/releases/20261022-recording-stream-throttle-3aa0b04`，源码 `3aa0b04`；前驱 `20261022-recording-clone-adopt-f651f4e`。Management PID1468800/SHA256 `2e0f3e8bf1d660dd92a0c11e18f5d9d819d75975c9bc5ccc6e854e61673501bc`；前端同步，Core/网关/Nginx保持。
- 录音主线新增“原声 / AI声音”单选；选AI声音保存`recording_voice_mode=stream_clone`。开播按录音原文40–80字切段，首段TTS后启动，后续逐段追加Core并缓存；前4轮逐段润色+P0，之后30%新/70%最近4轮缓存。
- 旧“整段生成后采用”入口保留但不再作为新流程；188旧版本无标记，需要重选AI声音保存新版本。
- 六包测试、8080/8081/8083健康通过；未真实开播验证，流式分段/润色/30-70缓存轮换仍待实际场次验收。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-stream-release-20261022`、`recording-stream-throttle-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-stream-throttle-3aa0b04.json`；回滚目标 f651f4e（前端）/46801f1（后端）。

## 历史生产（2026-10-22 00:26）

- 当前 `/opt/xiaolan/releases/20261022-recording-clone-adopt-f651f4e`，前端源码 `f651f4e`；前驱 `20261022-recording-custom-key-46801f1`。仅前端发布，Management PID1455515/SHA256 `bc56f606...`未变；Core/网关/Nginx保持。
- 修复版本188“标题是AI声音但audio_asset_id还是原录音139”：`adoptFullShowVoiceCandidate`对`user_audio`直接return，采用候选TTS被丢弃；现在录音主线也执行采用，CUSTOM音频切到克隆TTS。
- 现有188不会自动变；必须刷新后重新生成/采用/保存，新版本才会指向AI音频。前端构建、8080/8081/8083健康通过；未真实点击复验。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-clone-adopt-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-clone-adopt-f651f4e.json`；回滚目标前驱46801f1。

## 历史生产（2026-10-22 00:22）

- 当前 `/opt/xiaolan/releases/20261022-recording-custom-key-46801f1`，源码 `46801f1`；前驱 `20261021-recording-clone-58bad96`。北京时间00:22:21—00:22:29发布，Management PID1454803/SHA256 `bc56f60667231455d4869c666b4f50797c1de843529fac57af9d22e616ce55c1`；只重启Management，Core/网关/Nginx/前端保持。
- 修复“稿件编号无效 / 声音设置”：声音生成与SRT重建接口原先只接受A–E，录音主线使用`CUSTOM`被拒；现在两个接口都接受`CUSTOM`。
- 录音主线仍保持`user_audio`，克隆TTS走`CUSTOM`；录音原文不重写。五包测试、8080/8081/8083健康通过；未真实点击复验。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-custom-key-release-20261022`，部署记录 `/opt/xiaolan/deployments/20261022-recording-custom-key-46801f1.json`；回滚目标前驱58bad96。

## 历史生产（2026-10-22 00:18）

- 当前 `/opt/xiaolan/releases/20261021-recording-clone-58bad96`，源码 `58bad96`；前驱 `20261021-recording-save-878b075`。北京时间00:18:40—00:18:46发布，Management PID1454088/SHA256 `0a879cde443186c4a90c913fdf5bf2ade26d6852c6dc493e8c7003cd0ec342f7`；Core775073/网关775930/Nginx保持，前端同步更新。
- 修复“录音话术 + 克隆声音”：旧 AI 生成声音会切到已隐藏的 `ai_pregenerated`，导致切换不生效并报通用错误；现在录音主线保持 `user_audio`，只把 `CUSTOM` 音频替换为录音原文生成的克隆 TTS。
- 后端允许录音主线正式音轨为上传原声或 `live_agent_formal_voice_timeline` 克隆音轨；录音话术不重写，原始录音保留可切回。
- 前端/五包测试、8080/8081/8083健康通过；生产资源含“请先保留录音主线”标记。未做真实页面点击验收。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-clone-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-recording-clone-58bad96.json`；回滚目标前驱878b075。

## 历史生产（2026-10-21 23:58）

- 当前 `/opt/xiaolan/releases/20261021-recording-save-878b075`，源码 `878b075`；前驱 `20261021-welcome-prefix-a7cd7de`。北京时间23:58:19—23:58:26二进制发布，无进行中直播；Management PID1452281/SHA256 `185bd1d2363fdac2c9a953327c4b4766d4b4242ec327b762a1e193795088c435`；Core775073/网关775930/Nginx/前端保持。
- 修复录音模式保存：切换`user_audio`后，旧版本带过来的`content_pools`不再参与保存校验，直接落库；不需要旧“更新受影响内容”步骤，也不会再报“资料在生成期间发生变化”。
- 这是录音模式专用兜底；AI预生成/AI动态仍保留内容池一致性校验。当前系统只保留“初始化方案”，不再要求用户找“更新受影响内容”。旧已保存方案保持可用。
- 服务端五包测试通过；8080/8081/8083健康。未用真实页面点击复验保存回执。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/recording-save-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-recording-save-878b075.json`；回滚目标前驱a7cd7de。

## 历史生产（2026-10-21 23:07）

- 当前 `/opt/xiaolan/releases/20261021-welcome-prefix-a7cd7de`，源码 `a7cd7de`；前驱 `20261021-style-shuffle-aa4682b`。北京时间23:07:16—23:07:22发布，Management PID1450506/SHA256 `97a306fc5a264e2a020b826800d4282f443aaeb163ca55b658488448abc5868f`；只重启Management，Core775073/网关775930/Nginx/前端保持。
- 开场欢迎前缀移除：`opening-welcome-templates.json`36条全部由“欢迎欢迎{称呼}…”改为“{称呼}…”（生产校验0条以“欢迎”开头）；`text_generation.go`开场样板指令改为“不要再额外加欢迎前缀，直接从称呼或钩子进入”；MANIFEST同步新SHA256。
- 这是开场主线欢迎模板的“欢迎”前缀，不是互动欢迎策略；互动欢迎仍保留自然欢迎语义，若也要去掉需另改。
- `go test ./internal/httpapi ./internal/decisionexecutor ./internal/speechbase ./internal/db ./internal/model`在干净源码通过；8080/8081/8083健康。未用新直播验证开场首句。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/welcome-prefix-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-welcome-prefix-a7cd7de.json`；回滚目标前驱aa4682b。

## 历史生产（2026-10-21 23:00）

- 当前 `/opt/xiaolan/releases/20261021-style-shuffle-aa4682b`，源码 `aa4682b`；前驱 `20261021-style-id-fix-5184a12`。北京时间23:00:41—23:00:48二进制发布，确认无进行中直播；Management PID1449367/SHA256 `a5f1d2c501dc168e1d8a51a92857f3f12fede6867a8af447735a2736e1983d05`；Core775073/网关775930/Nginx/前端保持。
- 修复多选词固定顺序：`ShuffleStyleMDForPrompt`在每次主线/开场送模前，按同类（观众称呼、主播自指、短确认、方言等）打乱风格卡词序；一行内多个称呼词也打乱。模型不再总看到“朋友们”排第一。
- 交互称呼去掉程序对`anchorTerms[0]`的5倍加权，`anchorStyleAddressTerms`返回前随机打乱，朋友们/老乡们等候选等权。
- 场次405实测：前两段用卡正常，但“朋友们”反复出现、“老乡们”几乎不出现，验证了固定顺序/首词加权问题；本轮修复后需新场验证多称呼轮换。
- 干净源码`go test ./internal/speechbase ./internal/decisionexecutor ./internal/httpapi ./internal/db ./internal/model`通过；8080/8081/8083健康。未用新直播验证实际轮换。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/style-shuffle-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-style-shuffle-aa4682b.json`；回滚目标前驱5184a12。

## 历史生产（2026-10-21 22:49）

- 当前 `/opt/xiaolan/releases/20261021-style-id-fix-5184a12`，源码 `5184a12`；前驱 `20261021-diff-gap-msg-8a1707f`。北京时间22:49:10—22:49:16二进制发布，确认无进行中直播；Management PID1447998/SHA256 `4651288d477b0a9e11905a213c483dcff354ca45377538aef0dfa417c7230184`；Core775073/网关775930/Nginx/前端保持。
- 修复直播风格被清空：快照`style_id`是元提示词资产ID，旧校验误当主播ID比较，导致开播时清空styleMD。新增`bound_style_id`单独保存主播ID；只有它非0且与当前绑定不一致才清空。旧快照没有该字段时保留快照风格，不再误清。
- 根因证据：巴国布衣方案5/场次398，快照`style_id=14`、绑定主播id=1，旧代码判定不一致后清空；两段主线因此是通用口播。修复后需开新场验证；当前场次已结束。
- 建议重新初始化/发布一次方案，让新快照写入`bound_style_id`；旧版本176也可由新代码兼容使用风格卡。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/style-id-fix-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-style-id-fix-5184a12.json`；回滚目标前驱8a1707f。
- `go test ./internal/httpapi ./internal/decisionexecutor ./internal/db ./internal/model`在干净源码通过；8080/8081/8083健康。未用真实直播验证修复后的实际播报。

## 历史生产（2026-10-21 22:06）

- 当前 `/opt/xiaolan/releases/20261021-style-card-defaults-2557e7a`，源码 `2557e7a`；前驱 `20261021-style-card-live-9211c66`。北京时间22:06:01—22:06:02仅前端发布，未重启任何服务；Management仍PID1444504/SHA256 `c0d45edf9c27822e0e94246091bddc6e6f4189c5f43a78887b3ffa0d64e2583e`/VCS9211c66，Core775073/网关775930/Nginx保持。
- 修复切到“风格卡”但没有卡时显示空字段：前端自动填入并保存默认卡，字段包括人设“自然、口语、像在跟一个人说话”、观众称呼“朋友们、老乡们”、自称“我、咱们”、口头禅“我给你说嘛”、避免词“家人们、宝子、亲爱的”、短句节奏、热情60/专业55/促单40、普通话、补充“不主动推荐链接；事实没有的不补、不猜”。
- 去掉风格面板顶部说明和保存后说明，只保留字段、默认值和保存按钮；`pnpm run build`通过，生产index指向`index-8uPXVpWI.js`，资源含“我给你说嘛”默认值标记，8080/8081/8083健康。未做浏览器逐字段点击。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/style-card-defaults-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-style-card-defaults-2557e7a.json`；回滚目标前驱9211c66。

## 历史生产（2026-10-21 22:04）

- 当前 `/opt/xiaolan/releases/20261021-style-card-live-9211c66`，源码 `9211c66`；前驱 `20261021-style-card-live-163c9f7`。北京时间22:04:01—22:04:08二进制增量发布，Management PID1444504/SHA256 `c0d45edf9c27822e0e94246091bddc6e6f4189c5f43a78887b3ffa0d64e2583e`；只重启Management，Core775073/网关775930、Nginx、前端保持。
- 本轮修复风格卡真正可用于直播间：初始化时把卡的元提示词编译成方案资产，不再要求原始样本和样本评分基准；方案保存/开播允许“有风格卡、无评分基准”，本场不打分；实时互动称呼优先读取卡的称呼覆盖与 audience_address habits。
- `go test ./internal/httpapi ./internal/decisionexecutor ./internal/db ./internal/model`在干净源码通过；风格卡称呼回退单测通过；8080/8081/8083健康。未实际开播、未用真实模型/TTS验证整场。
- 使用顺序：主播切“风格卡”→保存卡→绑定方案→重新初始化→保存/开播；已有旧快照需重新初始化。评分基准不是风格卡必需，卡场次按不打分处理。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/style-card-live-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-style-card-live-9211c66.json`；回滚目标前驱163c9f7。

## 历史生产（2026-10-21 21:51）

- 当前 `/opt/xiaolan/releases/20261021-fact-diff-ddd5d30`，源码 `ddd5d30`；前驱 `20261021-anchor-style-mode-344ce55`。WorkBuddy于北京时间21:51:28—21:51:34发布事实差异修复，Management PID1443196/SHA256 `e3663f9fd4e13fd5be10a417ca54361a5101f6cfd9f00e7d41f548b6121e444e`；Core775073/网关775930、Nginx保持；前端继承本轮的风格来源版本，未重发。
- `ddd5d30`的父提交是`344ce55`，因此当前线上已包含：主播风格来源二选一（风格卡/风格样本）、`style_source_mode/style_card_json`、风格卡参数面板、中性/成对呼叫、问答直接承接、TTS句级P0等。`web/index.html`仍指向`index-D-3QmVFm.js`，资源含“风格卡”标记。
- 本轮风格来源发布（344ce55）的详细记录保留在下方历史段；当前生产由WorkBuddy事实差异发布承接，不能再用344ce55覆盖ddd5d30。

## 历史生产（2026-10-21 21:50）

- 当前 `/opt/xiaolan/releases/20261021-anchor-style-mode-344ce55`，源码 `344ce55`；前驱 `20261021-fact-diff-452c576`。北京时间21:49:52—21:49:58发布，Management PID1441973/SHA256 `20a36ab9717a835fb9ac56534fa1f8df14cb679b2c86f1eced7a4253584ce797`；只重启Management，Core775073/网关775930保持，Nginx不变；前端web/web-desktop同步更新。
- 本轮补齐主播“风格来源”二选一：`style_source_mode=card|sample`，新增`style_card_json`；样本仍用`profile_json`，风格卡单独存。`GetLiveAnchorStyle`按来源返回生效profile，绑定/生成链路读取生效profile；已有样本的主播也可切风格卡，两套profile不互相覆盖。
- 前端左侧主播卡新增“风格”下拉（风格卡/风格样本），左侧标签“主播风格”对应风格卡，“风格样本/分析结果”对应样本；风格卡显示自定义参数页，样本显示旧样本页。新建主播默认风格卡并写入默认参数；去掉空态建议；页面文字统一16px。
- `go test ./internal/db ./internal/httpapi ./internal/model`、风格卡来源单测、`pnpm run build`通过；生产index指向`index-D-3QmVFm.js`，资源含“风格卡”标记；8080/8081/8083健康通过。未做浏览器逐字段点击。
- 说明 `E:/直播伴播/docs/2026-10-21-主播风格来源与风格卡.md`，证据 `E:/直播伴播-local/anchor-style-mode-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-anchor-style-mode-344ce55.json`；回滚目标前驱fact-diff-452c576。已绑定方案切换来源后需重新初始化/保存快照才生效。

## 历史生产（2026-10-21 21:31）

- 当前 `/opt/xiaolan/releases/20261021-anchor-style-panel-cac613b`，前端源码 `cac613b`；前驱 `20261021-convention-top-d25bdd6`。北京时间21:31:03—21:31:05前端增量发布，未重启任何服务；Management仍PID1438252/SHA256 `08afa88429e5ec639f5331ffac7192f372b292e43865dfdca5803ee9c1ef26c5`/VCS `0ca61d1`，Core775073/网关775930保持，Nginx不变。
- 本轮仅前端：主播风格页新增“主播风格”标签（在“风格样本”左侧），提供人设、观众称呼、主播自称、口头禅、避免词、句子节奏、热情/专业/促单强度、地域口音、补充说明；保存时程序生成`anchor-delivery/v1`规范与称呼覆盖，不向用户暴露JSON。已有样本分析的主播锁定手动覆盖，需新建无样本主播。
- 复用了现有`PUT /api/v1/live-anchor-styles/{id}`，未改后端/数据库；`pnpm run build`通过，生产`web/index.html`已指向新`index-aaV6mHGf.js`，资源内含“不导入文本也能设定”标记；8080/8081/8083健康通过。未做浏览器逐字段点击验收。
- 证据 `E:/直播伴播-local/anchor-style-panel-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-anchor-style-panel-cac613b.json`；回滚目标前驱d25bdd6。
- 当前Management `0ca61d1`已包含之前的称呼中性/成对、问答直接承接和问题prompt修复；本前端发布没有改Management二进制。

## 历史生产（2026-10-21 21:01）

- 当前 `/opt/xiaolan/releases/20261021-address-bridge2-9be7150`，源码 `9be71507e9710fc2ce38718011ba65c9e48adac5`（`c80469f` + 问答prompt修复）；前驱 `20261021-address-bridge-c80469f`。北京时间21:01:19—21:01:25发布，Management PID1436621/SHA256 `85d2a25eae99ef4473401a44efbe222ce2689d1d4f76629386108f93f5823b42`；只重启Management，Core775073/网关775930、Nginx、前端保持。
- 本轮范围：称呼池加入中性/男女成对词（哥姐、哥哥姐姐、叔叔阿姨、兄弟姐妹、老乡、老板、朋友、家人），单性别词入池自动成对化；`爸爸/妈妈/老公/老婆/主人`等语义越界词继续拦截；事实问答改为一次“称呼+答案”，无事实直接边界，清掉“问得好/我这就/马上/稍等”空转承接；`interaction_expression`删掉“问得好”示例。
- 真实数据对照：租户14/房间29/方案5，旧记录9条全部是“问得好+我这就”空转，新探针9条全部“称呼+直接内容”；探针把11条事实全给模型，只证明承接结构，不代表事实准确率。说明 `E:/直播伴播/docs/2026-10-21-称呼与承接真实数据对照.md`。
- `decisionexecutor`全量、定向20轮、发布守卫、8080/8081/8083健康通过；Management全量被WorkBuddy `8decefc` 的anchorcycle对比任务用例阻断（非本轮改动，未修）。未用真实TTS、未开播。
- 证据 `E:/直播伴播-local/address-bridge2-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-address-bridge2-9be7150.json`；回滚目标前驱c80469f。WorkBuddy的`1dc00db`（直播间规范/自家称呼）未包含在本发布。

## 历史生产（2026-10-21 20:02）

- 当前 `/opt/xiaolan/releases/20261021-tts-p0-guard-c51c883`，源码 `c51c8832daae286d63e94d542dd2727635f18048`；前驱 `20261021-p0-minimal-edit-91cffdc`。北京时间20:01:57—20:02:03发布，Management PID1429088/SHA256 `4d8746a84a9016e265e51aa6183ea4ad2438056c46ff8e4c4f11839cdca0a6a9`；只重启Management，Core775073/网关775930及程序哈希、Nginx保持。
- 本轮是TTS前句级P0改写的业务锚定补丁：命中句只回喂本句，明确列出违规词，给正向替换对照，不注入轮盘卖点/事实；程序只选零P0命中且保留原“N号链接”、不新增数字的候选；仍不行丢句，全丢走安全边界；verbatim跳过。生产二进制已核验含 `interaction_p0_sentence_rewrite`；`91cffdc`的轮盘P0最小编辑和`33cefef`租户维护入口一并保留。
- 20轮定向、`decisionexecutor`全量、Management全量`go test ./...`、发布守卫及8080/8081/8083健康检查通过。未用真实模型/TTS、未开播；真实模型20次改写仍未跑。
- 说明 `E:/直播伴播/docs/2026-10-21-TTS前句级P0改写.md`，证据 `E:/直播伴播-local/tts-p0-guard-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-tts-p0-guard-c51c883.json`；回滚目标前驱91cffdc。

## 轮盘 P0 最小编辑（2026-10-21 19:55，历史记录）

- `/opt/xiaolan/releases/20261021-p0-minimal-edit-91cffdc`，源码 `91cffdcdd8169cb6cb80ab40adb4fe22a137b9de`；前驱 `20261021-tenant-agent-33cefef`。WorkBuddy 于19:55:04—19:55:10发布，Management PID1427888/SHA256 `a6b203708c35d8fd80ab633e25571cc37f217591295d4ec58a916a3c4ef78fb5`；Core775073/网关775930及程序哈希、Nginx保持。
- `91cffdc` 的父提交是 `8fcc6dc`（TTS前句级P0改写），当时线上同时包含租户维护智能体 `33cefef`、TTS前句级P0 `8fcc6dc`、轮盘P0最小编辑 `91cffdc`。该版本已被 `c51c883` 取代，不能再覆盖当前线上。

## 租户维护智能体（2026-10-21 19:44 已上线，历史记录）

- 用户要求完善现有“小蓝直播搭子”作为租户维护入口并提交部署；实现 `33cefef6f483f8658a4477014448a84ce2ce03c0`。当前 `/opt/xiaolan/releases/20261021-tenant-agent-33cefef`，北京时间19:44:33—19:44:49上线；实际前驱WorkBuddy `20261021-wheel-p0-rewrite-812cfcb`。
- 本轮范围：租户 `client-agent` 读取当前方案事实/福利/商品/概况；包邮按发布快照1/2/3、地区未写走边界；system域写命令复用`live_strategy_intent`+`live_strategy_actions`，覆盖事实/福利/商品/话术/方案切换，走“预览→确认→执行→版本号”；动作卡文字确认/取消、plan+room旧卡失效、方案选择器、方案绑定房间优先、写失败纯中文。
- Management PID1426324/SHA256 `d89d6e74ab6cb60d3b97bc8664549f4ce1101b6df11ac6d8c3e6372768fe33b1`；只重启Management，Core775073/网关775930、Nginx、母版/清单和客户资产保持。发布包83文件（1二进制+41前端×web/web-desktop+元数据），current文件4409。
- Management全量`go test ./...`、前端`pnpm run build`、9项前端辅助测试、发布守卫及8080/8081/8083健康检查通过；公网电脑/手机/Core音频200，`client-agent`未登录401，新前端标记存在。未用真实租户账号做动作卡/版本号/跨租户联调，未调用真实模型/TTS或开播；客户手机端/销售端不在本轮产物。
- 说明 `E:/直播伴播/docs/2026-10-21-租户维护智能体上线.md`，证据 `E:/直播伴播-local/tenant-agent-release-20261021`，部署记录 `/opt/xiaolan/deployments/20261021-tenant-agent-33cefef.json`；回滚目标前驱812cfcb，已准备未执行。编辑结束并交接WorkBuddy；下方“当前”均为历史时点。

## 用户错误提示仅中文（2026-10-21 18:06 已上线，历史记录）

- 最新用户明确要求错误提示只能中文，不能泄露英文和内部代码；覆盖下方首次诊断上线的展示约定。弹窗、内嵌错误、读取失败警告、错误记录及复制内容显示中文真实原因/处理建议和公开场次；不显示接口路径、方法、英文枚举、技术状态码、请求编号、源码位置及堆栈。已知声音/连接/超时/上游拒绝等分别转中文，未知原因明确缺少可显示的中文原因，不能猜成声音故障；服务端原始诊断和业务决策保持。
- 前端实现89a431a/收尾6abe5dc；18:06:37—18:06:38上线 `/opt/xiaolan/releases/20261021-error-chinese-6abe5dc`，实际前驱WorkBuddy free-style-open-074f98f。运行Management仍1417665/SHA256447bebb6e6eda1d8ae97c929ede4ccb06628c7f0705bb9c7149cc3f3d88ac7ba/VCS074f98f；Core775073/网关775930及哈希保持，三服务无重启。不能按前端源码6abe5dc推断后台其它新提交已发布。
- 固定干净源码类型/构建、15项定向、浏览器中文原因/复制/记录/五宽度通过；全前端140项123过17原基线旧失败，无新增，不能称全套通过。旧前端96产物与生产复现一致，4149旧无关文件、母版/清单、其他前端及Nginx保持；公网/五鉴权/含销售小智标准检查通过，3317完整前端备份及E盘指针更新。
- 说明仍用 `E:/直播伴播/docs/2026-10-21-系统真实错误诊断显示.md`，本轮证据 `E:/直播伴播-local/error-chinese-release-20261021`，持久附件 `/opt/xiaolan/deployments/20261021-error-chinese-6abe5dc/`；保留完整实际前驱及前端守卫回滚，未实际回滚。无客户资料/提示词/队列修改、真实模型/TTS或恢复监控。编辑结束并已交接WorkBuddy，后续同文件重新登记；下方“当前”均为历史时点。


## 系统真实错误诊断显示（2026-10-21 17:32 已上线，历史记录）

- 当前 `/opt/xiaolan/releases/20261021-error-diagnostics-be31326`；固定干净源码be31326302736d7f87801fad35d78b05955b7b80，含诊断a8c7527/19f09c0与WorkBuddy最新初始化、可选/取消风格f0ba138/27ea93e/cbf9a9a/be31326。实际前驱是刚发布的style-unbind-be31326，原守卫先拒绝旧基线后重新整合，不能回到下方历史发布覆盖新修复。
- 具体原因、环节、错误码、场次、HTTP状态/请求编号保留并脱敏；供料错误链、声音状态、文字/TTS/Core交付、通用request、组件/Promise错误分别接通；前端支持复制及本页最近20条记录。历史仅system_error无详情明确说明信息不足，不伪造旧根因。正常停播/历史场隔离及Core不盲目重发边界保持。
- 17:32:25—17:32:29上线；Management1415672/SHA256 a8eb31db220fcb5e8a03cffd962a7a5d0155186861af5946a2820435062b5a2d/vcs.modified=false。三次全场/Core空闲守卫，仅重启Management，Core775073/网关775930和哈希保持；4105旧无关文件、母版/清单、Nginx、系统设置/模型池及13类客户资产保持。
- 固定源码1719测试通过/104显式环境或付费探针跳过/0失败，Linux编译、前端类型/构建、浏览器五项及公网/五鉴权/标准生产检查通过。前端138项121过17旧失败，与原线上基线逐项一致无新增，不能称全套通过。3227完整前端哈希/大小/权限备份和E盘权威指针已更新。
- 说明 `E:/直播伴播/docs/2026-10-21-系统真实错误诊断显示.md`；证据 `E:/直播伴播-local/error-diagnostics-release-20261021`，持久附件 `/opt/xiaolan/deployments/20261021-error-diagnostics-be31326/`。完整实际前驱及守卫回滚已备未执行。无真实开播/付费模型/TTS或恢复监控；业务源码编辑结束并已交接WorkBuddy，后续同文件改动重新登记。下方各“当前”只描述对应历史发布时点。


## 主线成品复用闭环（2026-10-21 10:15 已上线，历史记录）

- 用户要求按最新设计完善并用起复用；实现 `a4209888eb7b3fb16016359e5b645cf569931bba`。按当前SkipReview签发真实采纳凭据，审核开启时保留真实审核回执验证；不能伪造模型通过。不同侧面库存共存，有TaskFactScope时按相关任务事实失效，音色/语速仅换声音。保留每场首次开场实时文字/TTS及后续滚动七三，不叠乘旧容量概率，不自动开互动复用。
- 当前 `/opt/xiaolan/releases/20261021-mainline-reuse-a420988`，北京时间10:15:46—10:15:50上线；Management1395851、SHA256 `e23c741bcf9c570b35f5166153f8749158537713b2734008ae4ee06ffc7d4582`、vcs.modified=false。实际前驱是WorkBuddy刚发布的remove-semantic-review-6c6827a（Management1394464/45c3ceab），已保留其最新后台及完整前端。Core775073/网关775930及程序哈希保持，不按前端/目录名称推定后台。
- 持共享锁与三次全场/Core空闲、完整指纹守卫，仅替换管理程序并新增来源说明；4023旧无关文件、3101完整前端、实际191母版/清单、Nginx、模型池设置和13类客户资料保持，无生产业务迁移/方案修改。完整前端哈希/大小/权限备份及E盘权威指针已更新。
- 干净固定源码1825项通过/114显式环境与付费探针跳过/0失败；真实随机前缀MySQL九项、实际生产母版六项闭环、独立10公网哈希/5鉴权及含销售/小智标准生产检查通过。1000循环暖库4196/5994只是策略模拟；10:19尚无新直播，不能宣称已真实命中或播完。新库随正常开播积累，不强行导入旧无证音频，不主动调用付费模型/TTS、开播或恢复旧监控。
- 说明 `E:/直播伴播/docs/2026-10-21-主线成品复用闭环修复.md`，证据 `E:/直播伴播-local/mainline-reuse-closure-20261021`；持久附件 `/opt/xiaolan/deployments/20261021-mainline-reuse-a420988/`。守卫回滚保留完整WorkBuddy前驱，仅重启Management，已准备未执行。源码和记录编辑结束并交接WorkBuddy，后续同文件修改重新登记。


## 方案顶部信息精简（2026-10-20 23:05 已上线）

- 用户要求移除顶部“有待保存资料”“本场加载/未开播”“版本记录”；前端`ba2606f8427370f7e298a22d0aaf452137685115`仅修改本页模板，保留绑定数量/方案编号/最新保存版本、真实保存反馈和历史读取，其它页面状态保持。
- 当前前端发布`/opt/xiaolan/releases/20261020-plan-header-ba2606f`，实际前驱是WorkBuddy新发布`20261020-remove-total-constraint-5ba5c9a`。以其完整现场持共享锁/指纹守卫，只替换两入口、新增40资源及来源说明，3893旧文件、母版清单及删旧结果、其它前端和Nginx保持。
- Management已由WorkBuddy更新为PID1341273、SHA256`91fdb9ef7d7744dcee7a86f10bd245863fb48fcaaf8f345cba2597f532374850`、VCS5ba5c9a；下方1334113属历史状态。本次三服务未重启，Core775073/网关775930及程序哈希保持。前端发布名不能当成后台版本。
- 前驱两套各49产物一致，固定候选类型/构建/10项原回归、12公网哈希/5鉴权及含销售小智标准检查通过；实施阶段19项保存检查17过2旧失败与基线一致，未修改保存逻辑/旧测试。3015完整前端备份及指针已更新，未操作真实方案/绑定、调用模型/TTS、清理服务器或恢复监控。
- 回执`E:\直播伴播\docs\archive\2026-10-21\history\2026-10-20-方案顶部信息精简.md`；证据`E:\直播伴播-local\plan-header-simplification-release-20261020`及服务器同轮`/opt/xiaolan/deployments/20261020-plan-header-ba2606f/`。守卫前端回滚已准备未执行，后续仍按actual current保护并行更新。

## 设备绑定完整回显（2026-10-20 22:44 已上线）

- 用户要求退出重进页面时真实还原设备绑定。前端`320929e95cbfa01b1b823da907e28050cf918828`在初次/切房重建时恢复本房全部已绑定设备，主设备优先、角色读接口；额外空行仍手动增减，不写浏览器绑定副本或隐式改角色。
- 当前前端发布`/opt/xiaolan/releases/20261020-device-binding-restore-320929e`，实际前驱initialize-plan-2ac2d16。持共享锁复核现场指纹，只替换两入口、新增40资源和来源说明；3854旧文件、WorkBuddy后台/母版/MANIFEST、其它前端与Nginx保持，三服务未重启。
- Management仍1334113/bfcd1dd2/VCS755eb0c，Core775073、网关775930及程序哈希保持。不要把前端发布名当后台版本；工作区并行后台改动未纳入本次发布。
- 前驱web/桌面各49产物一致；固定候选浏览器reload/切房/角色/解绑与旧回执隔离、类型/构建、10项原回归通过；12公网哈希、5鉴权及含销售小智标准检查通过。2975完整前端备份和权威指针已更新。未修改真实绑定、开停直播、调用模型/TTS或恢复监控。
- 回执`E:\直播伴播\docs\room-device-bindings.md`；证据`E:\直播伴播-local\room-device-binding-restore-release-20261020`，服务器同轮持久附件位于`/opt/xiaolan/deployments/20261020-device-binding-restore-320929e/`。守卫前端回滚已准备未执行；小蓝盒子待机掉线未由本次处理。

## 初始化方案按钮改名（2026-10-20 22:02 已上线）

- 用户要求“初始化直播智能体”改名为“初始化方案”，相关三处页面引导同步；仅文案，保留增量初始化、下拉全部重置和真实进度。不要依据下方历史记录恢复旧名称。
- 当前前端发布`/opt/xiaolan/releases/20261020-initialize-plan-2ac2d16`，源码`2ac2d16cf5fbee0bb49d935f3665dc8118a8d15a`。以实际前驱live-mode-775d5a9完整现场为基线，持共享锁并复核文件/进程/Nginx后只替换两入口、新增40资源和来源说明，3813旧文件保持。
- 三服务未重启：Management1334113/bfcd1dd2/VCS755eb0c、Core775073、网关775930及程序哈希保持；WorkBuddy语义桶母版和MANIFEST、其它提示词、客户/销售移动端与Nginx保持。前端发布名不能当成后台版本。
- 前驱两套各49产物一致，候选类型/构建/10项回归、12公网哈希/5鉴权及含销售小智标准生产检查通过；2935完整前端备份与权威指针更新。没有真实方案操作、模型/TTS调用、盒子修复或恢复直播监控。
- 回执`E:\直播伴播\docs\archive\2026-10-21\history\2026-10-20-初始化方案改名上线.md`；证据`E:\直播伴播-local\initialize-plan-rename-release-20261020`，服务器同轮附件位于`/opt/xiaolan/deployments/20261020-initialize-plan-2ac2d16/`。只切前端的守卫回滚已准备未执行；后续继续核对actual current和并行更新。

## 直播模式入口（2026-10-20 20:56 已上线）

- 用户要求将顶部“生成与预览”改为“直播模式”，放在下方“声音”右侧。前端 `775d5a91622627f76dcd005116ac87e39a9e35b4` 仍进入原fullshow页面，保留业务和初始化组合按钮；不要恢复顶部旧入口。
- 当前发布 `/opt/xiaolan/releases/20261020-live-mode-775d5a9`，实际前驱是WorkBuddy语义桶发布 `20261020-semantic-bucket-755eb0c`。本次仅前端，持共享锁捕获并核对完整现场后切换两个入口，新增40资源/来源说明；3772旧文件、判断母版及MANIFEST、其它提示词、客户/销售移动端和Nginx保持，三服务均未重启。
- Management仍PID1334113，SHA256 `bfcd1dd2e372a4579adf1f4b7a10b14d8ebfddf4d14b8597c1415f2fc4047493`、构建VCS755eb0c；Core775073/网关775930及程序哈希不变。不要按前端发布名推断后台版本。下方19:19的1322582/2f4369c4属于历史状态。
- 前驱web/桌面各49产物复现一致；候选类型/构建、10项回归、9组合成交互六宽度通过。12公网哈希、5鉴权及含销售/小智的标准生产检查通过；完整2895文件前端备份及权威指针更新。没有开播、模型/TTS或真实方案业务操作。
- 说明 `E:\直播伴播\docs\archive\2026-10-21\history\2026-10-20-直播模式导航入口.md`；证据 `E:\直播伴播-local\live-mode-release-20261020`；服务器持久附件同名目录位于 `/opt/xiaolan/deployments/`。守卫前端回滚已准备未执行。后续发布继续以实际current为准，不覆盖WorkBuddy并行更新。

## 初始化与重置组合按钮（2026-10-20 19:19 已上线）

- 用户要求把初始化与重置收为一个组合按钮：主按钮“初始化直播智能体”单行，仍走增量；右侧箭头仅展开菜单，“全部重置”触发原 forceInitialize / force=1 业务。处理中同时禁用两个区域，保留真实进度。不要恢复独立红色重置按钮或把箭头点击直接变成重置。
- 当前前端发布 `/opt/xiaolan/releases/20261020-initialize-split-cc9144a`，代码 `cc9144a17a351b02a00e93b758f4d16954e22bc5`。以实际 WorkBuddy 前驱 three-brothers-switch-2f4369c4 为基线，先复现 web/桌面各49产物，再持共享锁继承现场，仅替换两个入口并新增40资源；3730旧文件、提示词、客户/销售移动端与Nginx保持。
- 本次无服务重启或后端发布：Management仍1322582/SHA256 `2f4369c49b3aa8baaf705d857efd582c755ea8701b9575595aec8dd25e9bf4b4`，构建VCS 7a0fd31+dirty；Core775073/网关775930保持。发布名cc9144a只描述本次前端来源，不表示语义桶或本地未提交后台已上线。
- 9项回归、固定源码类型/构建、实际导航父组件7组交互及六宽度检查通过；12公网哈希、5鉴权及含销售/小智的标准冒烟通过。2855完整前端字节/大小/权限备份核验完成，权威指针 `E:\直播伴播-local\frontend-preservation\latest.json` 已更新。说明 `E:\直播伴播\docs\archive\2026-10-21\history\2026-10-20-初始化与重置组合按钮.md`，证据 `E:\直播伴播-local\initialize-split-button-release-20261020`。原前驱保留，只切前端的守卫回滚已准备、未执行；后续部署仍重新检查 actual current 和并行更新。

## WorkBuddy原地更新后的发布保护（2026-10-19 12:52只读核查，覆盖下方旧运行版本说明）

- 用户明确要求：后续部署先检查服务器，不能覆盖WorkBuddy刚更新的内容。本次只读，未部署/重启/改生产文件。实际current名称仍为`20261019-rolling-scheduler-e93437b`，但Management已原地替换为PID1170098、VCS `1752bb6fa614cbc95c4c64db0e73c1b2f8f42f0a`、`vcs.modified=true`、SHA256 `b9ec504447dcf57df95164187e743ca6fdaf41b3a4fa93717d240497d6c94c8b`。不能用目录名、Git提交号或本聊天早先干净候选推断线上完整源码；下方PID1148393及二进制哈希已不是当前运行程序。
- 现场`style-meta-prompt-extraction.md`已更新且匹配E盘WorkBuddy未提交母版。3689发布文件已捕获指纹；相较上次发布捕获，管理程序/该母版变化，新增两份对应备份，未见文件删除，Nginx不变，Core775073/网关775930保持。后续以发布当时actual current重新整合，保留现场前端、母版和其它附件；锁内再次核对实际指纹，有并行更新即重做基线，不能强行按旧值发布。
- **本轮发现发布缺项**：实际`ANCHOR_STYLE_PROMPT_DIR=/opt/xiaolan/current/configs/prompts/anchor-style`缺少`first-live-opening.md`；当前二进制包含新的首次开场路径，明确配置目录不回退，会导致该路径准备失败。二进制亦含`loadGenerationP0`，但缺`compliance/L1/p0-generation.md`，无专用覆盖路径，生成侧加载同样会报缺失。没有调用真实模型/TTS或开播复现，不把静态检查当实播结果。后续须与WorkBuddy核对确切构建源码、补齐依赖和MANIFEST后联合验证，不只替换二进制或只看healthz。
- 说明：`E:\直播伴播\docs\2026-10-19-WorkBuddy更新后部署基线与缺失母版核查.md`；证据：`E:\直播伴播-local\predeploy-workbuddy-check-20261019`。旧“Codex尚未部署1752bb6”仍属历史操作事实，但不能据此断言当前服务器未含该代码。

## 滚动权重调度发布（2026-10-19 09:06 已上线，当前生产）

- 用户授权“部署”；北京时间09:06:41—09:06:44上线 `20261019-rolling-scheduler-e93437b`，固定干净源码 `e93437b04582c231b7a01bae2991c9b610ae4227`（实现 `1d655c7`），主目录仍为 `E:\直播伴播`。商品与卖点逐段按累计差额选择，普通排程的开场/讲解/收口共同参与连续两段限制；保留商品1.2、卖点1.3和公共20%，单一可用商品/已有续讲承诺例外。新场使用新逻辑，不自动初始化或保存方案。
- Management PID1148393，SHA256 `201307a556239ee2e7e72a750f3713393210562a3c017cca7b84aa7c792ab8e6`。持共享锁、三次全部会话/Core音频空闲及实际指纹守卫通过，只重启Management；Core775073/网关775930保持。完整前驱 `20261019-token-ac-565d63f` 保留，169现场母版及MANIFEST、2815完整前端、配置/模型池、Nginx和13类客户资产核验一致；3687文件仅替换管理程序并新增来源说明，3685旧文件保持。
- 干净固定源码完整Management1762项通过、110环境/付费探针跳过、0失败；Linux编译及VCS未修改标志通过。用户原V10/id128、同run_id离线1000循环6018段复演最大普通连续2，主体商品1461:1753、公共804/4018、同权重卖点876:877、20角度全用；不是实际新场播放验收。独立7公网文件/11鉴权及含销售/小智的标准生产冒烟通过，E盘完整前端保护指针更新；未开播或调付费模型/TTS。
- 修复/发布/回滚交接 `E:\直播伴播\docs\2026-10-19-滚动权重与防连续讲解修复.md`；证据 `E:\直播伴播-local\rolling-scheduler-release-20261019`；服务器持久18附件 `/opt/xiaolan/deployments/20261019-rolling-scheduler-e93437b/`，原始回执同名.json。专用回滚只重启Management，已准备但未实际执行。源码及本轮文档编辑结束并已交接供WorkBuddy查阅，后续同文件修改重新登记；本条覆盖下方旧生产版本。

## 主线token优化A+C二进制发布（2026-10-19 00:29 已上线，当前生产）

- 用户交接565d63f发布文档并要求教会WorkBuddy；北京时间00:29:34—00:29:37上线 `20261019-token-ac-565d63f`，代码改动7c4405f。固定干净565d63f构建，只改生成P0视图及renderCycleTextMessages环节指令位置；没有修交接所述prepend层3/4消息跳变，不声称整个供应商请求前缀已稳定或缓存收益已实测。
- Management PID1128401，源码 `565d63fe0e16a7a27f27ebf04096a0af66c42c53`，SHA256 `dd495622c0ef4565bb9cc4d450ce9e4ce57134084fea694686492a6443d0057b`。持共享锁、三次会话/Core空闲守卫后只重启Management；Core775073/网关775930保持。完整前驱 `20261019-prompt-boundaries-957e99b` 保留，169母版及MANIFEST原字节继承00:08修复，不用源码旧母版覆盖。3686文件仅替换管理程序/新增来源说明，3684旧文件、Nginx、配置/模型池和13类客户资产一致。
- 完整Management模块1742项通过、110环境/付费探针跳过、零失败，Linux干净构建通过；额外实际生产母版隔离输入契约验证9条适用P0内容保留，3150→2212字符，四种环节渲染system一致/动态指令进user。字数不是token或费用，真实命中率/退回率待新场。7公网文件/11鉴权、含销售/小智标准冒烟及2815完整前端哈希/大小/权限通过；E盘备份指针更新。未自动开播、保存方案、调付费模型/TTS或实际人工回滚。
- 教程/回执 `E:\直播伴播\docs\2026-10-19-给WorkBuddy-主线token优化二进制发布实操与回滚.md`；工具证据 `E:\直播伴播-local\token-optimization-release-20261019`；服务器持久附件 `/opt/xiaolan/deployments/20261019-token-ac-565d63f/`，原回执同名.json。专用回滚只重启Management，不能直接运行会重启Core/网关的通用rollback-release.sh。本轮文档编辑结束并释放供WorkBuddy查阅；当前状态覆盖下方旧生产版本。

## 母版授权句与审核规则增量发布（2026-10-19 00:08 已上线，当前生产）

- 用户授权按 WorkBuddy 准发布交接部署并交付操作教程；北京时间00:08:28—00:08:30原子切换为 `20261019-prompt-boundaries-957e99b`。仅从生产现场母版定点删改6文件7处及更新MANIFEST六项哈希；保留169份完整清单，其余163份母版/备份原字节不变，未用仓库136份覆盖。删除生成侧“肯定好吃”等授权与四层审核原第8/9条；有效P0原文件本次未改。不声称已经根治所有退回或违禁生成。
- 本次无需重编译或重启：Management `ANCHOR_STYLE_PROMPT_DIR` 指向 `/opt/xiaolan/current/configs/prompts/anchor-style`，加载器每次请求现读。运行程序仍是9698c4e、PID1115478/SHA256 `0ff49ea00ed66c0bd1f194b29d0ddf4b62cab0a7b6fd13810687443edd394af1`；Core775073/网关775930未重启。Management可执行文件仍引用前驱 `20261018-handoff-errors-9698c4e`，不得清理该目录。当前发布名957e99b表示母版交接来源，不冒充二进制源码版本。
- 持共享锁、三次全会话/Core音频空闲保护通过；3685文件仅7个变化，3678旧文件一致。29项相关测试通过，7公网文件/11鉴权及含销售/小智的标准生产冒烟通过；2815完整前端哈希/大小/权限、Nginx、设置/模型池与13类客户资产保持。E盘前端权威指针已更新，复用核验一致的完整前驱备份。未开播、改方案、调用模型/TTS或实际执行回滚。
- 教程/回执 `E:\直播伴播\docs\2026-10-19-给WorkBuddy-母版增量发布实操与回滚.md`；证据/脚本 `E:\直播伴播-local\prompt-permission-release-20261019`；服务器持久证据 `/opt/xiaolan/deployments/20261019-prompt-boundaries-957e99b/`。本次文档编辑结束释放供WorkBuddy查阅；后续修改重新登记。当前状态覆盖下方旧生产目录记录。

## 部署交接收尾（2026-10-18 22:18 已上线，当前生产）

- 用户授权部署交接；北京时间22:18:22—22:18:25上线 `20261018-handoff-errors-9698c4e`，Management源码 `9698c4efcbd17e7545401431291d4480e12e98e7`，唯一主目录 `E:\直播伴播`。本条覆盖下方旧生产管理版本。包含WorkBuddy事实冲突拆分提示、上游错误透传和P0母版调整，以及Codex补全阿里嵌套错误解析、FAQ音频前缀缓存比较和测试夹具收尾。不把删除词表当成已证明根治session311无声；技术失败退避仍待另行处理。
- Management PID1115478，SHA256 `0ff49ea00ed66c0bd1f194b29d0ddf4b62cab0a7b6fd13810687443edd394af1`。共享锁、三次空闲守卫通过，仅重启Management；Core775073/网关775930保持。3685文件中3681旧无关文件一致，两个前端仍为f6e3e0c原字节。两份生产独立母版、完整169份母版清单、Nginx、设置/模型池和13类客户资产保护核验通过。
- 干净固定源码构建完整质量脚本通过：Management1742、Core323、Gateway123，共2188项用例/子用例通过，110环境或付费探针跳过；三前端及Linux构建通过。15当前公网入口/资源、11真实方法未登录401及包含销售端/小智的标准生产冒烟通过，2815完整前端文件备份哈希/大小/权限核验及E盘权威指针更新完成。历史资源字节保留，不保证原本不对外提供的历史web资源URL可访问。
- 回执 `E:\直播伴播\docs\archive\2026-10-21\history\2026-10-18-部署交接收尾上线.md`，证据 `E:\直播伴播-local\handoff-release-20261018`。部署结束后用户自行开启session313，22:21:42启动、加载V9/id126及本管理版本，22:21:59.848收到Core实际开始回执；此后只读监控，不因收尾文档提交再次部署或重启。源码与上述收尾文档编辑结束释放，后续同文件修改重新登记交接。

## 联合调度与送播修复（2026-10-18 18:32 已上线，当前生产）

- 用户授权“部署吧”；北京时间18:32:56—18:32:59上线 `20261018-joint-delivery-f6e3e0c`，Management及普通/桌面前端源码 `f6e3e0ca665a67b6c1a077f8f4d4d7d599c83cd3` 已推送main，唯一来源 `E:\直播伴播`。本条覆盖下方旧生产版本记录；包含 WorkBuddy 材料可用性、公共family/侧面候选、审核容错及配套三份母版、事实矛盾提示，以及声音稳定指纹/确定未送出配额返还。Core接收结果不明不自动返还或重发，实际PCM仍另看回执。
- Management PID1106903，SHA256 `1b2f79cac1fe0729321adcc1509e01c218e20576deca16b904862a1300c5788e`；持共享发布锁，三次全场/Core音频空闲和实际文件/程序守卫通过。替换管理程序、两个前端入口、三份审核母版及清单，新增40前端哈希资源和来源说明；3684文件中3636旧文件保持，Core775073/网关775930、Nginx、系统设置/模型池及13类客户方案资产保持。未停止用户直播，未自动开播/保存方案/付费调用。
- 两份生产独立母版 `cycle-product-focus.md`（SHA256 `bb4bf4287574e34bb41bdd68f16067777990ba7ef05a3afc63835a5da6de4874`）和 `qwen-selling-point-angle-extraction.md`（`78dee441b6b8438d0870fffc8dc227d0fb12d64fc7a02398ddadf23cadd0f763`）原样保留，不用仓库旧文本覆盖；保留历史母版资源并核对实际169份完整清单。后续发布继续保护这些实际生产差异，不能把全部母版当成本次源码统一生成或直接全目录覆盖。
- 相关8包检查零失败（7包有测试，speechbase无独立测试），982项通过、60环境探针跳过，前端类型/生产构建和Linux管理程序构建通过；15项公网入口/资源、11个真实方法未登录401检查通过，2815个完整前端文件哈希/大小/权限备份核验通过。E盘权威指针及旧C盘发现入口已更新；未把离线或路由检查当成实际新场播放验收。
- 说明 `E:\直播伴播\docs\2026-10-18-联合调度与送播修复上线.md`，证据 `E:\直播伴播-local\joint-delivery-deploy-20261018`。旧版本d01e2d9目录保留；源码及本次三份收尾文档编辑结束并已交接给WorkBuddy查阅，原未跟踪讨论文档保留不代提交。后续同文件修改重新领取。

## 话术侧面材料筛选修复（2026-10-18 已上线，当前生产）

- 用户授权“开始实施发布”；北京时间15:55:29—15:55:33上线 `20261018-fact-side-material-d01e2d9`，Management源码 `d01e2d96cf9c6481bc4b2f826ebd143e2bcca5db`，唯一来源 `E:\直播伴播`。此条覆盖下方保存修复发布记录中的旧管理PID/版本，旧发布仍保留。
- `CategoryFromFactText` 在 `anchorcycle` 包内先解公共/商品归属前缀再取分类；JSON标签中的 `/`、`；`、`]`、引号可正确跳过，不import `httpapi`。正文内容策略和可说/改写共用侧面筛后事实，避免“完整关联事实”再注入；最终审核仍保留完整任务事实。行业侧面/分类仍按注入，商品/卖点/公共权重、原归属来源与四层业务未改。
- 话术库加入材料规则版本及行业侧面/映射指纹，旧规则生成的稿件不能继续命中；没有删话术库记录或改复用开关。真实V9状态9侧面回放材料由每次8条变为1～3条，仅预约核销侧面主动材料含用券/预约；历史正文仍标为避重上下文，不能声称全部消息中没有旧文字。880项测试通过、60跳过，管理命令及Linux构建通过；无真实付费模型/TTS调用或新场开播，实际话术效果待新场验证。
- Management PID1101221，SHA256 `db6c4cda06f5b6beda6fc1780049b2e6930d889071b644ed3e192fd669e8d48f`；共享锁、三次全会话/Core音频空闲和基线保护通过。只换管理程序并新增来源说明，3641旧文件、2775完整前端字节/大小/权限、13类方案资产、系统设置/模型池、Nginx保持；Core775073/网关775930未重启。12公网资源及11真实方法未登录401检查通过，E盘权威指针和旧发现入口已更新。
- 说明 `E:\直播伴播\docs\2026-10-18-fact-side-material-fix.md`，证据 `E:\直播伴播-local\side-fact-filter-fix-20261018`。已保存V9无需重新初始化即可在下次启动使用修复；不自动保存新版本/开播。WorkBuddy原未提交测试及两份保存方案待办文档保持，不代提交；后续按协作登记领取文件。

## 保存方案来源映射及保存入口修复（2026-10-18 已上线）

- 用户授权“部署上线”；北京时间15:11:54—15:11:57发布 `20261018-plan-save-source-mapping-cd389df`，源码 `cd389dfb70eb53a92b6aae5b4eff68cd08b6f493`，来源为唯一主目录 `E:\直播伴播`。本条生产状态覆盖下方目录整理时“尚未部署”的历史记录。
- 包含 Codex 真实事实来源/人工归属修复，以及 WorkBuddy 已提交的保存入口去前置更新和风险剔除回执。来源改为实际 `knowledge:N`，不再制造 `facts:line:N`；明确公共/商品范围保持，缺失或歧义来源返回可定位错误，内容池完整性校验保留。前端保存直接提交版本，内容池待更新/失败不再触发更新并等待。
- Management PID1098707，SHA256 `c27de1b8b79afb889337a7586ac8a9383f4776f5077d934ee687c87be1ae6653`。共享锁及三次会话/Core音频空闲检查通过，只重启Management；Core775073/网关775930保持。继承实际current全部资源，仅替换管理程序和web/web-desktop入口，新增40资源及来源说明；3598个旧文件、13类方案资产、系统设置/模型池和Nginx核验一致。
- 三个Go包945项通过、89跳过、零失败，实际9条输入离线来源回放及管理命令编译通过；Linux程序与前端生产构建通过。完整父页模拟接口验证待更新/失败状态均直接保存、重复点击拦截、保存失败重试、租户/方案载荷与手机无横向溢出。12公网入口/资源、11未登录鉴权及2775完整前端备份哈希/大小/权限通过；E盘权威指针与旧C盘发现入口均已更新。
- 旧发布保留可回滚；没有代客户保存新版本、改事实/商品或触发付费模型/TTS，不把模拟页面和离线回放当成生产登录实操验收。WorkBuddy未提交测试及两份原docs保留。部署说明 `E:\直播伴播\docs\2026-10-18-plan-save-source-mapping-deployment.md`，完整证据 `E:\直播伴播-local\plan-save-deploy-20261018`。

## 双方协助与同文件交接（2026-10-18 用户最新要求）

- 用户要求 Codex 与 WorkBuddy 都在 `E:\直播伴播` 最新版本上工作、相互协助；需要修改同一文件时必须交接。此规则适用于双方后续所有任务。
- 开始任务先读取 `E:\直播伴播\协作交接.md` 和 Git 差异，登记负责人、任务、完整文件路径、基线版本及状态。更新主线前先检查远端和未提交内容，保留对方工作，不能因追求“最新”覆盖正在进行的修改。
- 同一文件已有负责人时，先记录交接请求。交出方说明已完成/未完成内容、未提交差异和验证情况，并明确记录“已交接”；接手方重新读取当前文件及差异后再编辑。等待期间推进其它无冲突文件，不并行覆盖同一文件。
- 完成任务后在共享交接记录中更新状态、结果提交和下一步。提交只包含自己负责的改动，不替另一方提交、撤销或覆盖进行中的工作。共享文档是交接入口，不能仅凭 Git 工作区干净认定对方已完成。

## 唯一主工作目录：E:\直播伴播（2026-10-18 用户最新要求）

- 用户明确要求 Codex 与 WorkBuddy 合并代码、回归 `E:\直播伴播`。本目录的 `main`（跟踪 `origin/main`）是双方唯一主工作区；后续修改、测试、构建和提交均从这里开始。此条覆盖下方“工作在 Codex 副本”“不写入 E 盘”等历史约定。
- **双方同目录协作铁律（2026-10-18 用户新增）**：Codex 与 WorkBuddy 都在 `E:\直播伴播` 的最新 `main` 上工作，相互协助。若一方要改动的文件与另一方正在改动/刚改过的文件重叠，必须先交接确认，不得各自盲改后覆盖对方成果。动手前先 `git status` 与 `git log -1` 核对对方最新提交，避免基于旧副本覆盖新代码。
- 已将双方整合过的 GitHub main 基线3bc513a完整恢复至这里，961个旧中间态缺失文件已恢复；旧分支、未提交内容与其它副本均有可验证备份。`E:\speech-generator-build`、`E:\speech-generator-build-main` 和 Codex 下旧工作副本只作历史/草稿核对，不再作为发布来源。临时隔离和构建产物放 `E:\直播伴播-local`，完成后合回本目录。
- 本次没有修改保存方案的审核/事实/生成业务，没有更新生产服务。仅清理audio-service中一份完全相同的unregisterReceiver重复定义，使全部Go服务可以编译。管理后台、客户移动端、销售移动端和设备模拟器构建通过；音频服务测试、事实类型控件及完整方案页模拟接口五宽度验证通过。
- 管理服务全量回归1641个用例/子用例通过、109跳过、30失败（28个顶层），在迁移前原main副本重跑失败包得到完全相同的失败集合，没有新增。旧test-unified-plan-save脚本仍等待当前UI已移除的“更新受影响内容”按钮，超时已记录，不声称全量测试通过。
- 最新2735文件完整前端备份及权威指针已复制并核验至 `E:\直播伴播-local\frontend-preservation\latest.json`；旧C盘发现入口指向同一份E盘备份。生产仍为20261018-remove-navigation-tagline-0b90f61，Management4884904/PID1085092，Core775073与网关775930，未重启。不得以目录统一为由自动部署或变更商户资料。
- 交接说明见根目录 `docs\archive\2026-10-21\history\给WorkBuddy-代码已统一-2026-10-18.md`；完整备份、差异清单、回归日志和最终核对在 `E:\直播伴播-local\code-unification-20261018`。先阅读当次最新协作约定与实际运行状态，再实施后续业务任务。

## 删除导航装饰标语（2026-10-18 已上线）

- 用户要求移除导航右侧红线标出的“智能驱动 · 高效直播”及星光图标；已从PlanConfigurationNavigation删除对应元素和专用样式，避免额外占行。五个导航入口、初始化/全部重置及其业务保持，不调整其它标语。
- 北京时间12:21:13—12:21:14上线20261018-remove-navigation-tagline-0b90f61，仅换两个前端入口及新增40资源。修改前web/桌面各49产物与生产一致；构建、完整父页模拟接口/五宽度检查通过，12公网资源、9鉴权及2735完整前端备份核验通过，最新指针更新。3558旧无关文件、三服务PID/程序、Nginx及设置/模型池保持，无重启、生产事实写入或付费调用；WorkBuddy原目录不写入。证明本聊天work/remove-navigation-tagline-proof，说明docs/2026-10-18-remove-navigation-tagline.md。

## 事实类型单行显示（2026-10-18 已上线）

- 用户最新要求“直接一行”，覆盖下方旧完整换行要求。事实类型保持黑色16px和固定42px高度；类型列216px，常见长名称完整单行，超长名称省略，悬停及原生下拉保留完整名称。圆角/底色/箭头与本行归属范围协调，表内最小宽度1296px保留原正文宽度。仅样式修改，分类来源/保存CAS/建议分类/反馈弹窗及原事实业务保持。
- 修改前web/桌面各49产物与生产哈希一致。构建、现有控件和完整父页五宽度浏览器验证通过，含单行高度/相邻同底色/完整标题、保存失败和原筛选排序/编辑新增；模拟接口无生产写入和付费调用，不冒充已登录人工验收。
- 北京时间12:03:33上线20261018-fact-type-single-line-99b4592，仅换两个前端入口并新增40资源；3517旧无关文件、三服务PID/程序、Nginx及设置/模型池保持，无重启。12公网资源、9未登录鉴权与2695完整前端备份哈希/大小/权限通过，最新指针更新。Management仍4884904/PID1085092，不回退分类接口。说明docs/2026-10-18-fact-type-single-line.md，证明本聊天work/fact-type-single-line-proof。WorkBuddy原目录不写入。

## 事实分类反馈排版规范（2026-10-18 已上线）

- 用户要求整理事实类型的建议/错误回显，避免窄列长红字撑高整行。表内只显示16px短状态胶囊“待确认/分类失败/保存失败”和保存中状态；点击胶囊打开当前类型/建议类型/中文原因说明，支持Escape、焦点返回和小屏滚动。关闭说明不确认、不写入分类。保留原始响应，不把英文枚举/技术错误原样排在表内；顶部提示只显示简短结果。
- 保留手工保存CAS、低把握不自动保存及高把握建议保存、筛选排序搜索、正文原位编辑新增、加载禁用和失败保留；方案切换后的迟到分类错误不回显到新方案。没有后端、事实数据、行业枚举或模型提示词修改，不恢复批量分类UI或商品导入。
- 以已验证前端原始字节重建，普通web/桌面各49产物哈希与生产相同后修改；构建、长原因不撑高/中文说明/错误重试/键盘焦点/五宽度及完整父页原功能回归通过。模拟接口无生产写入或付费调用，不冒充已登录页面人工验收。
- 北京时间11:51:32—11:51:33上线20261018-fact-type-feedback-4431ba8，只换两个前端入口并新增46资源；3470个旧无关文件、三服务PID/程序、Nginx及设置/模型池保持，没有重启。12公网资源、9未登录鉴权和2655个完整前端备份哈希/大小/权限核验通过，最新指针已更新。实际Management仍4884904/PID1085092，不能退回缺分类接口的后端。说明docs/2026-10-18-fact-type-feedback.md，证明本聊天work/fact-type-feedback-proof。

## 事实类型保存及建议分类接口恢复（2026-10-18 已上线）

- 用户要求修复手工切换事实类型的“请求失败”。11:06—11:07 的 PUT 分类请求返回404，建议分类POST返回405；实际后端构建漏了WorkBuddy已接入的接口。先捕获当前E:/speech-generator-build-main七个已部署差异为4fcf16e，重建17246个代码符号大小一致；没有将model-pool-source进行中的提示词/卖点池修改整体发布，两处原目录均不修改。
- 恢复分类PUT及建议分类POST，事务CAS、租户/方案/活动事实隔离、category修订审计及幂等保留；分类变更不重整四层、不改正文/归属。GET、保存校验及建议共用当前全局+行业+租户自定义目录，不写死行业枚举，不恢复批量分类UI或商品导入。GET按方案与账号验证范围，客户无需重复传tenant_id。
- 北京时间11:37:36—11:37:39上线20261018-fact-category-save-4884904，后端源码4884904、Management PID1085092/SHA256 daed9629afebad0f1e070241b46447619d3bb787668501181d474f4caa85b71f；共享锁及三次AI/Core空闲守卫通过，仅重启Management。3470个其它文件、2609个前端及完整备份、13类方案资产、系统设置/模型池、Core775073/网关775930及Nginx一致。
- 965项Go回归通过，88环境跳过，无失败；本地隔离MySQL已跑真实HTTP分类读取/保存、CAS/越权/非法值及正文/四层/归属保留，存储并发/审计回滚/幂等通过。生产只读和未登录401校验，不冒充已登录人工验收或真实模型调用。说明docs/2026-10-18-fact-category-save.md，证明本聊天work/category-save-error-proof，最新前端备份指针已更新。
- 后续后端发布必须保留 PUT /api/v1/live-agent-plans/{planID}/facts/{factID}/category 及 POST /api/v1/live-agent-plans/{planID}/facts/suggest-types；发布验收按真实方法检查未登录401，不能只检查healthz，不能从缺接口的旧E盘目录覆盖当前后端。

## 事实类型中文回显修复（2026-10-18 最新用户要求，已上线）

- 用户要求修复已采纳事实的类型回显。WorkBuddy 已接入真实逐条分类和右键“建议分类”，覆盖下方“空禁用占位”历史要求；不能回退这些功能，也不能恢复已取消的批量类型或商品导入。类型未匹配不擅自改成其它，客户事实/分类原值保留。
- 运行中的 Management SHA256 为84a05d59c010c418dce1c02da244a0cc526c891e1d7aa2243dcb85e8056758bd，对应E:/speech-generator-build-main中的行业合并函数要求query同时有plan_id与tenant_id。只有plan_id会静默返回全局分类，导致行业标识英文回显。前端分类读取及重试/刷新/辅助入口显式带当前方案与目标租户，客户会话可兜底；pending和迟到隔离也包含租户。不要仅依据旧model-pool-source后端源码判断实际运行逻辑。
- 原生事实类型下拉保留保存API、CAS、键盘和可访问名称，已选中文用覆盖层完整换行，黑色16px。加载或读取失败禁用操作；未知值显示未匹配分类并保留原标识提示。原筛选、搜索、排序、归属、四层详情及原位新增/修改保持。
- 先按实际WorkBuddy前端原始字节重建，两个入口49/49产物哈希全相同后再修改；只捕获已上线的8个前端文件变化，不带其进行中的后端/提示词。E:/speech-generator-build及E:/speech-generator-build-main均不写入，原model-pool-source的414个前端源文件指纹未变。
- 北京时间11:00:55—11:00:56发布20261018-fact-category-display-27921a7，仅换普通web/桌面入口和新增40个资源；3428旧无关文件、三服务PID/程序、Nginx及设置/模型池一致，无重启。12公网入口/资源、7鉴权检查和2609个完整前端备份的哈希/大小/权限核验通过，共享指针已更新。构建、分类/租户/迟到隔离专项、控件异常与完整父页五宽度回归通过；模拟接口无生产写入或模型/TTS调用，不冒充已登录真实页面人工验收。说明docs/2026-10-18-fact-category-display.md，证明本聊天work/fact-category-label-fix-proof。

## 事实分类请求携带当前方案（2026-10-17 最新用户要求）

- GET /api/v1/live-agent-fact-categories 使用当前编辑方案的 plan_id query；首次未选方案保留无参数全局回退。主页面重试/资料更新、资料补全、互动补事实和助手分类读取均带可用方案ID，切换方案重新加载；pending合并与迟到响应隔离加入方案范围，不能沿用上一方案分类。
- 本次按用户明确任务只修改分类请求。事实类型列仍是原先留给WorkBuddy的空禁用占位，不擅自填写类型或接保存字段；不能将17项模拟分类验证说成真实下拉已接入显示/保存。接口及分类来源沿用后端，不写死行业分类、不改后端/客户资料/配置、不重启，不写E:/speech-generator-build。
- 网页150ec6d与独立桌面695608a已推送，桌面继承c2ec3ce；北京时间22:20:09—22:20:10已上线20261017-plan-fact-categories-150ec6d。两份构建、分类请求/17项模拟餐饮/切方案迟到隔离及完整父页筛选排序/新增修改/多宽度回归通过。3055旧无关文件、现场WorkBuddy管理程序与备份、三服务/Nginx/配置保持；12公网资源、6鉴权与2237完整前端备份哈希/大小/权限核验通过，指针更新。说明docs/2026-10-17-plan-fact-categories.md，证据本聊天work/plan-fact-categories-proof。

## 取消商品资料导入功能（2026-10-17 最新用户要求）

- 用户取消“导入商品信息及福利”，覆盖下方添加该功能的历史约定。移除按钮/进度/失败列表、专用导入模块和样式及仅为它添加的并发禁用；不再从商品卡批量生成事实。保留事实类型空列、原排序/过滤/搜索、手工新增/修改四层和WorkBuddy其它功能，不改后端/数据/配置、不重启。
- 先前37条导入事实ID67—103已按用户授权删除并有备份/37条审计；杨鸭子原41条、巴国布衣9条及商品卡/福利/已保存版本保留。不得因取消功能重新采纳或恢复这些记录。删除证据本聊天work/import-product-facts-duplicate-proof/deletion-report.json。
- 普通web及独立桌面同步，桌面继承7f088a5，不写E:/speech-generator-build，不整树覆盖main。说明docs/2026-10-17-remove-product-fact-import.md，证据本聊天work/remove-product-fact-import-proof；北京时间21:58:18—21:58:19已上线20261017-remove-product-fact-import-61d8c6d，独立桌面c2ec3ce分支已推送。两份构建及入口移除/原功能/多宽度检查通过；仅换两入口，3053旧无关文件、三服务/Nginx/配置保持，无重启。12公网、6鉴权及2197完整前端备份哈希/大小/权限通过，指针更新。未恢复已删数据或写客户事实。

## 导入商品卡及福利为事实（2026-10-17 最新用户要求）

- 归属过滤器下加“导入商品信息及福利”，导入当前方案所有活动商品卡及当前有效福利，按已保存商品ID/福利范围自动归属，保留有效期。真实字段、改名及个性属性分条，角色/权重不当事实，不推断优惠。事实类型仍空白，留WorkBuddy接人工确认，不改占位组件。
- 复用原四层规范化采纳与scope CAS接口，按来源与指纹去重，保护人工内容/归属；失败逐条显示，归属失败可重试补齐，不重复新增。切换方案只完成已提交条目的固定方案归属，停止后续并隔离迟到UI回执，不自动初始化/保存方案/开播。
- 普通web及独立桌面同步，桌面继承87db15a；不写E:/speech-generator-build，不改后端/提示词/配置或重启服务。说明docs/archive/2026-10-21/retired/2026-10-17-import-product-facts.md，证据本聊天work/import-product-facts-proof；北京时间15:59:20—15:59:21已上线20261017-import-product-facts-ea31984，独立桌面7f088a5分支已推送。两份构建、专项导入/去重/归属/失败/切换及原功能和多宽度核验通过。3011旧无关文件、三服务/Nginx/配置保持，无重启；12公网、6鉴权和2197完整前端备份哈希/大小/权限通过，指针更新。未代用户导入生产资料或调用真实模型/TTS。

## 事实类型空下拉列（2026-10-17 最新用户要求）

- 仅在归属范围前增加“事实类型”列，各行与新增行放空下拉框，不填类型、不绑定事实category/数据、事件或API，不保存；留给WorkBuddy接数据。占位组件FormalFactTypePlaceholder.vue，只有可访问标签及空禁用select，16px；不能猜类型选项或字段映射。
- 现在六列序号/事实类型/归属/正文/状态/操作，详情/子事实/空态colspan=6，新行也六格。原排序/筛选/搜索/状态展开/原位新增修改均保持。普通web与桌面同步，桌面继承bbc7985，不能写E:/speech-generator-build、改后端或重启服务。说明docs/archive/2026-10-21/retired/2026-10-17-fact-type-placeholder.md，证据本聊天work/fact-type-placeholder-proof；北京时间15:24:46—15:24:47已上线20261017-fact-type-placeholder-55b7425，独立桌面87db15a分支已推送。两份类型检查/构建、空下拉六列及原功能和五宽度验收通过；2970旧无关文件、三服务/Nginx/配置保持，无重启。12公网、6鉴权及2157完整前端备份哈希/大小/权限通过，指针更新。未接数据/保存，未改生产事实或调用真实模型/TTS。

## 已采纳事实界面按图2改版（2026-10-17 最新用户要求）

- 归属筛选移到顶部工具栏，与事实正文关键词搜索叠加；表头“归属范围”继续正序/倒序/原序排序。保留完整事实数组用于初始化/保存等业务，仅展示子集；搜索兼容大小写/全半角，清空恢复，编辑/新增/保存锁定工具栏，换方案清空。定向修改/风险定位/开始添加需同时重置搜索和归属，避免隐藏目标。
- 参照图2采用浅蓝紫头部、纸张/勾装饰、彩色序号、柔和下拉/按钮；新增独立状态列，原参考表达/表达提醒/风险提示及四层展开保留，不能把“已采纳”写成审核通过。五列序号/归属/事实/状态/操作，详情/子事实/空态colspan=5，新增行也五列。16px起，手机表格内部滚动，不让页面横向溢出。
- 普通web与独立桌面同步，桌面继承40bb2e4，保留WorkBuddy及原添加/修改四层业务。仅前端，不改后端/模型/客户资料、不重启，不写E:/speech-generator-build。说明docs/2026-10-17-facts-panel-redesign.md，证据本聊天work/facts-panel-redesign-proof；北京时间14:50:39已上线20261017-facts-panel-redesign-41f7d3b，独立桌面bbc7985分支已推送。两份类型检查/构建、完整父页搜索/筛选/排序/状态展开/五列新增修改及五宽度验收通过。仅两入口及40资源更新，2929旧无关文件、三服务/Nginx/配置保持，无重启；12公网、6鉴权和2117完整前端备份哈希/大小/权限通过，指针已更新。未改客户资料或调用真实模型/TTS。

## 事实归属范围表头筛选（2026-10-17）

- 用户要求在归属范围表头下方筛选公共/各链接，仅改变显示。默认全部，商品选项按当前租户/方案活动商品ID匹配并按链接自然数字排序；未指定和失效关联单独可筛选，不能当公共。序号/隔行按可见行，空结果保留表头并提示，总条数不变且显示当前条数，16px黑字。
- 保留原表头排序、归属修改、原位编辑/添加及四层后台；编辑/新增/保存时锁定筛选，切换范围重置全部。添加时恢复全部避免新未归属事实隐藏；定位风险或修改隐藏事实时恢复可见。完整formalFacts继续用于初始化/保存等业务，不能改成可见子集。
- 普通web和独立桌面同步，桌面继承cbb4099，保持WorkBuddy及之前文案精简，不写E:/speech-generator-build、不改后端/模型/客户数据、不重启。说明docs/2026-10-17-fact-scope-filter.md，证据本聊天work/fact-scope-filter-proof；北京时间14:25:49已上线20261017-fact-scope-filter-5abdc4f，独立桌面40bb2e4分支已推送。两份类型检查/构建、完整父页筛选/排序/归属修改/编辑锁/空态/切换方案及四宽度核验通过，仅两入口及40资源更新。2888旧无关文件、三服务/Nginx/配置保持，无重启；12公网、6鉴权及2077完整前端备份哈希/大小/权限通过，指针已更新。未改生产事实或调用真实模型/TTS。

## 事实页文案精简（2026-10-17）

- 用户删除事实页重复“事实依据”小标题、事实卡副标题中的“用于主线口播与智能问答”、右侧“以下内容同时供主线口播和互动回答使用；修改、删除均会留存操作记录。”说明。保留上方事实依据导航、卡片主标题、方案名/条数和全部表格/添加业务。
- 普通web及独立桌面同步；桌面继承5f7a59b，不回退WorkBuddy或其他功能。仅模板删除，无后台/模型/客户资料变更，不重启服务，不写E:/speech-generator-build。说明docs/2026-10-17-fact-copy-cleanup.md，证据本聊天work/fact-copy-cleanup-proof；北京时间14:14:19已上线20261017-fact-copy-cleanup-549da76，桌面cbb4099独立分支已推送；两份类型检查/构建和四宽度外观核验通过，仅两入口及40资源更新。2847旧无关文件、三服务/Nginx/配置保持，无重启；12公网及2037完整前端备份哈希/大小/权限通过，共享指针已更新，未写客户事实或调用真实模型/TTS。

## 表格底部原位添加事实（2026-10-17 最新用户要求）

- 用户要求添加按钮在已采纳事实表格下方，正常文档流随行数移动；点击只在表格末尾增加一条待输入行，不弹窗。输入完成离开该行或点保存沿用原名称提炼和manual:fact-editor采纳接口，由原程序/模型整理四层；空行/取消不提交，保存失败保留草稿，组合输入不提前提交，同一时间仅一个待输入行，16px。
- 接收原采纳回执的saved/existing，保留审核拦截/冲突及unchanged去重；新事实未人工确认归属时不能冒充公共，保存后可用原下拉框选择。切换租户/方案隔离名称和采纳迟到回执，旧请求不能清空新请求的保存状态。保留原修改自动保存、排序/序号/隔行/展开及WorkBuddy业务模式等。
- 普通web与独立桌面同步；桌面继承9fb0386，不整树覆盖main。不改后端/提示词/生成/调度/客户资料，不重启服务，不写E:/speech-generator-build。说明docs/2026-10-17-fact-inline-create.md；构建、浏览器与上线证据见本聊天work/fact-inline-create-proof，北京时间14:07:17已上线20261017-fact-inline-create-67c4f5e，桌面5f7a59b独立分支已推送。两份类型检查/构建、完整父级新增/修改/归属/排序/四宽度验收通过。仅两入口及40资源更新，2806旧无关文件、三服务/Nginx/配置保持，无重启；12公网、6鉴权和1997完整前端备份哈希/大小/权限通过，指针已更新。未改客户资料或调用真实模型/TTS。

## 事实内容原位自动保存（2026-10-17 最新用户要求）

- 用户取消额外编辑界面，仅点击修改在事实内容单元格原位输入，1200ms停顿或失焦自动保存，组合输入不提交；串行合并、失败保留草稿/重试、无改动和空内容不提交，16px。切换范围清理定时器、隔离迟到回执。
- 继续原PATCH事实接口和版本CAS，保持分类/名称，清空旧表达字段让原程序与模型重整四层，不改后端/提示词/归属/生成/保存方案。整行编辑面板方案未提交/未部署，不能带入本次。桌面继承25b6ab7，保留排序/序号/WorkBuddy等，不写E:/speech-generator-build，不重启。说明docs/2026-10-17-fact-inline-edit.md。北京时间13:49:57已上线20261017-fact-inline-edit-0c138d5，桌面9fb0386独立分支已推送。两份构建及完整父级自动保存/组合输入/失败重试/连续稿/CAS/迟到回执/四宽度验收通过；只更新两入口及40资源，2765旧无关文件、三服务/Nginx/设置与模型池保持，无重启。12公网及1957完整前端备份哈希/大小/权限通过，指针更新；未改生产事实/方案或调用真实模型/TTS，未提交的整行面板没有上线。

## 事实归属表头排序（2026-10-17）

- 用户要求归属范围表头归类排序；点击正序公共/链接自然数字顺序，再次倒序，再次恢复原顺序。同归属稳定排序，未指定/失效商品始终末尾；只调整显示，不能改变事实ID、原数组或保存/生成上下文。编辑/保存时禁用按钮，切换范围恢复原顺序，序号/条纹跟随显示。
- 普通web和独立桌面同步，桌面继承e17c272；保持去分类栏、序号、四列展开、事实归属、字段改名、WorkBuddy业务模式等。文字至少16px，不改后端/客户数据，不重启服务，不写E:/speech-generator-build。说明docs/2026-10-17-fact-scope-sort.md。北京时间13:22:52—13:22:53上线20261017-fact-scope-sort-c245b58，桌面25b6ab7独立分支已推送。两份构建及完整父级排序/归属/四宽度验收通过；仅两入口及40新资源更新，2724旧无关文件、三服务/Nginx/设置与模型池保持，无重启。12公网及1917完整前端备份哈希/大小/权限通过，指针更新，未写客户事实/方案。

## 事实列表序号与去分类栏（2026-10-17）

- 用户移除已采纳事实左侧分类栏；直接展示全部方案事实，在归属范围前添加从1开始的显示序号，不改变事实ID或分类/内容/归属数据。表格占满宽度、展开跨四列，保留隔行、三个按钮同排及16px字号。
- 普通web与独立桌面源码都同步，桌面继承d363e1e，保留事实归属、字段改名、正式保存和WorkBuddy业务模式/行业，不能恢复任务讲解。不改后台/客户事实，不重启服务，不写E:/speech-generator-build。说明docs/2026-10-17-fact-list-serial.md。北京时间12:54:49—12:54:50上线20261017-fact-list-serial-9c56533；桌面e17c272独立分支已推送。只更新两入口及40新资源，2683旧无关文件、三服务/Nginx/设置与模型池一致，无重启。两份构建与四宽度完整父级验收通过，12公网检查及1877前端备份哈希/大小/权限通过，指针已更新，未改客户事实。

## 桌面移除任务讲解（2026-10-17 已上线）

- 用户再次要求移除入口；普通web已有，但旧桌面快照仍显示。以5d02e1a独立修复四个子栏目、取消LectureTasksPanel挂载、旧miner跳转事实依据，导航文字最小16px。保留讲解数据/后台、事实归属、商品字段改名、正式保存及WorkBuddy业务模式/行业。
- 北京时间12:43:35上线20261017-desktop-no-lecture-d363e1e，源码d363e1e0d7ac346f595fe601a6a711e49b309b29已推送fix/desktop-remove-lecture-20261017；后续桌面继承该分支，不回退旧快照或将其整树覆盖main。只换桌面入口和新增20资源，2663旧无关文件、三服务、Nginx及设置/模型池保持，不重启或写客户资料；E:/speech-generator-build未改。
- 类型检查/构建、3项导航回归、完整父级8项导航/业务模式/四宽度验收及事实归属流程通过。12公网检查、1837完整前端备份哈希/大小/权限通过，共享指针更新。说明docs/2026-10-17-desktop-remove-lecture.md，证据本聊天work/remove-lecture-desktop-proof。

## 事实归属范围（2026-10-17）

- 用户要求事实内容前增加归属范围下拉框，公共或当前方案商品链接。事实scope.kind=common/product、scope.product_link_id按商品卡ID保存，数据库scope_json；旧NULL继续模型分类，UI显示请选择，不冒充已确认公共。
- 人工范围优先进入初始化、事实指纹与保存快照，不被模型覆盖；变更后重新初始化保存才用于下次开播。当前直播和旧版本不自动修改，Core/TTS/正文及调度不改。
- 事务CAS、租户/方案/活动商品验证、完整修订审计；前端失败回退和范围切换迟到回执隔离。保持隔行、按钮同排和三列展开，文字至少16px。桌面继承e793897及WorkBuddy，详见docs/2026-10-17-fact-ownership-scope.md；未写E:/speech-generator-build。
- 北京时间12:31:51—12:32:09上线20261017-fact-scope-a44dc8b，三次空闲检查后仅重启Management至1012522；Core775073/网关775930保持。两列可空scope_json已核实，旧事实/历史未自动归属，12类方案资产、设置/模型池、2614旧文件及母版/行业/Nginx保持。12公网资源、4未登录鉴权及1817前端完整备份哈希/大小/权限通过，指针已更新。桌面源码5d02e1a；既有Go27顶层失败和前端1字符串断言与基线一致，无新增。未开播或改客户事实/方案。

## 桌面商品卡字段名称恢复（2026-10-17 已上线）

- 用户要求点击修改笔可同时修改字段名称与内容。规格/销售价/数量/适用按商品保存field_labels，刷新保留；名称1—32字，不含换行或控制字符，失败保留草稿，取消不提交，文字至少16px。内部字段键与原业务保持。
- 后台和main普通web原已支持2feb70f；现场桌面64e6064快照缺少四个前端接入点。独立桌面源码e793897只恢复这四处，保留WorkBuddy及正式保存修复，已推送fix/desktop-product-field-labels-20261017；不得将旧桌面整树覆盖main。后续桌面发布以该源码及实际current整合，不能回退64e6064裸快照。说明docs/2026-10-17-desktop-product-field-labels.md，证据本聊天work/product-field-label-desktop-proof。
- 北京时间09:38:50—09:38:51发布20261017-desktop-field-labels-e793897，仅换桌面入口并新增20资源。2595旧文件、Management974245/Core775073/网关775930、Nginx及设置/模型池一致，未重启服务或写WorkBuddy原目录。构建、20项既有测试、完整父级字段编辑/正式保存及四种宽度验证通过；12公网检查与1771文件完整备份哈希/大小/权限通过，共享指针已更新，未修改生产商品资料。

## 公共20%、商品合计80%（2026-10-16 最新用户决定，已上线）

- 用户最新要求公共卖点与全部商品维持20%分配，覆盖下文公共不独立轮播决定。以已采纳的中间讲解任务名额计，公共20%、商品合计80%；商品内部仍只按商品权重，公共数量不影响商品份额。不强制每个小循环固定插公共，不改正文/事实/审核/TTS/播放管线。
- 独立5名额打乱窗口跨轮持续，公共单独共享卖点/角度历史，不推进商品游标或消费商品内部配额。缺公共/公共方向不可用时继续商品供料，不造任务或集中补偿旧失败；重试、复读、JSON重载与算价承接不重复扣名额，新场进度清零。保存方案、初始化及客户事实均未改。
- anchorcycle/httpapi两个完整包840通过、58环境跳过，无失败。3000主体任务公共600；商品同权重1200/1200，普通/主推1091/1309。V5/id118与最新V6/id120各20轮77主体名额为公共15、商品28/34；纯调度占位采纳，无真实模型/TTS或播放，不视为实播验证。说明docs/2026-10-16-common-share-scheduling.md，证明本聊天work/common-share-20261016。生产状态另按实际守卫与发布报告记录，不能将代码完成当作已上线。
- 北京时间23:38:59—23:39:02上线20261016-common-share-8aab52a，源码8aab52a9bc78906f756f83593bf138cb81d59ebd；三次全部AI/Core空闲守卫通过，仅替换Management至974245/SHA256 d4c7f63293849084db6c8d59b8991dd8f4c26093c75e249323c70bc9ac0fb5b8。Core775073/网关775930未重启；2594旧无关文件、1751前端及完整备份、170母版、115行业文件、10公网检查、设置/模型池与10类客户方案资产一致。当前2596发布文件，完整前端指针已更新。305场发布前已结束，未为发布停止AI或自动开播；本版实播效果待用户测试。部署证明在上述本聊天证据目录。

## 互动按商品取材与兜底（2026-10-16 最新用户决定）

- 用户要求明确商品则“该商品完整事实＋公共事实”，明确比较则带涉及商品，不能明确则保留当前方案全包，取代下文22:28无条件保留全包。程序从明确业务链接/唯一标题识别，含糊/未知/重名不猜；按已有product-common-v1不可变归属及完整来源核验取材，不把空结果替换事实。
- 规划和正文均按此取材，目的角度资产只渲染允许事实，表达约束按商品绑定；事实检查仍取全包。编辑页只在当前材料可核验时复用已有归属，否则当前全包兜底，不回退旧事实；当前2号新增地址/有效期导致与V5不同，编辑页走兜底是预期。不会自动初始化/保存/开播，不修改客户资料、Core、调度、主线生成/润色、TTS或WorkBuddy原目录。
- 16次真实Qwen3.7 Flash跨餐饮、成品原料、部件和服务回放关闭思考，11条P0完整，8组结构及商品/金额/主要条件核对通过；实际入模排除其他商品，未知/比较保留全包。报价顺带展开、未知推断和旧归属错误未根治。说明docs/2026-10-16-interaction-product-scope.md，证据work/interaction-product-scope-proof；完整测试/发布状态以实际报告为准。
- 北京时间23:10:30—23:10:33发布20261016-interaction-product-scope-af8f0bc，Management966112；Core775073/网关775930未重启。2602个旧无关文件、170母版、115行业文件、1751前端和10公网检查通过，设置/模型池及10类客户方案资产一致，完整前端备份指针更新。六包1070项通过、27个既有顶层失败无新增；三个范围接入回归由旧代码失败变通过。正式实播尚未验证，未自动初始化/保存/开播。

## 互动完整事实包（2026-10-16）

- 用户要求纠正互动正文用精简商品上下文替换完整事实的错误。业务规划只能限制回答范围，不能替换当前方案商品＋公共完整事实；模拟同样保留。事实检查读取不可变derived_safety，旧content_pools兼容，损坏引用/跨方案拒绝。不得写商品或行业特例。
- 修复互动请求日志接口漏cachedInputTokens参数，编译期验证db.Store接口；只记录模型缓存命中，不改缓存开关。Core、调度、主线正文/润色、TTS及客户方案不改，不自动初始化/保存/开播，WorkBuddy原目录不写。
- 跨行业回归和修改前对照、只读V5材料及真实Qwen3.7 Flash五组10次调用见docs/2026-10-16-interaction-full-facts.md与work/interaction-full-facts-proof。四组业务符合；真空问题仍有未知工艺被否定的过度推断，不称质量全通过或串内容根治。既有decisionexecutor失败与基线逐项对照，不称全量Go通过。发布状态据实际报告补记。
- 北京时间22:28:09—22:28:11已发布20261016-interaction-facts-0fec3a2，仅重启Management至962886；Core775073/网关775930不变。2601个旧无关文件、170母版、115行业文件、1751前端和10公网检查通过；设置/模型池、10类客户方案资产指纹一致，完整前端备份指针已更新。专项新增12项由旧代码失败变通过，27项既有顶层失败无新增；没有自动初始化、保存或开播。正式直播效果待后续核验。

## 商品公共材料升级（2026-10-16）

- 用户确定模型自行判断“商品/公共”两类，暂不增加人工归属确认；程序维护内部ID、显式商品绑定、完整原始事实和去重。每商品保存自己的卖点池、公共轮盘副本、商品事实与公共事实；公共比较可以引用其他商品，但专属权益不变成公共事实。
- 行业内容仍取方案业务模式/所属行业注入的原selling-dimensions.md；通用母版只新增归属协议，不能写死商户/行业卖点。行业文件加载失败不静默回退；保存检查行业维度、事实和初始化完成标记，变化需重新初始化。
- 保存与启动共用实际任务准备。公共任务只展开一次，共享角度使用历史；商品频率1.2、同商品卖点1.3、混品小循环及既有四层正文/润色/审核/TTS/Core保持。旧正式版本不可变，不自动重初始化、保存或改写客户事实。
- 1090项相关Go测试通过，真实隔离MySQL持久化/重新加载通过；真实Qwen3.7 Flash跨餐饮、家电、教育回放验证完整材料贯通，不能称归属语义100%准确。说明docs/2026-10-16-product-common-material.md，证明work/product-common-material-proof。上线状态待发布证据补记。
- 北京时间22:24:20—22:24:22已发布20261016-product-common-e8c6f41，源码e8c6f41348e9efda45c8b3d26b3e04f4948a1480，仅重启Management至958562；Core775073/网关775930不变。两次空闲守卫通过，2597个既有无关文件、115个行业文件、170份完整母版、1751个前端文件和10项公网检查通过；系统设置/模型池及10类方案资产指纹一致，未重初始化或保存客户正式版本。最终跨行业回放30次HTTP均成功且全部关闭思考，19/10/13个任务通过真实保存/启动准备；原始返回、隔离MySQL日志、发布和线上核对报告均在上述证明目录。共享前端完整备份指针已更新，WorkBuddy原目录未写入。需用户重新初始化后保存新版本才使用新材料。

## 每场权重调度边界（2026-10-16）

- 用户最终限定本次只解决商品→卖点×角度＋侧面的安排。小循环允许混品、随机出现3:1或4:0，同一场跨小循环累计有序；不能锁定一个商品占整轮，不能强制每轮平均。
- 商品定位与链接频率共同决定轻微偏好，有效长期频率最高/最低不超过1.2；同商品卖点缺省等权，显式微调后最高/最低不超过1.3。角度数量不能放大卖点份额；旧40:5角色权重不参与新调度。
- 已确认现有正文生成、侧面筛事实、条件润色、审核、TTS及Core为新业务，本次不得重构或替换。调度临时进度仍仅Redis，保存版本不可变，新场不继承上场欠额。说明docs/2026-10-16-cycle-scheduler-redesign.md；测试和上线状态须据实际证据报告。
- 调度e5013a3于18:17上线，真实服务回测发现旧复读换角度分支跨商品；补修de3abfd于18:39:55—18:39:57上线，复读换角度也使用同一商品/卖点调度，保留待采纳配额。仅替换Management至955599，Core775073/网关775930未变；2598个既有无关文件、1751个完整前端备份及10项公网资源核验通过，空闲守卫通过，配置和方案未改。
- 用户授权巴国布衣V4/id117真实文字服务30次推进；最终17组采纳，其中讲解1号5段、2号6段，归属错配0。56次Qwen3.7 Flash请求关闭思考并完整携带11条有效P0，无TTS/Core/正式场次，私有内存状态，排除可选宏观回灌。测试框架PASS不代表质量合格，不能将重试算成30段全部可播。证据work/live-script-repetition-proof的最终capture、metrics及两份中文报告。
- 事实不全源头已查清：buildCycleProductFocus的独立卖点池分支展开任务后早退，普通事实modelFacts未进入scope.Facts；V4完整7条主事实仅剩2张商品卡，每段可说/审核只有1张卡。四层母版存在但材料不全，第19/20组近乎复读、人均算账等仍有错误通过。本次未改事实、正文或审核，不得写成根治；修复此管线需遵循用户后续明确范围，不自动改写V4。

## 方案更多菜单文字（2026-10-16）

- 用户要求截图“更多”下拉菜单字号16px、黑色清晰。四个操作项统一16px、#111、500字重及1.6行高，删除项也遵从黑色文字要求；原点击行为及禁用状态保留。
- 只调整LiveStrategyView中该菜单样式，完整父级四种屏宽浏览器验证实际字号/颜色/字重和无溢出，类型检查/生产构建通过。证据work/plan-menu-font-proof；前端发布继承现场WorkBuddy及主播绑定版本，所有服务不重启。


- 北京时间09:01:13上线20261016-plan-menu-font-9872591；2584个旧非入口文件、Management893095/Core775073/网关775930和Nginx一致，未重启服务。10项公网入口/资源及1739文件完整前端备份核对通过，原WorkBuddy目录未写入。

## 主播绑定卡片与取消使用（2026-10-16 已上线）

- 用户要求点“设为当前智能体主播”后，实际绑定主播排第一、绿色外框、名称前小勾，卡片下面显示“取消使用”胶囊按钮。选中查看与实际绑定分开，只有绑定成功才标为当前使用；取消后解除当前方案关系，主播、样本、训练及其他方案绑定保留。
- 新增DELETE主播bind-plan接口，沿用租户和写权限；绑定/解绑锁同一有效方案，旧主播取消遇到新绑定返回409，重复取消幂等。无数据库结构变化，不清理资料，不改当前直播快照。
- 页面按账号/租户/直播间/方案丢弃迟到读取与变更回执；操作中禁用重复提交，失败重新读取实际绑定并保留可见错误。主播管理修改的文字至少16px，四种屏宽无横向溢出。
- 本地真实隔离MySQL验证方案/租户隔离、并发替换与资产保留；HTTP鉴权、4项卡片回归和12项导航/方案回归、完整父级12项浏览器检查通过。证据work/anchor-binding-card-proof，说明docs/2026-10-16-anchor-binding-card.md。未修改WorkBuddy原目录，最初整合3563459，发布守卫阻止并行WorkBuddy原地升级后，重新合入f9ef6db及行业四件套6b0de9b；115个实际行业文件与源内容逐个核对一致，现场换行原样继承。


- 北京时间01:55:34已发布20261016-anchor-binding-4a85619。空闲检查通过后仅重启Management至893095，Core775073/网关775930未变；2536个旧无关文件、现场全部母版和行业目录、模型池、设置、方案与主播资产/绑定保持。没有操作生产主播绑定。10项公网入口/资源和1699文件完整前端备份核对通过，原WorkBuddy目录未写入。

## 刷新保留当前方案（2026-10-16）

- 用户要求刷新后仍为页面当前选中的智能体方案。按用户+目标租户+直播间+普通/授权协助场景保存选择，优先本次手动选择与已保存偏好，不再无条件回到运行方案/绑定首项。只恢复当前有效且有权访问的方案；不存在时按原顺序回退。
- 临时清空或请求失败不清掉偏好，房间关系读取丢弃旧请求/旧范围/卸载回执，加载中手动选择不能被旧结果覆盖。本次只持久化页面编辑选择，不更改直播实际运行方案、绑定、初始化或开播状态。不继承旧未分账号的全局plan-id。
- 12项前端回归、类型检查/构建及完整父级11项浏览器检查通过，含两次刷新、房间/账号独立、迟到响应、加载失败与四种屏宽。说明docs/2026-10-16-plan-selection-persistence.md，证据work/plan-selection-persistence-proof；发布保护WorkBuddy现场，服务不重启。
- 北京时间01:04:09前端发布20261016-plan-selection-bb46d6c。2606个旧非入口文件、Management888726/Core775073/网关775930及Nginx一致，未重启；10项公网资源与共享完整前端备份1869文件核对通过。原WorkBuddy目录未写入，其新提交0d7f095尚未在本次现场发布，不提前发布其部分前后端联动；新版首次选择时建立用户偏好，不迁移旧无账号归属的全局plan-id。

## 互动规则的方案隔离（2026-10-16）

- 用户反馈选择巴国布衣仍显示杨鸭子回答策略。策略列表及编辑必须使用当前选中方案，不能只按直播间查询，也不能回退到房间另一个绑定方案。新列表/纠正显式传plan_id；后台按租户+房间+方案归属过滤和校验，未绑定旧记忆保留但不混入任意方案。
- 切方案、租户、房间或账号同步清空列表/修改/删除确认，丢弃迟到回执，A→B→A亦不可复活旧结果。列表错误区别于没有策略，当前方案可重试。修改/删除/版本与纠正来源不能操作其他方案，已保存策略不清理或自动迁移。
- 模拟挖掘、直播互动、处理历史原有方案范围保留；生成原有严格隔离保持，纠正输入也不再引用未绑定旧记忆。回答策略文字至少16px。WorkBuddy业务模式/行业配置及原目录保留；见docs/2026-10-16-interaction-plan-isolation.md，证据work/interaction-plan-isolation-proof。
- 12项Go、8项前端回归、类型检查/构建及完整父级11项浏览器验收通过。北京时间00:52:10发布20261016-interaction-isolation-43433ce，仅空闲时重启Management至888726；Core775073/网关775930不变。2558个旧无关文件、现场母版/行业目录、系统设置、模型池、方案及记忆内容保持；生产前后记忆归属及历史数量一致。10项公网资源验证通过，共享完整前端备份1829文件匹配实际服务器，WorkBuddy原目录未改。

## 移除任务讲解页面（2026-10-16）

- 用户要求去掉截图中的“任务讲解”。商品与事实只保留商品资料、活动福利、商品资料补充、事实依据四个子栏目；不再挂载LectureTasksPanel，旧miner聚焦事件回到事实依据，防止空白页。
- 仅移除该配置页入口与内容，保存的讲解任务、二级事实、版本及后台接口保留；不清理业务数据或改变正在运行的直播。WorkBuddy业务模式及行业配置按最新提交整合，原开发目录不写入。
- 16项既有前端回归及完整父级8项浏览器验收通过，覆盖四栏目切换、旧聚焦兼容、业务模式保留和四种屏宽；类型检查与生产构建通过后仅发布前端，所有服务不重启。说明docs/2026-10-16-remove-lecture-tasks-page.md，证据work/remove-lecture-tasks-proof。
- 北京时间00:36:07已发布20261016-remove-lecture-a4ea0dd，2518个旧非入口文件、现场WorkBuddy Management887718/a47cf85、Core775073、网关775930及Nginx一致，未重启任何服务。10项公网入口/资源指纹通过，共享完整前端备份1783文件与实际服务器哈希/大小/权限一致；原WorkBuddy开发目录未改。

## 商品资料补充布局与链接范围（2026-10-16）

- 用户要求本模块文字统一16px，移除“补全清单”可见标题，将“商品品类”显示为“所属行业”并移到顶部；其下为“链接选择”（全部及当前方案所有商品卡），与“换下一批清单”在同排垂直居中。保留原下方问题、跳过、对话与待确认资料；窄屏允许控制行换行以免溢出。
- 用户进一步确认“全部”检查整个方案，单链接仅检查对应商品。fact-gap-check及资料助手请求以product_link_id绑定，服务端验证租户/方案/活动状态；使用已有商品与福利范围，不靠显示文案猜ID。整方案路径兼容，单商品保留条件与适用纠正，未绑定的共用事实不能自动套用。检查请求完整携带有效P0。
- 链接切换只重载左侧清单，取消旧检查并丢弃迟到结果，保留右侧对话/草稿。所属行业沿用原product-category存储，不将本次改名扩为业务模式行业配置调整。两份相关母版在原目录维护，发布校验整个实际母版目录。
- 用户明确WorkBuddy正在修改“业务模式”页面，必须保护其进行中的代码、行业配置及后续线上发布。本次不改PlanBusinessModePanel/planBusinessMode/usePlanBusinessMode，不写E:/speech-generator-build；共享父级只增加商品卡属性传入，api.ts仅扩展补全请求。部署前后核对实际current，发现并行更新即停止切换、重新整合，不以旧构建覆盖业务模式。
- 当前16项前端回归、13项定向Go测试、类型检查/构建和完整父级10项浏览器验收通过，含四种屏宽、原保存、链接范围/对话保留/失败重试/方案切换，未运行真实模型/TTS或保存生产资料。说明docs/2026-10-16-supplement-link-scope.md，证据work/supplement-link-scope-proof。
- 首次发布守卫发现WorkBuddy原地升级Management至3fdaa65及新增行业目录，未切换或重启。重新整合该已上线提交与14个行业配置后，于北京时间00:13:36发布20261016-supplement-links-8310b66。仅空闲时重启Management至886745；Core775073/网关775930不变。2471个旧无关文件（含实际行业目录）、WorkBuddy行业选择/配置包、系统设置、模型池、方案原字段和Nginx保持一致。
- 业务模式源组件及接口保持，11项浏览器回归通过；整合后行业加载4项、模型5项、调度8项定向测试通过。10项公网入口/资源通过，最新完整前端备份1743文件匹配实际服务器哈希/大小/权限。后续仍以实际current及最新整合源码发布，不能恢复到首次守卫阻止的旧基线。

## 业务模式下的行业选择（2026-10-15）

- 用户要求两张业务模式卡片下各加行业选择框，先配置常规行业。电商带货和本地生活各提供15项（含综合/其他），分别保存到当前方案的ecommerce_industry/local_service_industry；默认general，不按现有商品猜测行业或覆盖已选业务模式。
- 两个行业框独立，修改未选中模式的行业不切换业务模式；刷新及模式切换保留，跨租户/方案隔离。新增PUT business-industry沿用方案授权、严格校验对应目录；GET biz-domain兼容原字段并附带两套行业与选项。目录以服务端为准。
- 当前只保存选择，不接入重挖卖点、分类、提示词、P0或生成逻辑。读取失败不伪造默认成功，保存失败回退，旧方案迟到响应丢弃；新增文字至少16px，四种屏宽无水平溢出。说明docs/2026-10-15-business-industries.md，证据本任务work/business-industry-proof。
- 北京时间23:21:14发布20261015-business-industries-aa8759e；无活动/暂停直播及声音时仅重启Management至885265，Core775073/网关775930不变。2411个旧无关文件、系统设置、模型池、方案原字段及业务模式、Nginx一致；两个行业列默认值及新接口鉴权已核实。10项公网入口/资源验证通过，完整前端备份1703文件与实际服务器哈希/大小/权限一致。

## 业务模式入口与选择保存（2026-10-15）

- 用户要求在“商品与事实”前新增“业务模式”，页面展示“电商带货”“本地生活”两张大互斥单选卡。用户明确本次只做入口、单选和保存，后续业务逻辑另做；不要据项目本地生活草稿自动扩展到重挖卖点、清空分级、改提示词/分类或直播生成。
- 当前方案选择保存到live_agent_plans.biz_domain，存量/新建方案数据库默认ecommerce，本地生活为local_service。专用GET/PUT biz-domain接口按原方案权限验证租户及运维范围；非法值400、缺失/归档方案404。原方案创建/重命名接口继续兼容，不依赖新字段。
- 原导航与初始化/保存/预览保留。两卡使用原生radio，选择自动保存；读取失败不冒充默认成功、保存失败回退、切换方案忽略迟到回执。新增文字至少16px，窄屏两卡上下排。说明docs/2026-10-15-plan-business-mode.md，证据本任务work/business-mode-proof。
- 北京时间22:54:53发布20261015-business-mode-d5e52d1。仅无活动/暂停直播和声音时重启Management至884572，保留WorkBuddy c6dfb55及现场配置；Core775073/网关775930、2364个旧无关文件、Nginx、系统设置、模型池及方案原字段不变。新增列与默认值已核验，GET/PUT新路由未登录均401。真实隔离MySQL、HTTP鉴权/响应、9项前端回归、构建及完整父级11项浏览器验收通过。
- 两个公网入口及8个资源指纹通过，共享前端备份指针已更新，1657个文件哈希/大小/权限与服务器一致。

## 初始化入口移至顶部分类栏（2026-10-15）

- 用户要求将“初始化直播智能体”按钮移到“声音”后面，点击执行原初始化业务。通过分类栏操作插槽复用原AnchorControlBar及initializeAnchorGeneration，不新增初始化协议、不自动保存方案或开播，预览区不再重复显示该按钮。
- 原进度、禁用与防重复提交保持，成功/部分失败/异常提示移到分类栏下方，在四个配置分类及生成预览页均可查看。按钮及新增反馈文字最小16px，窄屏允许换行。下方模式选择、播放器及参数保留。
- 类型检查/构建、9项既有回归与完整父级合成数据11项浏览器检查通过，覆盖原POST/tenant/SSE、切换栏目保留进度、防重复提交、部分失败/HTTP异常/重试及四种宽度。未初始化生产方案或调用真实模型/TTS。证明work/initialize-navigation-proof，说明docs/2026-10-15-initialize-navigation-action.md。
- 北京时间19:34:51前端发布20261015-initialize-navigation-9da7ab6，2319个旧非入口文件、Management878594/Core775073/网关775930及Nginx保持不变，未重启服务。
- 10项公网入口/资源指纹核对通过，共享备份指针已更新，1611个前端文件与实际服务器哈希/大小/权限一致。

## 工作台隐藏顶部标题（2026-10-15）

- 用户要求去掉截图中划线的“智能体工作台”和“生成与预览”两行可见标题，下面全部保留。只删除工作台组件内的两个标题元素及其专用样式，保留无障碍区域名称、初始化、模式选择、播放器、参数、快照说明及原业务接口。
- 不恢复这两行标题，不把隐藏标题扩大为删除生成/试听功能。构建与完整父级四种宽度检查通过；证据见本任务work/remove-workspace-headings-proof。
- 北京时间18:58:18前端发布20261015-workspace-headings-c7a650e，2278个旧非入口文件保持一致，未重启服务。Management878594、Core775073、网关775930及Nginx保持不变。
- 两个公网入口及8个资源指纹核对通过，共享备份指针已更新，1571个前端文件与实际服务器哈希/大小/权限一致。

## 移除智能体配置模块（2026-10-15）

- 用户要求移除智能体工作台内的“智能体配置”页签及其内容，直接显示“生成与预览”。此要求替代此前该工作台双页切换的约定；不要恢复配置页或返回配置按钮。
- 顶部保存方案、初始化、主线模式、播放器及其它生成/试听功能保留。页面移除不代表删除方案、事实或内容池数据，不改变后台生成与保存协议。
- 新的工作台标题和说明文字最小16px。前端发布保留WorkBuddy已原地更新的桌面版本及运行中的服务，证明见本任务work/remove-agent-config-proof，说明见docs/2026-10-15-remove-agent-config-page.md。
- 北京时间18:48:29已发布20261015-remove-agent-config-a9aee5b。20项既有前端回归、类型检查、构建及完整父级四种宽度检查通过；2231个旧非入口文件、三个运行服务及Nginx保持一致，10项公网入口/资源核对通过。继承发布前WorkBuddy新Management878594/a43448e，不以旧二进制覆盖它。

## Qwen3.7 Flash四层正文验证（2026-10-15，离线验证未部署）

- 用户指定Qwen3.7 Flash、关闭思考、四层必说/可说/不提/改写，验证卖点×角度+事实+风格+P0到正文。已完成20组原编排输入与8组去可选切面的纯链路真实调用，28/28请求enable_thinking:false、温度0.25、完整9条有效P0，耗时1.816—5.690秒。生成候选可行，但首次正文仍会补写未授权免运费/竞品报价、未知功能或工艺，不能视作稳定可播稿或据此取消审核。
- 改价、称呼切换、商品过滤与部分条件保留生效；原编排一篇373字超300。P0字面语境候选不能等同违规，不将最好/绝对等词一律拦截。未用模型自评作事实结论。报告docs/2026-10-15-qwen37-flash-four-layer-body-validation.md，证据work/qwen37-flash-four-layer-proof。
- 本次只读取得当前方案资料，未启动直播/TTS、保存业务资产、改缓存/模型/母版或部署程序。前后快照及原三服务PID一致。服务器正文母版与仓库有一段避重说明差异，本次按实际服务器发送，保留原地修改，后续发布仍核对实际母版。

## 商品与事实导航名称（2026-10-15）

- 用户要求将智能体方案一级导航“事实与内容”改为“商品与事实”，同步子栏目无障碍名称。保留facts内部键、五个子栏目及切换流程。按既有字号要求，手机一级导航文字最小16px，窄屏两列排布。
- 既有导航3项回归、类型检查及生产构建通过，真实LiveStrategyView父级14项浏览器检查覆盖更名、栏目切换和1600/1024/390/320px无溢出及一级标签至少16px。证明work/product-fact-label-proof；本次前端发布不重启服务。

- 北京时间08:36:19前端上线20261015-product-fact-label-403fa69，源码403fa69。2717个旧非入口文件、Management850288/Core775073/网关775930及Nginx一致；10项公网资源通过，最新备份2024个前端文件匹配实际服务器。未重启直播，未修改后端或生成/缓存配置。

## 直播互动隐藏模型建议（2026-10-15）

- 用户要求直播互动页取消“模型建议”。按记录来源隐藏live_uncertain的模型原因、采纳按钮、建议补充和纠正建议，包括共用待确认卡片；保留实际现场、适用场景、生成的回答、用户纠正记录和填写纠正/无需纠正/删除操作。模拟挖掘的建议及采纳保持原流程，后台原始审核内容不删除。
- 同步调整直播页说明，直播及历史记录时间/已保存纠正文字至少16px。真实LiveStrategyView父级合成数据验证50项通过，覆盖直播编辑、模拟建议保留、历史搜索分页/错误/迟到响应和四种屏宽；生产构建与类型检查通过。发布只更新前端，不重启服务。

- 北京时间08:33:17上线20261015-live-suggestions-b67741d，源码b67741d；2676个旧非入口文件、Management850288/Core775073/网关775930及Nginx保持一致，未重启直播。10项公网入口/资源核对通过，最新备份1984个前端文件与实际服务器一致。证明work/live-suggestion-display-proof，说明docs/2026-10-15-live-interaction-suggestions.md。

## 互动处理历史搜索和分页（2026-10-15 已上线）

- 处理历史使用明确的关键词搜索/清空/状态筛选和上下两组分页，每页30条，支持首页/上一页/下一页/末页及页码跳转；沿用已提交搜索条件，输入草稿不自动提交。首次读取失败明确报错，失败翻页保留上次有效列表和页码，记录缩减后回到有效页；房间、方案、来源和账号切换丢弃迟到响应。
- 历史卡片标题加“互动问题：”，隐藏模型原因、建议补充和纠正建议，保留现场、原回答、用户纠正及纠正预览，不删除后台原始内容。模拟挖掘的建议和采纳继续保留；直播记录建议区按后续用户要求隐藏。新增控件及历史页文字至少16px，手机自动换行。
- 北京时间08:27:10前端发布20261015-interaction-history-6c82d37-r2，源码6c82d37，未改管理程序/数据库或重启服务。2635个旧非入口文件、Management850288/Core775073/网关775930及Nginx一致；10项公网资源一致，最新前端备份1944文件匹配实际服务器。来源记录用独立文件名保留旧记录。21项既有回归、构建、完整父级合成61条数据的36项功能/错误/迟到响应/四种宽度检查通过；生产未执行记录删除、纠正或模型/TTS测试。证明work/interaction-history-search-proof，说明docs/2026-10-15-interaction-history-search.md。

## 界面文字字号下限（2026-10-15 用户明确要求）

- 今后本项目新增或修改的界面文字最小为16px，包括正文、表单标签、输入框、按钮、状态标签、提示、占位文字及辅助说明；标题可按层级放大，不以缩小文字解决拥挤。
- 空间不足时调整间距、列宽、换行或响应式布局，保证文字可读且无重叠。交付时核对真实父级布局与手机宽度的实际计算字号；不得用浏览器缩放冒充符合下限。
- 本次商品卡片已落实本组件文字最小16px，并将“日常价”显示和编辑标签改为“销售价”，daily_price字段/数值/保存逻辑不变。北京时间08:07:08前端发布20261015-sales-price-6f49386，2553个旧非入口文件、三服务及Nginx保持一致；继承发布前后续更新的Management850288，未重启任何服务。四种宽度含五项属性的字号/布局、编辑名/原值及构建验证通过，10项公网资源一致，最新备份已核对1864前端文件。证明work/sales-price-label-proof，说明docs/2026-10-15-sales-price-label.md。

## 补全清单问题文字和按钮（2026-10-15 已上线）

- 用户要求放大卡片中问题文字并把“问这个”改成“回答这个”。填写提示字号14px改为16px，仅更名按钮，原openGap问答流程及未提交输入保留。不要因文字更名自动提交输入或保存事实。
- 北京时间07:54:00前端发布20261015-supplement-questions-4bb9b10，基于3251038最新整合源码；保留WorkBuddy页面与事实列表两列/隔行/同排按钮。2509个旧非入口文件、三服务及Nginx一致，未重启直播；10个公网入口/资源核对通过，最新备份已验证1824前端文件。11项既有清单/对话回归、构建、完整父级合成数据的问答成功/输入保留/四种宽度验证通过。证明work/supplement-question-size-proof，说明docs/2026-10-15-supplement-question-display.md。

## 已采纳事实列表两列布局（2026-10-15 已上线）

- 用户要求隐藏 WorkBuddy“当前方案已采纳事实”列表中的分类和事实名称两列。列表只显示事实内容和操作，保留左侧分类筛选；分类及名称继续保存，点击修改时在内容编辑区上方编辑。风险提醒保留在内容区域，表达及二级资料展开行跨两列。
- 北京时间07:24:49前端发布 `/opt/xiaolan/releases/20261015-fact-list-columns-d61ff04`，源码d61ff04，基于已整合WorkBuddy的d976350。只更新web/web-desktop入口及新增哈希资源，2427个原有非入口文件、三个服务PID/二进制与Nginx一致，未重启直播服务。未改E:/speech-generator-build原目录。
- 未修改基线构建逐项复现当时生产资源；类型检查、生产构建、10项既有导航/风险回归，以及完整LiveStrategyView合成数据的筛选、修改保存、两类展开行和1600/1024/390/320px显示验证通过。10个公网入口/资源指纹一致，最新前端备份指针已更新，1744个前端文件匹配实际服务器。证据位于本任务work/hide-fact-columns-proof，说明docs/2026-10-15-fact-list-columns.md。后续仍继承发布时实际current及最新整合源码。
- 用户随后要求隔行底色和三个操作按钮同排，已于北京时间07:39:36前端发布20261015-fact-row-style-9520b45。按筛选后事实序号交替白色/浅蓝色，表达/二级资料展开不打乱条纹，风险/编辑/展开提示色保留。操作栏360px，话术预览/修改/删除不换行且不与展开箭头重叠。完整页面合成数据四种屏宽、筛选、修改保存、跨列及构建通过；2468个旧非入口文件、三服务和Nginx一致，未重启直播。10个公网入口/资源一致，共享备份已核对1784前端文件。证明work/fact-row-style-proof。

## 直播下载和24小时保留（2026-10-14 已上线）

- 实际current为 `/opt/xiaolan/releases/20261014-live-session-download-9c1c5f9`，发布源码9c1c5f9，整合当时已部署的1608ea6。Management PID820631，SHA256=a27d3d834732e56f0c54fd1d94d7b2299c78d217cb5d433d87ec5d3c148a38c9；Core/网关未重启。
- 字幕区“本场回答库”右边为“直播下载”。下载已开始播放的主线、互动和已使用衔接语的完整文案/原声音片段，待播候选不导出。TXT无标题/时间戳，整场查询不受声音时间筛选和50条分页影响。功能上线前已结束、缺少完整播放证据的场次不能补齐，不得冒充完整设备录音。
- 用户指定直播下载与回答库原始文案只保留24小时。已播声音复制至独立私有 `live-downloads/tenant-*/room-*/session-*/audio-*.wav`，从开始播放计24小时；到期读取立即失效，后台删除下载文件和原稿记录。这个归档不恢复调度进度，也不参与生成、FAQ或正文/声音缓存复用。
- 已采纳的回答策略及其版本、学习出处长期保留在agent_memory_items/agent_memory_versions等独立表；24小时清理只涉及live_session_speech_assets和live_generated_speech_history，不清理已采纳策略。当前31个既有记忆版本的内容/结构摘要逐项核对保留；不将这个版本数当作31条独立策略。
- 两项平台缓存复用开关仍为false，供料缓存保留5小时仍是原配置。下载副本的24小时期限独立，不为下载打开缓存复用，也不删除正式方案的持久音频或已保存事实/策略。
- 此次只替换Management及web/web-desktop入口，新增39个文件，2210个原文件逐项保持一致。共享前端备份指针 `C:/Users/19918/Documents/Codex/2026-10-09/zh/work/frontend-preservation/latest.json` 已更新，1538个前端文件与服务器逐项一致。证明位于本任务 `work/live-session-download-proof/`。
- 下载/复制单元测试、临时MySQL表的权限/24小时清理/策略保留测试、真实OSS合成私有对象复制读取删除、RoomDetailView的151条导出/时区/分页/错误/焦点/四种屏宽验收均通过。未自动开始生产直播、生成话术或调用TTS。原称呼基线的已知失败见下载文档，未算全量Go测试通过。

## 程序与模型分工：模型返回内容，程序维护索引和关系

- 2026-10-10 用户明确规定，适用于系统内全部模型、智能体、生成、规划、审核和回灌接口，不按模型供应商区别处理。
- 模型只完成本次明确的内容任务并按约定格式返回所需内容。可以要求纯文本、列表或含内容字段的 JSON；不得让模型创建、填写、抄回、缩写或维护程序内部索引、引用标识及关系字段，包括 `id`、`index`、`task_id`、`candidate_id`、`hook_id`、`fact_ids`、`product_ids`、`benefit_ids` 等。
- 请求身份、账号身份、事实出处、商品/福利绑定、任务关系、版本、顺序、生命周期状态和数据库键由程序建立、保存和维护。程序通过当前请求上下文关联返回结果，不依赖模型回填 ID，也不依赖模型替系统保持编号或关系一致。
- 优先一次请求一个明确任务、返回一个对应结果。需要多项结果时拆成独立任务，由程序收集、排序和建立关系；不要让模型填整张带索引和关联字段的表。确需返回内容列表时，列表仅包含内容，不由模型填写程序关联元数据。
- 一个角度需要模型推演多个方向时，可以要求“1、2、3”的编号列表；这些数字仅用于展示和分段，不是系统索引。程序将整次返回绑定到发起推演的角度，自行生成子条目ID与顺序并维护JSON；模型列表编号重复、跳号或变化不得改变任务身份、出处和关联关系。
- 来源关联在请求前由程序明确，返回后仍按该请求上下文绑定。内容不足以确定关系时保留未关联状态或另发单项内容任务，不根据模型编写的编号、模糊标题或截断标识猜测来源。
- 商品价格、规格、时间、数量、观众口播中的“1号链接”等业务内容可以按任务正常返回；它们不属于让模型维护内部索引。模型仍需遵守当前有效事实、条件、P0及明确输出要求，正常近似价格与允许的合理延伸不能因本规程新增否决。
- 格式异常与内容违规分别记录。非关键格式异常局部处理，保留有效内容；未完成的判断不得冒充已完成，也不能把格式错误统一记为违规。单次异常按已确认的供料、跳过和回退机制处理，不据此停止主线。
- 修改现有接口时，同步调整母版、传输、解析、保存与下游消费，并用真实历史错误回放验证。保留原始返回和程序建立的出处记录。规程写入不等于旧接口已改造或已上线；交付时明确本次实际完成范围。
- 2026-10-10 用户确认保存角度推演池：同一角度在本场实际使用3次后补充新推演，阈值可配置；生成或待播不计使用，播放回执幂等。旧推演不丢弃，按代保留并长期积累；符合当前事实、规则和配置的旧方向可继续选用，失效方向保留记录、不进入当前可用池。新场次计数清零，复用已有有效方向，不全量重推。池资产按租户、方案、角度及资料指纹保存；场次进度仍遵循既有临时状态生命周期。新增正文仍结合当前上下文生成，不把长期积累等同于重复播放旧正文或音频。
- 2026-10-11 用户进一步明确以最终组合和口播变化为主要目标，同一基础方向可以复用并变换场景、切入、顺序和措辞；每个方向争取至少3种表达，不要求每条推演都创造新卖点。不得用“必须语义全新”的自动判定否决这些可用组合；完全相同方向从可选视图去重，原始历史仍保留。正文继续遵守已确认的逐字重复口径、有效P0、商品事实关系和明确输出规则。
- 2026-10-11 用户同意新扩展方向入可选池前由独立审核角色核对一次，程序按方向内容及当前事实、纠正、P0、审核母版和模型保存审核记录；内容与这些边界相同则复用，变化则旧判断不能授权新内容。只选择明确通过的方向；有问题、不确定或未完成方向保留原始历史而不选用。没有可用方向时按原任务继续供料，不因可选规划审核失败停止主线。方向审核不代表新生成的口播已审核，正文继续遵守既有最终文字处理。规程不等于生产已发布，按实际代码、回放、持久保存测试和发布证据分别说明。

## 提示词统一存放

- 2026-10-11 用户确认事实三栏分别为：已确认的正向及负向客观事实；自然语言负向表达约束；正向口语举例。保存前通过模型整理并复核，保留原始输入与版本出处。用户只给一个示例时，一次裂变3—5个，复核后按事实版本、当前目的和有效规则保存，程序每次调用随机排序。示例不是新增事实授权，可以复用原句、结构和准确说法，不要求正文每次与示例库100%不同，不设文字重合率门槛；以目的、角度、上下文及事实边界验收。
- 2026-10-11 用户纠正过严的审核口径：304杯身的“拿着沉甸甸”这类普通手感描述可以作为适当扩展，不因资料未逐字提供而判错。正常口感、手感、画面和主观感受允许自然表达；具体重量、时长、认证、实测、成分排除及明确性能保证仍需依据。不能把所有修辞都当新增事实拦截。
- 2026-10-11 用户要求审核高位关注P0、行业逻辑、逻辑错误和商品使用错误，其它表达可以宽一些。商品仅确认开袋即食时不能自动扩为煮汤、炖菜等新用法；商品有明确相关描述时按条件正常使用，原料用法不能转成成品用法。审核用户已确认事实与审核模型生成错误分别处理。
- 2026-10-11 用户进一步规定功效、功能、使用办法必须有事实库支持，明确事实负向要求是审核点。物流若有“不准说物流时效”或不承诺到货的要求，不能说“今天发明天到”；没有此要求且不违反P0、行业规则或明确事实则默认放行，不擅自新增禁令。其它核对疑问留后台审核意见，由用户自己判断、编辑事实；非拦截意见不能变成违规、阻断、自动修改或永久禁说。

- 2026-10-11 用户明确采用两级事实依据：一级为商品本身及与原料、部件、背景对象的已确认关系；二级为对应对象的资料，只作为模型重新生成讲解时的事实约束与边界，不能当固定正文照背。普通主线、互动、通用分析和共用提示词默认不携带二级；选中讲解任务才加载相关二级。不得把原料、部件的特性或用途改成成品的特性、功效、吃法或使用方法。程序维护两级关系和出处，模型仅返回对象与资料内容。旧原文、记录与已运行版本保留。

- 2026-10-11 用户要求自然语言纠正必须跨类目，通过项目母版与程序实现，不写商品特例。整理模型只返回事实修正、表达限制、展开指导或澄清问题，程序绑定被选内容、商品、出处与方案。局部取消不能自动升级成永久禁令；明确长期纠正保存为方案资产，修订关联库内容，保留旧历史。规划、主线和互动携带有效规则，内容池重建仍应用纠正；正在运行的方案快照保持不变。

- 2026-10-10 用户明确P0约束任何时候不能丢。所有内容生成、规划、离线测试、极简提示词和模型对照必须携带本次有效P0；不能为省token、缩短提示词或隔离变量省略P0，不设置绕过P0的测试开关。可简化可选角色、风格、范文、历史和无关资料，保留P0的范围、条件与例外。历史缺P0的测试保留原文，但不能用来认定完整合格或可上线。

- 2026-10-10 用户明确主播风格只控制语气词、口头禅、常用称呼、自指、句式、节奏、情绪及真实频率和已确认权重。相似度不按是否重复商品内容判断。具体商品话术、价格、优惠、库存、物流和售后承诺不能混入长期风格，案例仅供人看；下游生成不接收业务填槽范文。样本未出现某词不能自动推断为禁用。总规则边界测试与主播风格联调分别进行。母版修改、清理候选和实际方案资产生效须分别说明，保留原始样本和历史版本。

- 用户指定的系统提示词模板专用目录为 `E:\直播伴播\configs\prompts\anchor-style\`（仓库相对路径 `configs/prompts/anchor-style/`）。
- 今后系统所有新增或修改的提示词模板，包括风格提取、元提示词构建、卖点角度挖掘、口播生成、评分基准提取、监督与回灌等，统一在该目录以 Markdown 文件维护。目录名不限制用途。
- 程序读取该目录中的模板；不得把新的大段提示词正文写死在业务代码中，也不得另建散落的模板目录。模型选择与模板文件名解耦。
- 修改已有代码内嵌提示词时，将相关模板正文提取到该目录；仅为传输协议、输入数据或占位符赋值的代码不属于模板正文。
- 用户确认的主播元提示词、评分基准和卖点轮盘等运行资产仍按现有方案关联及数据库版本化保存。模板集中存放不替代数据库，也不覆盖模型原始返回。
- 不因这一约定擅自批量移动历史文件；历史模板在实际修改或用户要求迁移时逐项处理，并验证加载路径与发布打包。

## 母版与房间方案隔离

- 2026-10-11 用户明确：现在所有业务资料和规则按照直播间当前选择的已保存智能体方案加载，不再叠加旧房间L3。实时规划、正文、审核、衔接、预览、缓存身份和运行快照不得读取历史L1/L2/L3业务目录；公共有效P0仍完整携带。旧房间调教记忆必须有当前租户、房间及方案的明确关联才能参与生成，无绑定的历史记录（含风格标签）不得自动继承。历史资料保留，不自动迁移或采用进新方案。历史管理入口与正式生成入口分别说明，代码修改、测试和生产发布据实际证据说明。

- `configs/prompts/anchor-style/` 下的文件全部是项目级母版，不是某个直播间或方案的可变运行配置。
- 房间对应方案的提示词、主播元提示词、评分基准、卖点轮盘、评分权重与容差等稳定资产及配置保存于数据库，按租户、方案和版本隔离；房间通过方案关联读取生效版本，禁止跨租户或跨方案覆盖。
- 母版用于初始化、分析生成或明确导入方案版本。调整母版不得自动覆盖已保存的方案配置；方案上的用户编辑、权重调整及确认操作只写对应数据库资产，不写回母版文件。
- 新增权重功能时，默认规则也应从母版初始化为方案数据库配置；运行取方案的有效配置，不把共享MD文件当所有房间的实时权重来源。母版更新须通过明确导入/升级产生新方案版本。
- 单场运行采用已选方案的配置快照，后续方案修改按新版本生效，不擅自改变正在运行场次的标尺。
- 本条约束稳定方案配置，不改变已确认的大循环临时状态生命周期：本场进度、采纳记忆、1000字窗口和配置快照仍仅保存Redis，结束清理，服务器重启重新开始；不得借此把临时场次改为数据库恢复。

## 母版实时加载与协议兼容

- 默认每次新的分析、提取、生成、回灌请求从当前母版目录读取最新文件，服务器亦同；不得由浏览器或数据库历史模板正文覆盖最新母版。手动覆盖须在本次请求显式选择，不能把旧页面快照默认为手动输入。
- 已确认方案资产仍按上面的数据库隔离规则保存，母版更新不自动重写旧风格、评分基准或卖点轮盘；一项已发出的请求保留当时实际用的母版快照与版本指纹。
- 模板要求的输出字段、程序校验、保存和下游调度须同步兼容；未知结构或缺少本次母版要求的字段时，明确报错并保留原始返回，不默默丢字段、不由程序编造补齐。
- 发布打包须包含整个母版目录并校验文件哈希，不能以固定文件名单遗漏新模板。

## 跨类目回归

- 2026-10-11 用户进一步规定公共池/专属池这一处模型交互只负责内容与实质正确性，不要求填字段、表格或维护查询；格式全部由程序维护，不因回复形式不同拒绝内容。程序先选定单资料及单商品上下文，模型返回自然内容或语义判断，程序建立标题、出处、索引、关系和持久结构。结论确实不明确才补问内容；未完成判断不得冒充通过，不能把表达形式差异当内容错误。

- 2026-10-11 用户要求公共池与商品专属池生成简单有效，异常条目提示原因且不入池，但不能阻止合格内容继续生成、核对和保存。生成与审核按内容项隔离；内容违规、格式未完成和服务异常分别记录，不能把未审核内容当作通过，也不能因一条无依据旧福利或空引用否决整池。重试为可选操作，保留原始返回、旧快照和用户修改；程序维护编号及关联。完整P0属于控制信息，不能放入事实原文候选或输出为商品内容。

- 2026-10-11 用户允许话术模型分别返回主回答和延展两个内容字段，程序保存、关联和组合；延展审核可以携带主回答作为只读参考，只返回校对后的延展，不能改动主回答。格式可解析与语义分工、事实正确分别验证，不能把字段齐全等同于内容正确或主回答已核对；完整有效P0仍在每次调用携带。此项当前为离线原型测试，生产接入另据代码调用与发布说明。
- 2026-10-11 用户明确允许对难以防范的事实边界采用模型生成的短句模板，程序将已核对的模板提供给正文模型。该授权限定于本次相关风险内容，不把整篇商品范文混入长期风格。模型仍只返回内容，程序维护商品、任务、资料、P0及母版绑定；模板不是新增事实，资料更新后旧模板不得直接沿用。未核对或未通过的模板跳过，完整正文继续按有效P0与事实处理。此条是用户随后确认的模板使用例外。
- 温度对照必须保持相同输入、模型、P0、母版和其他参数，分别记录温度、原始返回、事实判断和重复情况；同商品不同任务及同任务事实更新都要覆盖。模型自评通过率不能等同于人工核对的事实正确率，不因小样本结果声称某温度能保证边界。
- 2026-10-11 用户确认备用局部改写由程序与后台审核角色配合完成，不能依赖本聊天现场逐句纠正。字面命中先按有效P0范围、条件和语义核对；正常建议、体验等不能仅因出现“最”等字判违规。确认需要修改时，模型只返回被选句段的新内容，程序绑定原文指纹与字符范围、保留其余文字、组装后再审；硬规则不能被模型放行覆盖。原文变化则旧替换失效。无依据的功效、时效、金额或承诺不能仅换近义词保留原主张；改写仍失败则本段跳过继续供料，不无限重试或停播。测试、接口接入和生产发布分别据实际证据说明。

- 2026-10-11 用户明确要求不能只给当前直播间、当前商品打补丁。涉及生成、规划、互动、事实传递或审核协议的修改，必须用不同类目、不同商品做模拟回归，覆盖成品与原料/部件、实物与服务、多规格价格、条件分支、未知事项、旧行业房间换品和自然语言纠正；先验证通用母版与程序规则，不在生产代码写商品特例。
- 跨类目离线模拟采用当前实际模型并完整携带有效P0，模型调用完成、结构可用、P0通过和事实关系正确分别统计。核对模型自评不能当成事实合格率，逐段保留原始请求、返回、程序绑定和跳过原因。模拟生成不代表已播放或已上线；发现跨类目新问题后先处理通用原因再决定发布范围。
- 2026-10-11 用户确认可在话术预览中直接输入自然语要求，后台整理出完整但不扩大目的的表达约束；只有用户点击采用才记录生效。程序维护事实、商品范围、用户、版本、出处和待采用草稿，大模型只返回约束内容。草稿及澄清不进入正式生成或审核。已采用的表达约束作为独立纠正记录，后续主线、模拟和真实互动共享加载与审核；不改写商品事实或二级对象关联。约束按语义覆盖同义和暗示表达，不能把早餐限制误扩成所有用餐场景或早上发货禁令。有效P0仍为最高位，正在播放的音频不回滚。


# 当前聊天工作区的发布约定

## 公共池不独立轮播（2026-10-16 已上线）

- 用户明确：商品出场只按商品权重计算，不按各商品卖点数量分配；公共内容只按需穿插，不单独轮播。公共任务不能进入商品内部卖点配额，也不能在复读、失败和耗尽回退时补进来。保留公共材料及事实，不擅自重新分类商品自身卖点。
- 已实现显式Common任务的滚动候选排除与计数保护，覆盖旧内容池common身份；生成、事实组装、四层提示词、审核、TTS、播放管线及商品/卖点既有权重规则不变。不改写方案或重新初始化卖点池。
- 巴国布衣正式V5/id118离线20轮，同样77个主体名额：旧版1号18、2号12、公共47；新版1号35、2号42、公共0。两个受影响完整Go包835项通过、58环境跳过，无失败。文档docs/2026-10-16-product-only-scheduling.md；本聊天证明work/product-only-scheduler-20261016。回放使用程序占位采纳，不是模型/TTS或真实直播验证。
- 用户要求先上线本版测试。北京时间23:18:35—23:18:38发布20261016-product-only-scheduler-80c3448，源码80c3448c22ea6ad5aaba65723803e250769fc08b，已合入生产互动商品范围修复af8f0bc；仅替换Management至PID967042/SHA256 800c7b07a60d99f35a37d16f97c9fded6177a6092957c288dea55b487aef2ed7。三次AI/Core空闲守卫通过，未为发布停止直播；Core775073/网关775930未重启。2604个旧无关文件、1751前端及完整备份、170母版、115行业文件、10公网检查、系统设置/模型池和10类客户方案资产均一致。最新整合两个完整Go包836项通过、58跳过，无失败；新场实播效果待用户测试。
- 公共资料保留可用，不代表已经保证口播按需提及；本版未新增公共触发器。2号卖点数量不足已查到模型原始输出即只有一个商品卖点，仍在独立排查，未混入本次发布。发布证明位于本聊天work/product-only-scheduler-20261016/deployment-report.json及production-verification.json；完整前端备份指针已更新。
- 23:22按既有发布收尾授权及共享锁完成清理，释放7.97GiB：9个过期发布、44个旧程序备份、13个旧包、9个闲置测试程序。保留当前、最近两次回滚、Core/网关引用及最近两小时产物，保护内容与三服务一致。当前仅移除11个无用旧bin备份，实际发布文件现2595个；1751前端及完整备份仍一致，最新指针已重新核对。本聊天清理证明work/product-only-scheduler-20261016/cleanup/report.json，不再用清理前完整文件数作为实时基线。

## 保留已上线的前端（2026-10-13 用户明确要求）

- 用户由 WorkBuddy 部署了前端，要求后续更新不得覆盖；截图确认保护对象包括智能体方案的“商品资料补充”页，左侧待确认资料、右侧资料助手及顶部五个入口。
- 后端发布以发布前服务器实际 `current` 为基础，完整保留其 `web/`、`web-desktop/`、`web-customer/`、`web-sales/`、新增静态页和现有 Nginx 路由；只覆盖本次已授权的后端文件。以发布前后的文件清单和哈希确认前端未变。
- 前端修改先取得包含该页面和其他已上线改动的最新源码，再合入本次改动、解决冲突并构建。不得拿旧工作目录的完整前端产物替换生产。各工作目录不自动同步，Git 提交号也不能证明服务器未被后续原地更新。
- 同一时刻只允许一份整合后的发布包切换生产；发布前再次确认实际版本及文件指纹，发生并行更新则重新整合，不强行覆盖。
- 记录和备份见 `work/frontend-preservation/latest.json`；保护说明见 `work/cosyvoice-integration/docs/2026-10-13-frontend-preservation.md`。备份用于核查和恢复，不把历史哈希作为后续版本必须回退的目标。
- 当前规定已记录，生产没有因此更改。旧全量构建脚本会复制本地三个前端，不能在源码尚未整合时直接发布。

## 保留用户声音缓存运维入口（2026-10-13 已上线）

- 已发布 `/opt/xiaolan/releases/20261013-user-voice-cache`，增加“直播运维 → 用户声音缓存”，可按用户设置主线成品复用、互动成品复用、声音保留时间，并恢复平台默认。沿用已有运维设置权限和管理审计，不自动改变用户开关或平台默认。
- 最新整合源码在 `work/user-cache-settings-source`，说明见该仓库 `docs/2026-10-13-user-voice-cache.md`。后续前端更新也必须保留这次新增入口、页面、权限路由和接口，不能从尚未合入缓存设置的旧目录全量覆盖。
- 本次继承发布前实际current全部文件，仅覆盖管理二进制与桌面/兼容入口、添加新前端资产；WorkBuddy商品资料补充相关源码未变，客户/销售手机与Nginx路由未变。实际发布核对见 `work/user-cache-settings-proof/production-verification.json`。

## 保留AI动态生成默认可用（2026-10-13 已上线）

- 当前发布 `20261013-dynamic-default-r2`：所有直播间默认可用AI动态生成，无需运维逐房间开通。旧 `dynamic_authorized=0` 和旧页面提交false不能关闭功能；选择模式、发布、实时生成、自动更新调度均不再以此标记为门槛。
- 功能可用不自动改变直播间已选模式，不自动开播。客户对运维的具体直播间协助授权、岗位权限、版本/模式校验、更新设置、CAS和审计仍保留。
- 最新整合源码仍在 `work/user-cache-settings-source`，说明见该仓库 `docs/2026-10-13-dynamic-default.md`。包含此前用户声音缓存设置、WorkBuddy最新正文审核快照与P0局部改写修复；不能从未合入这些修改的旧工作目录全量覆盖管理二进制。
- 发布核对见 `work/dynamic-default-proof/production-verification.json`，前端完整备份及当前指纹仍由 `work/frontend-preservation/latest.json` 指向。后续发布继续继承实际current所有旧哈希资源与最新修改。


## 主线整合源码（2026-10-14）

- WorkBuddy 的 `refactor/speech-generator-v2`、已上线未提交源码、受保护前端分支及本聊天用户缓存/动态默认可用提交统一合入 `main`。合并过程保留各来源历史，不重置其它开发目录。
- 当前完整整合目录为 `C:/Users/19918/Documents/Codex/2026-10-09/zh/work/main-merge-20261014`。其它目录不会自动同步，后续开发先获取主线最新提交再开展修改。
- 源码合并不等同生产发布；后续发布仍核对服务器实际current及原地更新，继承最新静态资源与Nginx路由。


## 商品资料补充新版界面（2026-10-14 已上线）

- 按用户图2改版资料补充区域，已于北京时间2026-10-14 08:57上线 `/opt/xiaolan/releases/20261014-supplement-assistant-6d2b5c1`。发布源码6d2b5c1；完整源码仍在 `work/main-merge-20261014` 及 GitHub main。
- 保留新资料卡、文档插画、助手头像、对话气泡、一体化输入框、TXT/Markdown读入、快速示例与原有补全/确认保存流程。文件及示例只预填输入，不能自动提交或保存为事实。
- 仅替换web和web-desktop两个入口HTML，新增哈希资源；1426个原有非入口文件哈希一致，三个服务PID/运行二进制、手机端和Nginx路由未变，没有重启直播。
- 最新前端备份指针 `work/frontend-preservation/latest.json` 已指向本版，经实际服务器指纹核对1094个前端文件；验证见 `work/supplement-ui-proof/production-verification.json`。原版本和旧哈希资源继续保留，后续发布仍以当时实际current为基础。

## 商品资料补充嵌入布局修复（2026-10-14 已上线）

- 当前前端已于北京时间09:11更新为 `/opt/xiaolan/releases/20261014-supplement-layout-2f44f4b`，源码2f44f4b，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 根组件必须保留确定的100%宽度、最小宽度0及1560px最大宽度，避免父级网格与内联尺寸容器共同导致缩窄。响应规则按组件实际宽度判断；验证须包含方案页真实父级和全局样式，以及有数据的补全清单和对话，不能只用独立组件预览。
- 新版资料补充交互与所有保护页面继续保留。本次仅更新两个入口HTML并新增哈希资源，1463个原有非入口文件一致，服务PID/二进制、手机端和Nginx路由未变。
- 最新前端备份指针 `work/frontend-preservation/latest.json` 已更新，1130个前端文件逐项匹配实际服务器。验证见 `work/supplement-layout-proof/production-verification.json`，说明见主线 `docs/2026-10-14-supplement-layout-fix.md`。后续发布仍以当时实际current为基础。

## 商品资料补充对话格式化（2026-10-14 已上线）

- 当前前端已于北京时间09:21更新为 `/opt/xiaolan/releases/20261014-supplement-format-664ea03`，源码664ea03，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 助手回复使用ChatMessageContent显示段落、标题、列表、引用和代码，句内强调复用StyleDisplayText，不再直露常见Markdown标记。只调整显示，原消息、模型历史、事实候选及保存内容不改写；不使用v-html执行模型文本。
- 上次根组件确定宽度和按容器响应规则继续保留。19项回归与完整方案页/手机宽度显示验证通过；本次仅更新两个入口HTML和新增资源，1500个原有非入口文件、服务PID/二进制、手机端及Nginx一致。
- 最新前端备份指针 `work/frontend-preservation/latest.json` 已更新，1166个前端文件逐项匹配实际服务器。验证见 `work/supplement-format-proof/production-verification.json`，说明见主线 `docs/2026-10-14-supplement-chat-format.md`；后续发布以当时实际current为基础。

## 补全清单与对话分栏（2026-10-14 已上线）

- 当前前端已于北京时间09:52更新为 `/opt/xiaolan/releases/20261014-supplement-split-ee08a7c`，源码ee08a7c，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 用户本次明确将原左侧待确认资料替换为补全清单；右侧对话始终挂载，问这个直接在当前历史中发起问答，无清单/对话视图切换。候选资料改为右侧输入框下方确认区，仍需用户确认保存。后续必须保留这一新结构。
- 单独刷新/跳过清单不能重置对话、输入或候选。问这个保留尚未提交的输入；文件/示例仍只预填。清单按方案、租户、房间和账号隔离，旧读取取消、迟到响应丢弃；左侧错误不能阻塞右侧自由对话。
- 根组件确定宽度、按容器响应、回复安全格式化与受保护运维功能保留。25项回归及完整父级布局5项验证通过。本次仅更新两个入口HTML并新增资源，1537个原有非入口文件、服务PID/二进制、手机端与Nginx保持一致。
- 最新备份指针 `work/frontend-preservation/latest.json` 已更新，1208个前端文件逐项匹配服务器。验证见 `work/supplement-split-proof/production-verification.json`，说明见主线 `docs/2026-10-14-supplement-split-panels.md`；后续发布仍以当时实际current为基础。

## 换下一批清单按钮（2026-10-14 已上线）

- 当前前端已于北京时间10:23更新为 `/opt/xiaolan/releases/20261014-supplement-next-batch-e82534e`，源码e82534e，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 原刷新清单按钮改名“换下一批清单”，保留紫色实底、加粗白字、切换图标和至少42px高度；清单容器不足360px时按钮独占一行，不能挤压标题。原读取操作和禁用条件未变，未提交输入、对话和候选继续保留。
- 11项既有清单/对话回归及真实父级桌面/手机验证通过。仅更新两个入口HTML和新增资源，1583个原有非入口文件、运行服务及Nginx一致；保留本次发布前服务器已经原地更新的管理二进制和主播提示词，不能用历史源码或哈希将其覆盖回旧版。
- 最新备份指针 `work/frontend-preservation/latest.json` 已更新，1244个前端文件逐项匹配服务器。验证见 `work/supplement-next-batch-proof/production-verification.json`，说明见主线 `docs/2026-10-14-supplement-next-batch-button.md`；后续继续继承当时实际current。

## 补全清单文字增大（2026-10-14 已上线）

- 当前前端已于北京时间10:34更新为 `/opt/xiaolan/releases/20261014-supplement-list-font-e051f4c`，源码e051f4c，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 清单卡片标题、说明、问题提示、标签、问这个/跳过按钮及底部补充入口统一增大1px，保留主次层级。右侧对话与换下一批清单按钮保持原样。
- 11项既有清单/对话回归、生产构建及完整父级桌面/手机验证通过，页面与卡片无水平溢出，未提交输入保留。仅更新两个入口HTML和新增资源，1620个原有非入口文件、运行服务及Nginx一致，保留已原地更新的管理二进制及主播提示词。
- 最新备份指针 `work/frontend-preservation/latest.json` 已更新，1280个前端文件逐项匹配服务器。验证见 `work/supplement-list-font-proof/production-verification.json`，说明见主线 `docs/2026-10-14-supplement-checklist-font.md`；后续继续继承当时实际current。

## 更多菜单外部点击收起（2026-10-14 已上线）

- 当前前端已于北京时间10:46更新为 `/opt/xiaolan/releases/20261014-strategy-more-dismiss-ef6a888`，源码ef6a888，完整源码仍在 `work/main-merge-20261014` 及GitHub main。
- 更多菜单打开后，点击菜单外部自动收起；捕获阶段监听兼容其它区域阻止冒泡的点击，菜单内操作与删除二次确认保留。页面卸载移除监听，重新进入仍正常。
- 完整LiveStrategyView合成数据桌面/手机交互、页面卸载及重新进入验证通过，14项既有导航/清单/对话回归和生产构建通过。仅更新两个入口和新增资源，1659个原有非入口文件、运行服务、手机端及Nginx一致。
- 首次发布前守卫发现管理二进制和主播提示词并行原地更新，未切换；前端哈希未变，重新核对实际基线后继承管理进程795036与新提示词发布。不能用历史二进制或提示词覆盖回旧版。
- 最新备份指针 `work/frontend-preservation/latest.json` 已更新，1316个前端文件逐项匹配服务器。验证见 `work/strategy-more-dismiss-proof/production-verification.json`，说明见主线 `docs/2026-10-14-strategy-more-menu-dismiss.md`；后续继续继承当时实际current。



## 腾讯精品官方声音（2026-10-14 已上线）

- 当前发布 `/opt/xiaolan/releases/20261014-tencent-voices-f4cc1d5`，北京时间13:01上线，源码f4cc1d5，完整源码在 `work/main-merge-20261014` 与GitHub main；包含WorkBuddy正式提交21404bf风格提取重构和此前本聊天改动。后续不能从旧声音/管理源码覆盖。
- 声音中心官方声音展示14款腾讯实时精品音色，支持输入原文试听；不再受CosyVoice声音库选择影响。我的声音保留Qwen/CosyVoice复刻与设计，旧官方ID和默认声音兼容保留。打开或试听不改变房间已选声音，只有点击使用才保存选择；现有运行快照机制保留。
- 腾讯凭据已由用户通过本机窗口保存至服务器环境配置并由新Management加载，不在Git或聊天中保存密钥值。WebSocket返回完整16kHz PCM后封装WAV，Core沿用24kHz转换。供应商、模型与音色ID按服务端目录绑定，主线/互动/桥接均支持腾讯；不混入超自然或大模型档。
- Management实际PID800329，SHA256=b869b5711a52695905b87324d78e02874d821c53bb1acf0e7451d25714a0acf2；Core/网关未重启。仅覆盖管理二进制、web和web-desktop入口并新增资源，1698个原有无关文件、手机端、提示词和Nginx一致；平台缓存默认值未改。
- 最新前端备份指针 `work/frontend-preservation/latest.json` 已更新，1352个前端文件逐项匹配服务器。证明在 `work/tencent-official-voices-proof/production-verification.json`，说明见主线 `docs/2026-10-14-tencent-premium-voices.md`。
- 腾讯真实问候接口及用户411字符原文测试成功；智柯75.46秒、智瑜80.55秒。仅生成试听文件，没有写事实、方案或直播队列，没有自动切换房间声音。协议/目录/HTTP/互动路由相关测试通过，前端14项回归通过；全量Go的27项既有失败在未修改整合基线也相同，不能记为全套通过。
- 发布仍以当时实际current及文件指纹为基础，持共享发布锁、确认空闲后只重启已授权服务，不回退历史提示词/前端/二进制。

## 智能体工作台双页布局（2026-10-14 已上线）

- 当前前端北京时间13:24已更新为 `/opt/xiaolan/releases/20261014-agent-workspace-pages-e3151af`，发布源码e3151af，完整前端源码已合入GitHub main。本次工作目录为 `C:/Users/19918/Documents/Codex/2026-10-14/d-myskill-skill/work/model-thinking-toggle`。
- 用户确认“智能体配置”页面放内容任务、未入池提示、待更新资料、公共池/商品专属池和方案检查提示；“生成与预览”页面放初始化直播智能体、主线方式、播放器及其以下全部参数和业务模块。不得重新把两部分上下堆在一个页面，也不把初始化挪回配置页。
- 双页切换保持模块挂载，保留参数、池展开状态和播放器。保存沿用统一保存流程；切换不自动保存、初始化、生成或开播。房间或方案变化继续按原上下文key隔离。
- 完整LiveStrategyView合成数据8项桌面/手机布局检查及14项既有回归通过，类型检查和生产构建通过。未修改主线基线构建的45个资源与发布前实际前端一致。发布时1737个原有非入口文件及三个服务保持一致，8项公网资源验证通过；最新前端备份指针已更新，1394个前端文件逐项验证。
- 管理服务随后13:24:46被并行原地更新为PID801647，SHA256=7b1b016f0cba26b6d59494d63eedf708112d896dd6be44845c78cdcffe9a4605，本次核对保留该后续版本。后续发布仍继承当时实际current，不能把管理服务回退到本次发布前的800917或更早版本。
- 发布证据位于本次工作目录上级的 `agent-workspace-pages-proof/production-verification.json`；共享备份指针仍为 `C:/Users/19918/Documents/Codex/2026-10-09/zh/work/frontend-preservation/latest.json`。说明见 `docs/2026-10-14-agent-workspace-pages.md`。


## 声音模型后台统一配置（2026-10-14 已上线）

- 当前声音配置发布为 `/opt/xiaolan/releases/20261014-voice-settings-9f9e204`，北京时间13:49上线，Management PID803890，SHA256 fc507eed927d88c8cff8c132c1dcd7c60b501f5a79e42cef4f740995043c2e1f；仅重启管理服务，Core/网关未重启。
- 用户声音中心不展示模型名、价格、顶部或卡片/创建弹窗模型下拉框。模型选择集中在“系统设置 → AI 与智能体 → 声音模型配置”，沿用超级管理员权限、管理审计和事务修订校验；客户不能通过旧模型偏好接口改模型。
- 自定义模型当前为CosyVoice 3.5 Flash，官方库腾讯精品。所有已有声音可见并保留原ID、样本和房间选择；客户明确试听/使用时需要转换则真实从样本复刻新编号，不把旧编号改绑另一模型，不自动替换已运行快照。录音主线自动复刻也采用后台配置。
- 官方声音仅提供成年中文男声/女声，隐藏外语和童声，共11款腾讯音色；完整旧目录只保留已保存声音兼容。官方卡片使用成人头像。
- 已整合智能体双页布局e3151af/2f8fdeb，以及并行原地发布的WorkBuddy方案删除最新未提交源码（180e3b9及9f9e204）；没有修改原开发目录。后续不能从旧声音或旧管理源码覆盖。完整源码仍在work/main-merge-20261014及GitHub main。
- 1781个原有无关文件、提示词、手机端、Nginx和缓存默认值一致；最新前端备份指针work/frontend-preservation/latest.json已验证1440个前端文件。证明work/voice-system-settings-proof/production-verification.json；说明docs/2026-10-14-voice-system-settings.md。
- 五个相关Go包、前端构建、14项导航/资料补充回归及桌面/390px浏览器验证通过。数据库集成实验因现有账号无创建隔离schema权限未成功，不宣称通过；生产仅只读确认新配置及鉴权，未生成真实测试声音或操作直播队列。以后发布仍继承当时实际current并持共享锁、核对指纹及空闲状态。


## 文字模型账号池（2026-10-14 已上线）

- 当前发布 `/opt/xiaolan/releases/20261014-model-pool-7511a9c`，北京时间21:13上线，部署源码7511a9c846b4d4e2c2ca39da521de43473955e45；Management PID822987，SHA256 e1c06bb8d88d6e0b731a4b727841c77d1937aeb1efeaaa5bbc78130ebca4886b。只重启管理服务，Core/网关未重启。
- 后台入口“系统设置 → AI 与智能体 → 模型池配置”；账号Key加密保存，可读取模型、手动设置名额、暂停新分配和查看占用。用户明确指定现有账号100个名额，已安全导入当前qwen3.7-flash并启用，系统审计记录用户授权的初始化操作，不在Git/聊天保存Key。
- AI首次启动在同一事务领取账号和模型，口播、审核、规划、互动及回归桥统一使用同一分配；暂停及Management重启保留，实际AI终态释放。并发名额使用数据库持久化锁和当前读取，不超分，不按采集状态或TTL收回；旧运行场次不能占用或释放替换场次的名额。TTS、声音识别和非直播后台分析沿用原配置。
- 完整源码独立开发于work/model-pool-source，已整合WorkBuddy1608ea6与GitHub最新主线02088c9，并推送main；work/main-merge-20261014同步。保留WorkBuddy称呼、人工抢答昵称、资料补充及主线直播下载/24小时记录等改动，没有修改E:/speech-generator-build的原开发文件。后续不能用未整合模型池或直播下载的旧管理/前端源码覆盖。
- 2249个原有无关文件、提示词、手机端、缓存默认值和Nginx保持一致；前端完整指针work/frontend-preservation/latest.json已核对1578个文件。证明work/model-pool-upgrade-proof/production-verification.json、configuration-verification.json；设计说明docs/2026-10-14-model-account-pool.md。
- 真实本地MySQL随机隔离schema验证了并发容量、重复申请、暂停/重启、密钥快照、释放、旧场隔离和配置CAS；新接口鉴权及虚拟供应商跨房间同Key/模型调用测试通过。前端21项受保护功能回归、桌面/390px模型池交互和生产构建通过；11个公网资源校验通过。五个Go包的29项既有失败与未修改02088c9逐项一致，无新增失败，不能称全套通过。未开播或向真实模型发送测试正文，真实直播使用待下一场验证。


## 模型池占用详情分页（2026-10-14 已上线）

- 占用房间从模型池主页面移入账号行“查看占用”的详情弹窗；默认每页10个，可选20/50，账号之间按池记录编号隔离。切换页大小/重新打开重置第一页，刷新减少房间后修正页码；零占用、暂停状态、Esc/遮罩关闭、焦点恢复及手机卡片显示保留。
- 前端22:36发布20261014-model-pool-occupancy-59d6812，仅更新两个入口并新增资源，2293个非入口文件与三服务一致。随后发现管理服务被后续旧源码1608ea6原地发布为PID824750，模型池接口返回404；在确认AI空闲后，于22:42发布整合后端20261014-model-pool-integrated-59d6812，Management PID826314/SHA256 46cd71aeca3bbcc0abbf9c1611b490d3dd5dc201f4acffc0752e351bbd40386b。仅重启Management，Core/网关未重启，2335个其它文件与1618个前端文件保持一致。
- 部署源码59d68122462fc38d60adbfa7871187d0a4248117，包含模型池、直播下载和WorkBuddy c0ad094最新TTS前缀修改；用户后来保存的qwen3.7-flash-2026-07-15及100个名额原样保留，没有重新导入Key或覆盖账号设置。三个模型池入口线上返回需登录401，已恢复路由。
- 后续管理发布必须以最新GitHub main完整整合模型池与直播下载，不得直接用缺少这些功能的E:/speech-generator-build旧main全量覆盖。原WorkBuddy工作目录未由本聊天修改；前端仍以发布时actual current及指纹为准。
- 浏览器虚拟24条占用验证10/20/50分页、末页、刷新收回页码、按账号隔离、空态、关闭/焦点和390px显示，21项既有前端回归与构建通过；恢复后端的模型池/调用路由/直播下载相关定向Go测试通过，未启动真实直播或生成测试内容。证明work/model-pool-occupancy-proof/production-verification.json与integrated-production-verification.json。最新前端备份指针work/frontend-preservation/latest.json已指向当前整合发布并验证1618文件，实际备份复用哈希完全一致的占用页版本。

## 同房间跨场主线话术库（2026-10-15 已上线）

- 北京时间02:28:30发布 `/opt/xiaolan/releases/20261015-speech-library-711b164-r2`，部署源码711b164e872871fd0d473ef1c82f022cc8456a92，Management PID836984/SHA256 32e69cad21c1d762afac1e874e2ac820034784948df68d15ac1bc6a896cbb156。Core775073与网关775930未重启；2337个旧无关文件一致，1664个前端文件和12个公网入口/资源核对通过，完整备份指针work/frontend-preservation/latest.json已更新。
- MySQL持久主线正文库按环境/客户/房间/方案/卖点×角度隔离，默认容量3、1/2/3+条复用30%/60%/70%、同条实际起播后冷却180秒。只有当前生效事实、条件、商品与福利范围、卖点/角度卡、风格、评分、P0、表达要求、模型及提示词全部匹配才复用；变化后退役，不因改回旧值复活。补充另一张方向卡不影响原样有效的卡。仅音色/TTS模型/语速变化只重做声音，保留有效文字。
- 完整音频包含准确PCM时间轴、字幕和安全句末切点；上传前固定引用，实际PCM起播幂等计数，完成/下一条实际主线起播释放旧引用，暂停与进程重启不释放。退役至少24小时且无活动引用才删除本库明确五对象。开场/讲解/辅助/预览/依赖前文及明确到期活动不进长期库，互动沿用现有流程。
- 后台“直播运维 → 用户声音缓存”新增“主线跨场话术库”中文配置与统计，保留原用户覆盖、权限、审计及CAS。发布前后实际配置指纹一致：主线true、互动false、临时保留5小时。互动false的数据库更新时间为北京时间10月15日00:14:55，早于本次发布；不能按此前23:59历史记录推断当前互动仍true。本次没有写两个开关或账号设置。
- 已整合WorkBuddy1024d36及原模型池/直播下载/声音配置，GitHub main和work/main-merge-20261014同步，开发仍在work/model-pool-source；E:/speech-generator-build原目录未改。后续不得用缺少新库或模型池的旧管理源码覆盖。
- 五项真实隔离MySQL测试、缓存/审核回执/声音失效/句末切点专项、前端构建与20项保护回归、模型池分页和手机设置交互通过。全管理测试与未修改基线同有30项旧失败、无新增，不宣称全套通过。真实OSS五对象写入/HEAD/读取/签名/删除通过且全部清理，桶无到期生命周期。未启动真实直播、未发送真实模型/TTS口播，当前库空，实际命中率待正式开播验证。
- 第一次切换因校验脚本误用GET检查POST模型列表而自动回滚，按真实方法校验后r2成功；未绕过守卫。证明work/speech-library-proof/report.md、production-verification.json、deployment-report.json、postcheck.json、storage-verification.json；设计见docs/2026-10-15-room-speech-library.md。

## 互动与衔接语下载HTTPS修复（2026-10-15 已上线）

- 用户截图21:28:42互动对应tenant14/room26/session283/asset698，旧下载和归档仅允许HTTPS，供应商却返回HTTP的DashScope结果OSS签名地址，导致invalid source audio URL。真实GET验证同一对象改HTTPS可读取，HEAD403仅说明GET签名不支持HEAD，不能据此判断文件不存在。
- 北京时间07:00:34上线 `/opt/xiaolan/releases/20261015-interaction-download-853ea9d`，部署源码853ea9d88283606341e4a4362567bcbdc847c855，Management PID845418/SHA256 90f53adc8eff70030c8bdba06937d701aadc2a2d77f555d288dbca6542c49dc7。只改管理程序，2386个其它文件、1664个前端文件及3公网入口核验一致；Core775073、网关775930、Nginx、配置与模型池未改。
- 直接下载和私有24小时归档统一把DashScope结果OSS的历史HTTP地址升级为HTTPS，签名及转义不变，非供应商HTTP/用户信息/非默认端口/伪造域名仍拒绝，无HTTP降级。历史条目无需重生成或改文案，按原任务重试自动补存，保留从原播放开始24小时。
- 07:02:41已核实asset698补存成功，完整GET200、948524字节RIFF，SHA256 7f2d286b682e0b73849646921025872abfa6b111fafd9d9b40fc0bc1c9e31475，到期仍10月15日21:28:42.790；同类记录继续自动补存，期间直接下载支持HTTPS原文件。供应商原WAV头为流式占位长度，实际载荷约19.76秒、列表22.48秒来自既有播放记录，本次不改TTS/音频，不据声明帧数报告12小时。
- HTTPAPI完整包与下载专项通过，未调用真实模型/TTS或操作直播。已推送GitHub main并同步work/main-merge-20261014，E:/speech-generator-build未改。保留同房间话术库、模型池和WorkBuddy页面。证明work/interaction-download-proof/report.md、deployment-report.json、production-verification.json、recovery-verification.json；前端备份指针work/frontend-preservation/latest.json已按全量哈希复用原备份并更新到本版本。


## 直播下载主播编号与互动合并（2026-10-15 已上线）

- 北京时间07:19:21上线 `/opt/xiaolan/releases/20261015-download-groups-d976350`，部署源码d976350a46ff28fd96b4f9cad5356022ecea1b0c，Management PID846891/SHA256 567d21f7db7ed0091a04974714797cbcf42bc477373b58d9e0d50e553187f4fa。只重启管理服务；Core775073、网关775930未重启，2385个旧无关文件、手机端、Nginx、模型池及全部系统设置保持一致。
- 下载页主线显示“主播说 00001”起的五位序号，按本场已播放主线排序，先编号后筛选/分页；互动改名“直播间互动”。仅同一客户/房间/场次及相同决策编号的已播放回答与桥合并，文字直接衔接，不再显示独立衔接语记录。纯文字整场导出也采用同一分组。
- 下载互动输出回答→实际已播放桥的单个完整WAV；没有桥下载原回答。相同参数保留原PCM、修复供应商占位长度文件头，不增加静音尾或截音；不同参数使用现有本地FFmpeg统一。任一部分不可用时提示失败，不将半份音频当作合并成功。保留原24小时保留、HTTPS地址校验与客户运维授权，未修改原始播放记录、音频归档或直播调度，未调用模型/TTS。
- HTTPAPI完整包、专项合并/隔离/筛选/文件头/混合采样率/部分失败测试通过，20项前端保护回归及生产构建通过；1440/390px浏览器验证通过。283场只读核对61条原始记录合并为32条主播说和18条互动，11组桥均准确对应；真实归档一组导出GET200，完整WAV1136684字节、23.68秒。该验证是历史文件导出，不是新一场直播播放验证。
- 已合入WorkBuddy最新1c6d2d5（Core用户维度与文档）并推送GitHub main，work/main-merge-20261014同步；本次未部署其Core改动，E:/speech-generator-build原目录未改。前端1704文件及12个公网入口/资源核验一致，最新备份指针work/frontend-preservation/latest.json已更新。证明work/download-group-proof/deployment-report.json、production-verification.json、archive-replay-verification.json、checks.json；说明docs/2026-10-15-session-download-groups.md。后续仍以实际current及最新源码整合发布，不得用旧下载/模型池/话术库源码覆盖。


## 商品卡字段名称编辑（2026-10-16 已上线）

- 北京时间09:36:32上线 `/opt/xiaolan/releases/20261016-product-field-names-e34fe12`，部署源码e34fe1238a293de45a5a994162a0dc7a094fc408，Management PID907639/SHA256 edce4c270440cec487aef8f98eacfe0ca872da42e4e3cc451f79cb5b84db42b6。只重启Management，Core775073和网关775930未重启；2583个原有无关文件、系统设置与模型池、手机端和Nginx一致。
- 商品卡规格/销售价/数量/适用的同行修改笔同时编辑名称与内容，提供保存/取消，名称按商品保存，刷新及复制保留。输入间换焦点不提前保存，行外沿用自动保存；空名与控制字符拒绝，文字至少16px，长名和320px宽度不溢出。内部字段键、商品ID、福利关系保持稳定，原客户授权及版本CAS保留；旧客户端更新不抹去改名。版本历史与新方案快照携带名称，生成事实同步使用名称，运行快照不擅自更改。
- 已合入最新GitHub主线及WorkBuddy ad864cf/33473da的方案保存/房间绑定修改，并推送main；完整开发源码work/model-pool-source，整合副本work/main-merge-20261014同步。原E:/speech-generator-build未改。上线前捕获同名release内管理/桌面并行更新，重新按实际文件重建备份与基线后发布，后续仍不能按历史指纹覆盖。
- DB/Model/HTTPAPI三个完整Go包、真实本地隔离MySQL持久化/复制/版本冲突/旧客户端/范围隔离、22项前端保护回归与构建、完整方案页四种宽度浏览器检查通过。两个JSON列核验、12个公网入口/资源与1739个前端文件/备份核验通过；最新指针work/frontend-preservation/latest.json已更新。未修改真实客户商品或发送模型/TTS请求。证明work/product-field-names-proof/deployment-report.json、production-verification.json、checks.json；说明docs/2026-10-16-product-field-names.md。


## 巴国布衣正式版本保存修复（2026-10-16 已上线）

- 北京时间16:10:31上线 `/opt/xiaolan/releases/20261016-plan-save-64e6064`，仅更新web和web-desktop两个入口并新增43个资源；2549个原有无关文件、三个实际运行服务、提示词、手机端、Nginx及全部系统设置/模型池保持一致，没有服务重启。
- 根因是方案列表只有room_count而无room_ids，保存页用room_ids推断绑定，将已绑定实时方案误判为未绑定并无错误退出。现在保存前读取房间实际绑定，内容更新成功后继续创建正式版本；失败、参数/账号/房间/方案变化、迟到回执均明确处理，只有当前范围且版本递增的真实回执显示本次成功。
- 已通过客户原正常登录页面点击保存，16:12:04.593正式生成巴国布衣方案5的V3/id116，tenant14/room26发布记录指向116。新快照为ai_dynamic、wheel43，包含1号普通商品5卖点21角度、2号主推商品7卖点35角度及当前product-focus-v3策略。旧V2/id115保留；未开播或切换正在运行快照。正式保存验证与新场实际讲解频率验证须区分。
- 主线完整整合源码5be851d包含WorkBuddy ef480ce及捕获的后续源码，保留原字段名称编辑等功能；GitHub main及work/main-merge-20261014已同步。桌面基线用现用WorkBuddy源码重建49产物全部匹配后，仅合入保存修复，源码64e6064另存GitHub codex/production-plan-save-20261016分支。此次不部署任何新后端或提示词，不修改E:/speech-generator-build原文件。
- 36项主线前端回归、桌面实际源码15项保存流程测试、两份生产构建及完整方案页桌面/390px交互通过；整合Go model/db完整包通过。真实页面显示本次保存成功V3且DB已发布，12个公网入口/资源核对通过。最新完整前端备份指针work/frontend-preservation/latest.json已核验1751文件。证明work/plan-version-save-proof/production-verification.json、formal-save-verification.json、deployment-report.json与production-saved-v3.jpg；后续仍继承发布时actual current并重新核对指纹。


## 多商品启动编号修复（2026-10-16 已上线，后续统一准备链路正在验证）

- 16:14:57起巴国布衣V3/id116的293、294、296—300场启动约30—43毫秒即formal_mainline_failed，并非TTS失败。ExpandProductSellingPoints的编号带商品前缀，New却改成无前缀，导致真实快照56任务0归属匹配；不能归因为客户池不足。
- 16:21:09上线20261016-product-task-start-9c71849，仅替换Management，PID949136，SHA256 c16acf37073c76a87804575ab57501941af49d7ad346e8901dd03a2376c86da7，源码9c7184967f69fb04c8602b5c3656813190ec210d。2594旧无关文件、1751前端、提示词、设置/模型池、Core775073、网关775930与Nginx一致。共享锁及两次真实AI/Core空闲守卫通过，没有停止客户直播。完整前端备份复用并逐项核验，最新指针已更新。
- 修复在程序中保留商品任务完整编号；真实V3离线重放修复后56/56匹配、ready，anchorcycle完整118项和相关HTTPAPI通过。不重写V3或修改客户事实/权重。用户随后自行保存V4/id117，V3被正常superseded，不能说本聊天保存了V4。
- 16:24:06用户另开301场，当前只读采样Core实际program version114，不能把它当V3多商品实际播放证明；不可为后续加固停止或重启该场。
- 用户进一步要求解决根本问题：后续统一任务编号函数，保存验证与正式启动共用prepareSavedAnchorTaskState，从不可变商品快照重建运行索引，并验证单段生成/审核/TTS/下发失败继续供料。此后续代码在work/model-pool-source，尚需空闲后发布；不能把已上线9c71849描述成已包含这套后续加固。证明work/live-stop-check-20261016/production-verification.json、reproduction-before.log、reproduction-after.log、root-full-tests.jsonl、shared-v3-preparation-replay.log。


## 保存与启动任务准备统一（2026-10-16 已上线，覆盖前述加固待办）

- 北京时间16:33:53上线 `/opt/xiaolan/releases/20261016-task-preparation-d278c2e`，部署源码d278c2e5cdaa048b2aa795ae4e2f673784f52752，Management PID950204/SHA256 c1ec6cbf5684d3e7f2fd7d1fa4f81402b6f2020ddd56e8d8c69b78add48aff13。只替换管理程序，2595旧无关文件、1751前端、提示词、模型池/系统设置、Core775073、网关775930与Nginx一致；12公网入口/资源和完整前端备份核验通过。
- Task.schedulingKey统一商品展开与New编号。保存validateRealtimeScheme和启动createLiveAnchorRun共用prepareSavedAnchorTaskState：按不可变商品卖点快照重建运行索引/归属，事实、权重、旧共享池/显式ID及缓存SlotKey保留，每个商品均通过实际构造器和选取器。不能再用尚未经New的Queue检查替代启动检查，也不让模型维护内部ID。
- 同类可恢复的正文/审核、TTS、单段下发失败通过既有cycleSupply跳过续播；已回归核验，并新增下发失败不取消AI的测试。未知运行异常不再统称TTS生成失败。两个完整Go包anchorcycle/httpapi共799项（含子测试）通过，真实V3/id116及用户自行保存的V4/id117均56/56任务通过共享保存/启动离线重放，没有真实模型/TTS/Core调用。
- 301场于16:31:11.963由用户manual_stop，发布前两次AI/Core空闲守卫通过；没有为发布停止用户场次。发布后只读核对plan5最新V4/id117、room26发布117，没有改写方案或自动开播。本统一版的新场实际播放尚待后续开播验证；301的Core version114不算这次多商品验证。
- 当前没有待发布任务。代码已推送GitHub main并同步work/main-merge-20261014，完整开发仍work/model-pool-source；WorkBuddy原E:/speech-generator-build文件35项捕获指纹再次一致。证明work/live-stop-check-20261016/root-deployment-report.json、root-production-verification.json、root-full-tests.jsonl、shared-v3-preparation-replay.log、shared-v4-preparation-replay.log；前端指针work/frontend-preservation/latest.json已指向本版本并复用逐项核验一致的完整备份。后续仍按actual current和实际指纹发布，不能强制历史版本。
