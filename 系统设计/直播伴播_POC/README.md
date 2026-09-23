# 直播伴播 POC

这个目录用于验证《直播间伴播_V1架构与实施规划.md》中的关键机制，先验证逻辑，再接真实百炼、真实 TTS、真实直播平台和真实盒子。

## 当前 POC 已验证

1. 普通进房、低量点赞走规则快路径，不调用 Agent。
2. 高频价格问题走规则 + 模板，不调用 Monitor Agent / Control Agent。
3. 复杂用户问题才调用 Monitor Agent 和 Control Agent。
4. 主线按完整短句/Segment 播放。
5. 用户问题到达时使用 SOFT_INTERRUPT，当前短句播完后再切互动。
6. 互动完成后从下一完整句恢复，不进行半句续播。
7. 较长互动使用 0 Token 桥接语后恢复主线。
8. 逼单由状态触发，再调用 Monitor Agent 判断是否需要。
9. 商品切换后，旧商品待播任务自动失效并丢弃。
10. 重复事件去重。
11. Agent Token、模板命中、打断、恢复、失效任务均有指标。

## 运行

~~~powershell
python "E:\直播伴播\系统设计\直播伴播_POC\poc_simulation.py"
~~~

正常结束应看到：

~~~text
POC_ASSERTIONS=PASS
~~~

## POC 中的 Agent

当前使用 FakeMonitorAgent / FakeControlAgent，目的是先验证系统编排逻辑。

后续接百炼时，只替换：

~~~text
FakeMonitorAgent
→ BailianMonitorAgentAdapter

FakeControlAgent
→ BailianControlAgentAdapter
~~~

Event Engine、Room State、Action、SpeechTask、Audio Focus、打断恢复逻辑不需要跟着模型供应商重写。

## 下一步实践顺序

1. 把当前单文件拆成正式模块。
2. 补 Action / SpeechTask JSON Schema。
3. 增加单元测试与回归场景。
4. 增加 PC 模拟盒子。
5. 接一个真实 TTS。
6. 接百炼 Monitor Agent。
7. 接百炼 Control Agent。
8. 再接真实直播事件。
