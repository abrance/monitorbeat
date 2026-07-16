# monitorbeat 本地 k3s 运维手册

> 记录 monitorbeat 在本地 k3s 集群上的部署、升级、排障流程。
> 最后更新：2026-07-16

---

## 1. 资源清单

所有资源都在 `monitorbeat` 命名空间下，由 Helm release `mon` 管理（chart: `monitorbeat-0.1.0`）。

| 类型 | 名称 | 说明 |
|------|------|------|
| Helm Release | `mon` | 唯一入口，所有资源由此渲染 |
| DaemonSet | `mon-monitorbeat-beat` | 采集 agent，每节点 1 个 Pod |
| Deployment | `mon-monitorbeat-web` | Web 仪表盘 + VM 查询代理 |
| Service | `mon-monitorbeat-web` | ClusterIP `8080`，仅供 Ingress 用 |
| ConfigMap | `mon-monitorbeat-beat` | agent YAML 配置（挂载为 `/etc/monitorbeat/config.yaml`） |
| ConfigMap | `mon-monitorbeat-web` | web YAML 配置（挂载为 `/etc/monitorweb/config.yaml`） |
| Ingress | `mon-monitorbeat-web` | Traefik + cert-manager，域名 `monitorbeat.xiaoyxq.top` |
| Secret | `monitorbeat-xiaoyxq-top-tls` | cert-manager 自动签发/续期的 TLS 证书（勿手动改） |
| Secret | `sh.helm.release.v1.mon.v*` | Helm release 历史（自动管理） |

**依赖的外部服务**（不在本命名空间）：

- `vmtest-1-victoria-metrics-cluster-vminsert` (bkbase-test ns) — agent 写入指标
- `vmtest-1-victoria-metrics-cluster-vmselect` (bkbase-test ns) — web 查询指标
- cert-manager ClusterIssuer `letsencrypt-prod` — 签发 TLS 证书

---

## 2. 快速命令

```bash
# 查看所有资源
kubectl -n monitorbeat get all

# 查看 Pod 状态与日志
kubectl -n monitorbeat get pods -o wide
kubectl -n monitorbeat logs -f deployment/mon-monitorbeat-web
kubectl -n monitorbeat logs -f daemonset/mon-monitorbeat-beat

# 查看当前配置（渲染后的 YAML）
kubectl -n monitorbeat get configmap mon-monitorbeat-beat -o yaml
kubectl -n monitorbeat get configmap mon-monitorbeat-web -o yaml

# Helm release 状态
helm -n monitorbeat list
helm -n monitorbeat history mon
```

---

## 3. 升级镜像（最常见操作）

镜像 tag 格式为 `sha-<commit短哈希>`。CI 在每次 push 到 `main` 后自动构建并推送到 `ghcr.io/abrance/monitorbeat` 和 `ghcr.io/abrance/monitorweb`。

**步骤：**

```bash
# 1. 确认远程已有新镜像（以 commit sha-abcdef0 为例）
docker pull ghcr.io/abrance/monitorbeat:sha-abcdef0
docker pull ghcr.io/abrance/monitorweb:sha-abcdef0

# 2. 更新部署（用 --set 覆盖 image，优先级最高）
helm upgrade mon ./deploy/helm/monitorbeat -n monitorbeat \
  --set monitorbeat.image=ghcr.io/abrance/monitorbeat:sha-abcdef0 \
  --set monitorweb.image=ghcr.io/abrance/monitorweb:sha-abcdef0

# 3. 等滚动更新完成
kubectl -n monitorbeat rollout status deployment/mon-monitorbeat-web --timeout=120s
kubectl -n monitorbeat rollout status daemonset/mon-monitorbeat-beat --timeout=120s

# 4. 确认新镜像已生效
kubectl -n monitorbeat get pods -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].image}{"\n"}{end}'
```

> ⚠️ **重要**：直接用 `helm upgrade` 不带 `--set` 时，会用 chart 默认 `values.yaml`（目前也是 `sha-336dd00`）覆盖。**不要用 `values.yaml` 管理镜像 tag**，统一用 `values-local.yaml` + `--set` 或 `-f values-local.yaml`，避免回退到旧 tag。

**等价命令（用本地 values 文件）：**

```bash
helm upgrade mon ./deploy/helm/monitorbeat -n monitorbeat \
  -f deploy/helm/monitorbeat/values-local.yaml
```

更新 `values-local.yaml` 里的 `image:` 字段后执行上面命令即可。

---

## 4. 修改配置

配置分两部分：

### 4.1 agent 配置（monitorbeat）

改 `deploy/helm/monitorbeat/values-local.yaml` 的 `monitorbeat.config` 段（或 `values.yaml`），然后：

```bash
helm upgrade mon ./deploy/helm/monitorbeat -n monitorbeat -f deploy/helm/monitorbeat/values-local.yaml
```

ConfigMap 变更会触发 `checksum/config` annotation 变化 → DaemonSet 自动滚动重建。

### 4.2 web 配置（monitorweb）

同上，改 `monitorweb.config` 段。web 的 `db_path: /data/alerts.db` 挂在 `emptyDir`，Pod 重建后告警历史会丢失 —— 如需持久化需改为 PVC（见第 6 节）。

---

## 5. 回滚

```bash
# 查看历史 revision
helm -n monitorbeat history mon

# 回滚到上一版（或指定 REVISION 数字）
helm -n monitorbeat rollback mon
helm -n monitorbeat rollback mon 12
```

---

## 6. 已知限制 / TODO

| 项 | 说明 | 建议 |
|----|------|------|
| 告警历史无持久化 | `/data` 是 `emptyDir`，Pod 重启告警规则保留（SQLite 在 release 外），但 history 会丢 | 改为 PVC 挂载 `/data` |
| 镜像 tag 靠手动 `--set` | 容易忘，回退到旧值 | 固化到 `values-local.yaml` 并用 `-f` 部署 |
| DaemonSet 仅 1 节点 | 当前 k3s 单节点，多节点会自动每节点 1 Pod | 无需操作 |
| agent 配置无热重载 | 改配置需重建 Pod | 已实现 SIGUSR1 reload，但 Helm 滚动更新更简单 |

---

## 7. 排障速查

| 现象 | 可能原因 | 排查 |
|------|---------|------|
| 页面白屏 / 加载中… | web 镜像旧（已修）或 VM 不可达 | `kubectl -n monitorbeat logs deployment/mon-monitorbeat-web` |
| 无主机数据 | agent 未运行 / VM insert 地址错 | `kubectl -n monitorbeat logs daemonset/mon-monitorbeat-beat` |
| Disk 指标为空 | 指标名错（已修为 `disk_root_used_percent`） | 查 VM `disk_root_used_percent` 是否存在 |
| TLS 证书过期 | cert-manager 未续期 | `kubectl -n monitorbeat describe certificate monitorbeat-xiaoyxq-top-tls` |
| Ingress 404 | 后端 Service 端口不对 | `kubectl -n monitorbeat get ingress mon-monitorbeat-web -o yaml` |

---

## 8. 本地文件索引

| 文件 | 用途 |
|------|------|
| `deploy/helm/monitorbeat/values.yaml` | chart 默认模板（参考用，不用于生产部署） |
| `deploy/helm/monitorbeat/values-local.yaml` | **本地 k3s 实际部署配置**（镜像 tag + 所有参数） |
| `deploy/helm/monitorbeat/templates/` | 所有 K8s 资源模板 |
| `docs/monitorbeat-k3s-运维手册.md` | 本文档 |
