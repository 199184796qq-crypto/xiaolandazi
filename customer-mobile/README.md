# customer-mobile

终端用户轻量手机端，基于 SvelteKit + TypeScript。

## 边界

保留：
- 首页状态
- 会员 / 时长卡 / 设备商城
- 订单与售后
- 邀请推荐
- 设备基础状态
- 小蓝直播搭子

不包含：
- Chrome / 浏览器直播采集
- 直播公屏、弹幕流
- 主播画面预览
- 复杂直播策略编辑
- 运行日志和设备调试

智能体调用 `/api/v1/client-agent/*`，只允许终端用户安全域。

## 本地启动

```bash
pnpm dev
```

推荐地址：`http://customer.localhost:5174/`

访问 `http://127.0.0.1:5174/` 或 `http://localhost:5174/` 时会自动跳转到独立的 `customer.localhost` 主机名，避免与管理后台共享同名登录 Cookie。

开发环境将 `/api` 代理到 `http://127.0.0.1:8080`。
