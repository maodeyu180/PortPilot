# 版本与镜像发布

源码仓库：[maodeyu180/PortPilot](https://github.com/maodeyu180/PortPilot)。

使用 GitHub Actions 构建镜像并发布到 GitHub Container Registry（GHCR），配置 Docker Hub 凭据后会同时发布到 Docker Hub。GHCR 使用 GitHub 自动提供的 `GITHUB_TOKEN`，发布任务仅申请 `contents: read` 和 `packages: write`；只使用 GHCR 时无需额外配置凭据。

## 触发方式

| 操作 | 结果 |
| --- | --- |
| 推送 `main` / 提交 PR | 构建界面、Go race 测试、Docker 启动与代理认证集成检查 |
| 推送 `v0.1.0` 等版本标签 | 先运行相同检查，通过后发布 amd64 / arm64 镜像 |
| 推送 `v0.2.0-rc.1` 等预发布标签 | 发布预发布镜像，不更新 `latest` |

普通代码推送不会直接更换线上程序或推送正式镜像。版本号由维护者选择，Actions 根据标签构建和推送。

## 发布新版本

1. 在 GitHub 仓库的 Actions 页面确认工作流可运行。若账号策略限制了 Actions 或写入 Packages，需要允许对应权限。
2. 将最终代码推到 `main`，等待 CI 成功。
3. 给要发布的提交创建标签并推送：

   ```sh
   git tag -a v0.1.1 -m "PortPilot v0.1.1"
   git push origin v0.1.1
   ```

4. 在 Actions 的 `Publish container image` 查看结果。成功摘要包含镜像地址、标签和 digest。
5. 首次生成的 GHCR 包默认是私有的。在 GitHub 个人主页的 Packages 中打开 `portpilot`，进入 Package settings，将可见性设为 **Public**，让开源用户免登录拉取。源码仓库公开不会自动将镜像包公开。

稳定版本 `v0.1.1` 会生成：

```text
ghcr.io/maodeyu180/portpilot:v0.1.1
ghcr.io/maodeyu180/portpilot:0.1.1
ghcr.io/maodeyu180/portpilot:0.1
ghcr.io/maodeyu180/portpilot:latest
```

两个架构共用同一镜像名和标签，Docker 按宿主机架构选择。首次发布成功并公开包之前，这些地址尚不可供公开拉取。

## 使用已发布镜像

进入源码目录，运行 `sh scripts/setup.sh` 生成 `.env`。`v0.1.1` 起管理密码可留空，在首次访问时直接设置并确认；也可提前填写 `PORTPILOT_ADMIN_PASSWORD`。已有管理密码会从数据卷保留。将镜像来源和版本设为：

```text
PORTPILOT_IMAGE_REPOSITORY=ghcr.io/maodeyu180/portpilot
PORTPILOT_IMAGE_TAG=0.1.1
```

然后执行：

```sh
docker compose pull init portpilot mihomo
docker compose up -d --no-build --wait
```

面板与初始化服务使用同一镜像，Mihomo 仍使用其官方镜像。不需要在服务器编译 Node 或 Go。

使用旧版 `v0.1.0` 镜像时，必须提前填写至少 12 个字符的 `PORTPILOT_ADMIN_PASSWORD`；该版本不支持网页首次设密。

升级时先备份数据，修改版本号，再只更新面板：

```sh
docker compose pull portpilot init
docker compose up -d --no-deps --no-build portpilot
```

这不会重建 Mihomo。初始化服务在下一次完整启动时使用新版本，但保留已有数据和已应用配置。

## Docker Hub 可选

在仓库 **Settings → Secrets and variables → Actions** 配置以下两项：

| 标签页 | 新增入口 | Name | Value / Secret |
| --- | --- | --- | --- |
| Secrets | New repository secret | `DOCKERHUB_TOKEN` | Docker Hub Access Token，需有镜像写权限 |
| Variables | New repository variable | `DOCKERHUB_USERNAME` | Docker Hub 登录用户名（Docker ID，不是邮箱，也不一定与 GitHub 用户名相同） |

直接入口：[填写 Token](https://github.com/maodeyu180/PortPilot/settings/secrets/actions)、[填写用户名](https://github.com/maodeyu180/PortPilot/settings/variables/actions)。只需这一枚 Token 和用户名，不需要保存 Docker Hub 账号密码。不要将 Token 写入源码、`.env.example` 或聊天记录。

在 Docker Hub 用户名对应的空间准备公开的 `portpilot` 镜像仓库。两项配置齐全后，下一个版本标签会同时发布到 GHCR 和 `<Docker ID>/portpilot`，使用相同版本标签并支持 amd64 / arm64。没有配置这两项时仅发布 GHCR；只配置其中一项会报错，避免误以为 Docker Hub 已完成发布。保存配置本身不会触发发布。

使用 Docker Hub 镜像时，将 `.env` 中的 `PORTPILOT_IMAGE_REPOSITORY` 设为 `<Docker ID>/portpilot`，`PORTPILOT_IMAGE_TAG` 仍填写所需版本号。

参考：[GitHub 镜像发布](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 认证与可见性](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Docker 多仓库发布](https://docs.docker.com/build/ci/github-actions/push-multi-registries/)。
