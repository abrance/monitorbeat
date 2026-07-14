# monitorbeat

> 干净的内核采集器，从蓝鲸 `bkmonitorbeat` 剥离 GSE / CMDB / 多租户 等 BK 特有逻辑而来。

---

## 路线图

按三阶段交付，每阶段独立可演示：

| 阶段 | 目标 | 文档 |
|---|---|---|
| **P0 地基** | 仓库骨架 + 调度器 + 配置 + 1 个 output，1 个干净 task（basereport）跑通 | [三阶段实现计划](./docs/monitorbeat-三阶段实现计划.md) |
| **P1 三大场景** | 拨测 / 日志采集 / 脚本采集 三类核心能力全跑通 | 同上 |
| **P2 收尾** | 27 个 task 全部就位 + 零 libgse 依赖 + 完整文档/Dockerfile/CI | 同上 |

## 能力概览

详见 [产品能力清单](./docs/monitorbeat-能力清单.md)：拨测、日志采集、脚本采集、主机指标、进程指标、系统事件、容器、可观测自身 8 大类共 27 个 task。

---

## 状态

🚧 **P0 准备阶段** — 仓库初始化完成，骨架占位文件已提交。代码实现待启动。

## 构建（占位）

```bash
# P0 启动后填充
make build
make test
make docker
```

## License

TBD