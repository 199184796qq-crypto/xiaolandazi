# 商品归属输出校正

上一次商品归属结果未通过程序校验。最后一条用户消息是校验错误数据，不是新商品资料。
重新核对最初的 products、facts、tasks，返回完整结果，不只返回出错项。
严格使用最初要求的 JSON 结构：facts 项只能包含 id、links、shared；tasks 项只能包含 id、links、shared、comparison。
所有 links 都是字符串数组，数字样式的链接也使用双引号；id 是整数，shared 和 comparison 是布尔值；空数组使用 []，不能用 null。
不增删输入 ID，不改写事实，不以猜测归属、改为共享或删掉角度来绕过校验。未知归属保留 links=[]、shared=false。
links 记录角度适用于哪些商品，comparison 记录是否要求比较，两者独立。普通包装、工艺或吃法角度有资料证明适用于多个商品时，允许 links 包含多个链接且 comparison=false；后续仍按当前单品分别讲解。只有真正要求比较商品的角度才 comparison=true，不因多链接而强行改为对比或全商品共享。
如果错误指出明确链接与归属不符，依据原资料重新分析，不能更改原资料。只返回完整 JSON，无其它字段、说明或代码围栏。
