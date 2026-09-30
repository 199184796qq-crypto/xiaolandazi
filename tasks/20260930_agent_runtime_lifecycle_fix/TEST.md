# 验证计划

## 本地验证

- 第一段播放
- 第二段播放
- 第三段播放
- 连续循环

## ECS 验证

systemctl restart xiaolan-core

验证：

- 不重新点击开始
- Agent 自动恢复
- speech runtime 存在
- audio queue 正常

业务验证：

第一段
↓
第二段
↓
第三段
↓
持续运行
