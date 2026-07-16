## 修复计划：monitorweb 遗留项

基于 `docs/前端架构负债.md` + `docs/缺陷-告警页空白与加载中.md` + 实际代码审计，分 3 批修复：

---

### Batch 1 — 🔴 生产级 Bug（已导致线上白屏/卡死）

| # | 问题 | 文件 | 修复方式 |
|---|------|------|---------|
| 1 | `ListRules` 返回 `nil` → JSON `null` → 前端 `!null === true` → 永远"加载中…" | `web/alerts/store.go:174` | `var out []AlertRule` → `out := []AlertRule{}` |
| 2 | `ListHistory` 返回 `nil` → JSON `{"items": null}` → 前端 `null.map()` → 白屏崩溃 | `web/alerts/store.go:427` | `var out []HistoryItem` → `out := []HistoryItem{}` |
| 3 | `GetEnabledRules` 同模式，Evaluator 消费但无 crash 风险 | `web/alerts/store.go:205` | 同上初始化 |
| 4 | `getStatesForRule` 返回 `nil` → `"states": null` → `AlertRule.States` 字段序列化 null | `web/alerts/store.go:239` | `var out []AlertState` → `out := []AlertState{}` |
| 5 | `alertStatusResponse.Items` 无 firing 时为 nil → `"items": null` | `web/api/alerts.go:150` | `resp.Items = []alertStatusItem{}` 或在 struct 定义加 `omitempty` |
| 6 | `disk___used_percent` 指标名错误（agent 实际发 `disk_root_used_percent`）→ Summary/Chart 永远无数据 | `web/api/server.go:219` + `web/ui/src/pages/HostDetail.tsx:7` | 两处 `disk___used_percent` → `disk_root_used_percent` |

### Batch 2 — 🟡 功能修复

| # | 问题 | 文件 | 修复方式 |
|---|------|------|---------|
| 7 | 前端无 Error Boundary，任何渲染崩溃 → 白屏 | `web/ui/src/main.tsx` | 加 `<ErrorBoundary>` 组件包裹 `<Routes>`，fallback 显示错误信息+重试 |
| 8 | `useAsync` 无 `refetch` 函数，Alerts 页用 `window.location.reload()` 硬刷新 | `web/ui/src/api/client.ts` + `Alerts.tsx` + `AlertHistory.tsx` | `useAsync` 返回 `refetch` 函数（重新执行 fn 并更新 state），替换所有 `window.location.reload()` |
| 9 | Probes 页无自动刷新，数据显示静态快照 | `web/ui/src/pages/Probes.tsx` | `useAsync` 加 `refetchInterval` 参数，Probes 设 30s 轮询 |
| 10 | AlertHistory 页同时渲染 error+loading，空数据时显示空表格 | `web/ui/src/pages/AlertHistory.tsx` | 修复条件渲染顺序：先 error → 再 loading → 再 data；空数据时显示 empty state |
| 11 | Alerts 页 `!` 非空断言危险 (`.find(...)!`) | `web/ui/src/pages/Alerts.tsx:133` | 安全守卫 + `?.` 可选链 |

### Batch 3 — 🟢 Quick Wins

| # | 问题 | 文件 | 修复方式 |
|---|------|------|---------|
| 12 | favicon.ico 404 | `web/ui/public/` | 添加 favicon.svg |
| 13 | tsconfig `noUnusedLocals: false` | `web/ui/tsconfig.json` | 改为 `true`，清理 Alerts.tsx 中 `rules` 死引用 |

---

### 影响范围

- **后端 Go**: 3 个文件 (`store.go`, `alerts.go`, `server.go`)
- **前端 TS/TSX**: 6 个文件 (`client.ts`, `main.tsx`, `Alerts.tsx`, `AlertHistory.tsx`, `Probes.tsx`, `HostDetail.tsx`)
- **配置**: 1 个文件 (`tsconfig.json`)
- **新增文件**: `ErrorBoundary.tsx`, `favicon.svg`

### 不在此计划中的项（超出"修复遗留项"范围）

- 中期方案如 zod 运行时校验、react-query 迁移 — 需要更大重构
- 未实现的 13 个 task — 后端任务，非 monitorweb 前端
- CORS 配置、统一错误协议 — 非当前急需
