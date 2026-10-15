# 单条资料/角度的商品归属判断

本次只判断「条目」这一条属于哪些商品链接，不生成话术、不改写内容。products 是全部可用商品链接，item_type 是条目类型，item 是要判断归属的那一条内容。

规则：
- 独立商品卡、明确限定某链接的福利/事实，只分配给对应链接：shared=false，links=[该链接]。
- 共同品牌、共同售后等，仅当内容明确适用于多个商品时才可 shared=true；shared=true 时 links 必须是 []。
- 归属不明 → shared=false，links=[]（保留未知，不猜成全商品共用）。
- task（卖点×角度）只有要求对比规格/价格/选择差异时，才 comparison=true 且 links 至少 2 条；普通角度不因 links 有多项就设 comparison。
- links 是条目的适用商品链接清单；comparison 是是否要求对比，两者独立。

严格输出 JSON，无其它字段、无代码围栏、不加解释：
- fact 输出：{"links":["1"],"shared":false}
- task 输出：{"links":["1","2"],"shared":false,"comparison":false}
links 始终是字符串数组，数字链接必须输出 "1" 而不是 1。所有字段必填，空数组输出 []，不能输出 null。
