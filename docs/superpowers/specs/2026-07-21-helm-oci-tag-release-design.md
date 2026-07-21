# Helm OCI Chart Tag 发布设计

## 背景

当前 GitHub Actions 会对 Helm Chart 执行 `helm lint` 和 `helm template`，并在非 Pull Request 事件中构建、推送 Docker 镜像，但不会打包或发布 Helm Chart。

## 目标

当仓库推送符合 `v*` 的 Git tag 时，在现有测试通过后构建 Helm Chart，并将其作为 OCI Chart 推送至 GitHub Container Registry（GHCR）。普通分支推送和 Pull Request 不发布 Chart。

## 发布流程

在 `.github/workflows/ci.yaml` 中新增独立的 `helm` Job：

1. 使用以下精确条件，仅处理 push 事件中的 `v*` tag：

   ```yaml
   if: >-
     github.event_name == 'push' &&
     github.ref_type == 'tag' &&
     startsWith(github.ref_name, 'v')
   ```

2. 依赖现有 `build-test` Job，确保构建、测试以及 Helm lint/render 均通过。
3. 授予 `contents: read` 与 `packages: write` 权限。
4. Checkout 仓库。现有 `build-test` 和新增发布 Job 都通过 `azure/setup-helm@v4` 安装固定版本 `v3.18.4`，避免 latest 漂移。
5. 从 tag 中移除开头的 `v`，校验后得到发布版本。该步骤必须发生在 registry 登录之前，确保无效版本不会执行登录或推送。
6. 将 GitHub repository owner 转为小写，作为 GHCR namespace。
7. 使用 `helm registry login` 和 Actions 提供的 `GITHUB_TOKEN` 登录 `ghcr.io`。
8. 使用发布版本覆盖 Chart 的 `version` 和 `appVersion`，打包到 `dist/`。
9. 校验包文件、包内版本和模板渲染结果。
10. 将包推送至 `oci://ghcr.io/<repository-owner>/charts`，再从 OCI 地址读取 Chart 元数据确认发布成功。

Helm 会根据包内的 Chart 名称和版本补全仓库路径。例如，tag `v1.2.3` 将发布：

```text
ghcr.io/<lowercase-repository-owner>/charts/monitorbeat:1.2.3
```

`helm push` 的目标必须是父路径，不能在命令中重复追加 Chart 名和版本：

```bash
helm push "$PACKAGE" "oci://ghcr.io/${OWNER}/charts"
```

## Workflow 步骤

### 固定 Helm 版本

两个使用 Helm 的 Job 均采用：

```yaml
- name: Set up Helm
  uses: azure/setup-helm@v4
  with:
    version: v3.18.4
```

### 提取并校验版本

版本步骤使用 `bash`，通过 `$GITHUB_OUTPUT` 将 `version` 和小写 `owner` 传给后续步骤：

```yaml
- name: Resolve release metadata
  id: release
  shell: bash
  env:
    OWNER: ${{ github.repository_owner }}
  run: |
    set -euo pipefail

    VERSION="${GITHUB_REF_NAME#v}"
    if [[ -z "$VERSION" ]]; then
      printf 'Invalid release tag: %s\n' "$GITHUB_REF_NAME" >&2
      exit 1
    fi
    if [[ "$VERSION" == *+* ]]; then
      printf 'Build metadata is unsupported: %s\n' "$VERSION" >&2
      exit 1
    fi
    if (( ${#VERSION} > 63 )); then
      printf 'Release version exceeds 63 characters: %s\n' "$VERSION" >&2
      exit 1
    fi

    printf 'version=%s\n' "$VERSION" >> "$GITHUB_OUTPUT"
    printf 'owner=%s\n' "${OWNER,,}" >> "$GITHUB_OUTPUT"
```

其他非法语义化版本由后续 `helm package --version` 拒绝。

### 登录 GHCR

```yaml
- name: Log in to GHCR
  env:
    GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}
  run: |
    set -euo pipefail
    printf '%s' "$GHCR_TOKEN" | helm registry login ghcr.io \
      --username "${{ github.actor }}" \
      --password-stdin
```

### 打包并校验发布物

