# 版本与镜像发布

源码仓库：[maodeyu180/PortPilot](https://github.com/maodeyu180/PortPilot)。

已发布的镜像同时提供 Docker Hub 与 GHCR 两个来源，支持 amd64 / arm64。当前安装版本为 `0.1.2`：

```text
maodeyu/portpilot:0.1.2
ghcr.io/maodeyu180/portpilot:0.1.2
```

## 使用已发布镜像（免 clone）

下载独立 Compose 文件即可使用，默认拉取 Docker Hub 镜像：

```sh
mkdir portpilot
cd portpilot
curl -fsSL https://raw.githubusercontent.com/maodeyu180/PortPilot/main/compose.release.yaml -o compose.yaml
docker compose pull
docker compose up -d --wait
```

访问 `http://服务器IP:8787`，首次打开时直接设置并确认管理密码。面板、一次性初始化服务与 Mihomo 已由 Compose 配好，不需要源码或构建工具。安装、备份、恢复和迁移步骤见 [部署说明](deployment.md)。

使用 GHCR 时，在 `compose.yaml` 旁创建 `.env`，填写：

```text
PORTPILOT_IMAGE_REPOSITORY=ghcr.io/maodeyu180/portpilot
```

要提前指定管理密码，也可在 `.env` 中填写至少 12 个字符的 `PORTPILOT_ADMIN_PASSWORD`。已有管理密码会从数据卷保留。

使用旧版 `v0.1.0` 镜像时，必须提前填写至少 12 个字符的 `PORTPILOT_ADMIN_PASSWORD`；该版本不支持网页首次设密。

升级时先备份数据，在 `.env` 中设置目标 `PORTPILOT_IMAGE_TAG`，再只更新面板：

```sh
docker compose pull portpilot init
docker compose up -d --no-deps --no-build --wait portpilot
```

这不会重建 Mihomo。初始化服务在下一次完整启动时使用新版本，但保留已有数据和已应用配置。

## 触发方式

| 操作 | 结果 |
| --- | --- |
| 推送 `main` / 提交 PR | 构建界面、Go race 测试、Docker 启动与代理认证集成检查 |
| 推送 `v0.1.0` 等版本标签 | 先运行相同检查，通过后发布 amd64 / arm64 镜像 |
| 推送 `v0.2.0-rc.1` 等预发布标签 | 发布预发布镜像，不更新 `latest` |

普通代码推送不会直接更换线上程序或推送正式镜像。版本号由维护者选择，Actions 根据标签构建和推送。

## 发布新版本

使用 GitHub Actions 构建镜像并发布到 GHCR，配置 Docker Hub 凭据后同时发布到 Docker Hub。GHCR 使用 GitHub 自动提供的 `GITHUB_TOKEN`，发布任务仅申请 `contents: read` 和 `packages: write`；只使用 GHCR 时无需额外配置凭据。

更新版本号时，同步维护 `compose.release.yaml` 中的默认镜像标签与 README 中的安装版本。

1. 在 GitHub 仓库的 Actions 页面确认工作流可运行。若账号策略限制了 Actions 或写入 Packages，需要允许对应权限。
2. 将最终代码推到 `main`，等待 CI 成功。
3. 给要发布的提交创建标签并推送：

   ```sh
   git tag -a v0.1.2 -m "PortPilot v0.1.2"
   git push origin v0.1.2
   ```

4. 在 Actions 的 `Publish container image` 查看结果。成功摘要包含镜像地址、标签和 digest。
5. 首次生成的 GHCR 包默认是私有的。在 GitHub 个人主页的 Packages 中打开 `portpilot`，进入 Package settings，将可见性设为 **Public**，让开源用户免登录拉取。源码仓库公开不会自动将镜像包公开。

稳定版本 `v0.1.2` 会生成：

```text
ghcr.io/maodeyu180/portpilot:v0.1.2
ghcr.io/maodeyu180/portpilot:0.1.2
ghcr.io/maodeyu180/portpilot:0.1
ghcr.io/maodeyu180/portpilot:latest
```

两个架构共用同一镜像名和标签，Docker 按宿主机架构选择。新镜像仓库首次发布后，需要确认包已公开再推荐用户安装。

## Docker Hub 发布配置（维护者）

在仓库 **Settings → Secrets and variables → Actions** 配置以下两项：

| 标签页 | 新增入口 | Name | Value / Secret |
| --- | --- | --- | --- |
| Secrets | New repository secret | `DOCKERHUB_TOKEN` | Docker Hub Access Token，需有镜像写权限 |
| Variables | New repository variable | `DOCKERHUB_USERNAME` | Docker Hub 登录用户名（Docker ID，不是邮箱，也不一定与 GitHub 用户名相同） |

直接入口：[填写 Token](https://github.com/maodeyu180/PortPilot/settings/secrets/actions)、[填写用户名](https://github.com/maodeyu180/PortPilot/settings/variables/actions)。只需这一枚 Token 和用户名，不需要保存 Docker Hub 账号密码。不要将 Token 写入源码、`.env.example` 或聊天记录。

在 Docker Hub 用户名对应的空间准备公开的 `portpilot` 镜像仓库。两项配置齐全后，下一个版本标签会同时发布到 GHCR 和 `<Docker ID>/portpilot`，使用相同版本标签并支持 amd64 / arm64。没有配置这两项时仅发布 GHCR；只配置其中一项会报错，避免误以为 Docker Hub 已完成发布。保存配置本身不会触发发布。

镜像版 Compose 默认使用本项目的 `maodeyu/portpilot`。Fork 项目发布到自己的 Docker Hub 空间时，再将 `PORTPILOT_IMAGE_REPOSITORY` 设为 `<Docker ID>/portpilot`，`PORTPILOT_IMAGE_TAG` 填写所需版本号。

参考：[GitHub 镜像发布](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 认证与可见性](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)、[Docker 多仓库发布](https://docs.docker.com/build/ci/github-actions/push-multi-registries/)。
