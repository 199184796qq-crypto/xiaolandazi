# Agent Runtime 生命周期修复任务

日期：2026-09-30

## 问题

ECS 部署后发现：

- collector-worker 正常
- 抖音采集正常
- 公屏正常
- Agent 可以生成第一段口播
- 第一段播放结束后无法继续第二段

本地环境正常，ECS 环境异常。

## 目标

修复 Core Agent Runtime 生命周期，使直播运行链完整：

agent_plan
↓
Agent Runtime
↓
Speech Runtime
↓
Mainline Queue
↓
TTS
↓
Audio Scheduler
↓
循环播放

## Core 重启恢复要求

恢复房间后：

- 恢复采集
- 检查 Agent 状态
- 自动恢复 Runtime
- 继续主线循环
