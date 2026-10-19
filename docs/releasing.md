# 版本与镜像发布

源码仓库：[maodeyu180/PortPilot](https://github.com/maodeyu180/PortPilot)。

使用 GitHub Actions 构建镜像并发布到 GitHub Container Registry（GHCR）。不需要 Docker Hub 账号，也不需要在仓库中保存个人访问 Token；工作流使用 GitHub 自动提供的 `GITHUB_TOKEN`，发布任务仅申请 `contents: read` 和 `packages: write`。

## 触发方式

| 操作 | 结果 |
| --- | --- |
| 推送 `main` / 提交 PR | 构建界面、Go race 测试、Docker 启动与代理认证集成检查 |
| 推送 `v0.1.0` 等版本标签 | 先运行相同检查，通过后发布 amd64 / arm64 镜像 |
| 推送 `v0.2.0-rc.1` 等预发布标签 | 发布预发布镜像，不更新 `latest` |

普通代码推送不会直接更换线上程序或推送正式镜像。版本号由维护者选择，Actions 根据标签构建和推送。

## 第一次发布

1. 在 GitHub 仓库的 Actions 页面确认工作流可运行。若账号策略限制了 Actions 或写入 Packages，需要允许对应权限。
2. 将最终代码推到 `main`，等待 CI 成功。
3. 给要发布的提交创建标签并推送：

   ```sh
   git tag -a v0.1.0 -m "PortPilot v0.1.0"
   git push origin v0.1.0
   ```

4. 在 Actions 的 `Publish container image` 查看结果。成功摘要包含镜像地址、标签和 digest。
5. 首次生成的 GHCR 包默认是私有的。在 GitHub 个人主页的 Packages 中打开 `portpilot`，进入 Package settings，将可见性设为 **Public**，让开源用户免登录拉取。源码仓库公开不会自动将镜像包公开。

稳定版本 `v0.1.0` 会生成：

```text
ghcr.io/maodeyu180/portpilot:v0.1.0
ghcr.io/maodeyu180/portpilot:0.1.0
ghcr.io/maodeyu180/portpilot:0.1
ghcr.io/maodeyu180/portpilot:latest
```

两个架构共用同一镜像名和标签，Docker 按宿主机架构选择。首次发布成功并公开包之前，这些地址尚不可供公开拉取。

## 使用已发布镜像

进入源码目录，运行 `sh scripts/setup.sh` 生成 `.env`。保留随机管理密码，并将其中两项改为：

```text
PORTPILOT_IMAGE_REPOSITORY=ghcr.io/maodeyu180/portpilot
PORTPILOT_IMAGE_TAG=0.1.0
```

然后执行：

```sh
docker compose pull init portpilot mihomo
docker compose up -d --no-build --wait
```

面板与初始化服务使用同一镜像，Mihomo 仍使用其官方镜像。不需要在服务器编译 Node 或 Go。

升级时先备份数据，修改版本号，再只更新面板：

```sh
docker compose pull portpilot init
docker compose up -d --no-deps --no-build portpilot
```

这不会重建 Mihomo。初始化服务在下一次完整启动时使用新版本，但保留已有数据和已应用配置。

## Docker Hub 可选

目前默认只发布 GHCR。如果后续需要同步 Docker Hub，再配置 Docker Hub 用户名以及具有镜像写权限的 Access Token：用户名可作为 Actions Variable，Token 保存在仓库 Actions Secrets 中。不要将 Token 写入源码、`.env.example` 或聊天记录。

参考：[GitHub 镜像发布](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)、[GHCR 认证与可见性](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)。
