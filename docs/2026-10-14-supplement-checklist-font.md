# 补全清单文字增大一号

用户要求补全清单内文字和二级文字都大一号。仅调整 ProductSupplementChat 的清单局部样式：卡片标题由16px增至17px，说明14px增至15px，问题提示、问这个/跳过按钮及底部补充入口由13px增至14px，状态标签由12px增至13px。保持标题、说明、问题之间的层级。

右侧对话、换下一批清单按钮、读取与确认保存流程保持原样。沿用确定的组件宽度和容器响应布局。

## 验证

- 既有清单与对话11项回归通过；vue-tsc及生产构建通过。
- 使用完整方案页父级与全局样式，合成数据验证1366px桌面与390px手机视口。两种宽度的实际字体均与目标一致，页面和卡片没有水平溢出；问这个继续在右侧对话，未提交输入保留。
- 前端发布以服务器实际 current 为基础，仅更新两个入口并添加新哈希资源，保留后端、已上线页面与Nginx路由。北京时间10:34已上线 `/opt/xiaolan/releases/20261014-supplement-list-font-e051f4c`，源码e051f4c。1620个原有非入口文件一致，三个服务PID和运行二进制、手机端及Nginx不变；7项HTTP资源核对通过。

验证记录位于本聊天 `work/supplement-list-font-proof/production-verification.json`；前端备份指针 `work/frontend-preservation/latest.json` 已更新，1280个前端文件逐项匹配服务器。