```yaml
- name: Package and verify Helm chart
  shell: bash
  env:
    VERSION: ${{ steps.release.outputs.version }}
  run: |
    set -euo pipefail

    mkdir -p dist
    helm package ./deploy/helm/monitorbeat \
      --destination dist \
      --version "$VERSION" \
      --app-version "$VERSION"

    PACKAGE="dist/monitorbeat-${VERSION}.tgz"
    test -f "$PACKAGE"

    ACTUAL_VERSION="$(helm show chart "$PACKAGE" | awk '$1 == "version:" { print $2 }')"
    ACTUAL_APP_VERSION="$(helm show chart "$PACKAGE" | awk '$1 == "appVersion:" { print $2 }' | tr -d '"')"
    test "$ACTUAL_VERSION" = "$VERSION"
    test "$ACTUAL_APP_VERSION" = "$VERSION"

    helm lint "$PACKAGE"
    helm template monitorbeat "$PACKAGE" > /dev/null
```

### 推送并验证远端 Chart

```yaml
- name: Push and verify Helm chart
  shell: bash
  env:
    VERSION: ${{ steps.release.outputs.version }}
    OWNER: ${{ steps.release.outputs.owner }}
  run: |
    set -euo pipefail

    PACKAGE="dist/monitorbeat-${VERSION}.tgz"
    CHART="oci://ghcr.io/${OWNER}/charts/monitorbeat"
    helm push "$PACKAGE" "oci://ghcr.io/${OWNER}/charts"

    REMOTE_CHART="$(helm show chart "$CHART" --version "$VERSION")"
    ACTUAL_VERSION="$(printf '%s\n' "$REMOTE_CHART" | awk '$1 == "version:" { print $2 }')"
    ACTUAL_APP_VERSION="$(printf '%s\n' "$REMOTE_CHART" | awk '$1 == "appVersion:" { print $2 }' | tr -d '"')"
    test "$ACTUAL_VERSION" = "$VERSION"
    test "$ACTUAL_APP_VERSION" = "$VERSION"
```

## 版本规则

Git tag 去除首个 `v` 后必须满足以下规则：

- 不能为空，因而裸 tag `v` 会被拒绝。
- 必须是 Helm 接受的语义化版本。
- 不允许包含构建元数据 `+...`。Helm 会将 OCI tag 中的 `+` 转成 `_`，而且 `+` 不能直接用于 Chart 当前的 Kubernetes 版本标签；拒绝它可以保证 tag 与 OCI 版本一致。
- 长度不能超过 63 个字符，以满足当前模板中 `app.kubernetes.io/version` 的 Kubernetes 标签值限制。

版本通过 `helm package --version --app-version` 注入，不修改仓库中的 `Chart.yaml`。

## 权限与错误处理

Job 权限为：

```yaml
permissions:
  contents: read
  packages: write
```

版本校验、登录、打包、包验证、推送或远端验证任一步骤失败都会使 Job 失败。由于 Job 依赖 `build-test`，校验或测试失败时不会尝试发布。

若 GHCR 中已经存在一个未关联当前仓库的同名 package，即使设置了 `packages: write` 也可能返回 `403`；此时需要在 GHCR package 设置中授予当前仓库 Actions 写权限。

## 验证计划

实现后进行以下验证：

### 静态和本地验证

- 对 workflow 运行 `actionlint`，检查 GitHub Actions 语法与表达式。
- 使用正常测试版本执行与 CI 相同的 `helm package`，断言文件名、`version`、`appVersion`、`helm lint` 和 `helm template` 均正确。
- 对版本提取逻辑验证以下负面用例：裸 `v`、非法语义化版本、包含 `+` 的版本、超过 63 字符的版本。它们必须在登录或推送前失败。

### GitHub Actions 事件验证

- Pull Request：`helm` Job 不运行。
- 普通分支 push：`helm` Job 不运行。
- 合法 tag（如 `v1.2.3`）：发布精确引用 `ghcr.io/<lowercase-owner>/charts/monitorbeat:1.2.3`。
- 无效 `v*` tag：Job 失败，且日志显示没有执行登录和推送步骤。
- 合法 tag 推送后，认证状态下运行 `helm show chart oci://ghcr.io/<owner>/charts/monitorbeat --version <version>`，返回的 `version` 与 `appVersion` 都等于 tag 去除 `v` 后的版本。

实际 GHCR 推送和远端读取需要由 GitHub Actions 的 tag 事件验证；本地验证不执行对外发布。
