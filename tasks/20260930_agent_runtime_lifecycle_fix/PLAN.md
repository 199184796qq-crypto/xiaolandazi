# Agent Runtime 生命周期修复计划

## Core 修改

检查：

- Agent 启动入口
- Room 恢复流程
- SpeechTask 调度
- Mainline 调度
- Audio Engine

## 统一入口

增加 EnsureAgentRuntime(roomID)

负责：

- 创建 Agent Runtime
- 创建 Speech Runtime
- 初始化主线队列
- 启动 Audio Scheduler

## 恢复流程

旧：

restore room -> collector

新：

restore room -> collector -> EnsureAgentRuntime

## 保护

增加 runtime lock，避免重复启动。
