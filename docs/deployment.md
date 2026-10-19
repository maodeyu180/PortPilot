# 部署 PortPilot

## 原生二进制 + systemd

在开发机执行 `make linux-amd64`，将 `bin/portpilot-linux-amd64` 作为 `portpilot` 上传到服务器 `/opt/portpilot/`。ARM64 服务器改为 `GOARCH=arm64` 构建。无需在目标服务器安装 Node 或 Go。

使用独立系统用户 `portpilot`，并准备以下目录（示意，安装步骤请按服务器规范执行）：

```text
/opt/portpilot/
├── portpilot             Go 可执行文件，0755
├── mihomo                官方 Mihomo 二进制，0755
├── admin.env             管理密码，root:root 0600
└── data/                 portpilot:portpilot 0700
    ├── state.json        导入后生成，0600
    ├── admin.json        管理密码加盐哈希 / 待初始化状态，0600
    └── mihomo.yaml       下载 / 应用配置，0600
```

`admin.env` 内容：

```text
# 留空时在首次访问页面设置，也可提前指定至少 12 个字符的密码。
PORTPILOT_ADMIN_PASSWORD=
```

将 `examples/portpilot.service` 安装到 `/etc/systemd/system/`，加载新单元并启动服务：

```sh
systemctl daemon-reload
systemctl enable --now portpilot.service
```

访问 `http://服务器IP:8787`。未指定管理密码时，在网页直接设置并确认管理密码。登录后导入订阅并设置入口公网 IP。初次启动内核前，从运行配置页下载 YAML，保存到 `/opt/portpilot/data/mihomo.yaml`，权限与所有者按上面调整。

下载与你的操作系统 / CPU 对应的 [Mihomo 官方发行版](https://github.com/MetaCubeX/mihomo/releases)，核对官方校验信息后放入 `/opt/portpilot/mihomo`。安装 `examples/portpilot-mihomo.service` 并启动：

```sh
systemctl daemon-reload
systemctl enable --now portpilot-mihomo.service
```

回到面板检测控制器，应用当前配置并测试节点。后续修改通过面板应用，不需要重启面板。

检查服务：

```sh
systemctl status portpilot portpilot-mihomo
journalctl -u portpilot -n 50 --no-pager
journalctl -u portpilot-mihomo -n 50 --no-pager
```

## Docker（Linux）

推荐用于自托管和源码部署。需要 Docker Engine 与 Docker Compose v2。默认包含：

以下步骤从源码构建。使用已经发布的 GHCR 镜像可跳过构建，见 [镜像安装与版本发布](releasing.md)。

| 服务 | 作用 |
| --- | --- |
| `init` | 一次性初始化数据和空内核配置，成功后退出 |
| `portpilot` | 管理面板，默认监听 `8787`，有健康检查 |
| `mihomo` | 独立代理内核，控制器仅监听 `127.0.0.1:9090` |

在源码目录中执行：

```sh
sh scripts/setup.sh
docker compose up -d --build --wait
```

初始化脚本只创建权限为 `0600` 的 `.env`，管理密码默认留空。重复执行不覆盖已有文件。也可手动复制 `.env.example`，提前填写至少 12 个字符的独立管理密码，跳过首次设置。

访问 `http://服务器IP:8787`，在首次设置页填写至少 12 个字符的管理密码并再次确认，保存后自动进入控制台，无需查看启动日志。设置完成前，订阅、节点、配置导出等管理接口全部锁定，后台订阅同步也不会开始。

首次设置接口不要求登录，第一次成功提交即确定管理密码。部署后请及时完成设置；需要避免公开初始化页面时，提前配置 `PORTPILOT_ADMIN_PASSWORD`。

登录后在「运行配置」填写客户端可访问的入口 IP，导入订阅，点击「应用配置」，再测试并导出地址。内核已经自动启动，初始空配置不会开放代理端口，不需要手动下载或启动 Mihomo。

面板和内核均以 UID/GID `10001:10001` 运行，根文件系统只读，无额外 capabilities。两个服务使用 Linux host 网络，避免为动态代理端口维护 Docker 映射；控制器保留本机监听。初次初始化容器没有网络访问能力。

### 账号密码

- **管理密码**：首次在网页设置，或由 `.env` 中的 `PORTPILOT_ADMIN_PASSWORD` 指定，用于登录面板。网页设置的密码以 PBKDF2-HMAC-SHA256 加盐哈希保存在 `data/admin.json`，不保存明文。
- **代理账号密码**：首次自动生成，默认用户名 `portpilot`，密码为 24 位随机十六进制字符串。在面板「访问认证」查看或修改。
- **内核控制器密钥**：首次随机生成，只用于面板与内核通信。

三者互相独立。每一个 mixed 监听入口都会显式写入非空 `users`，HTTP 与 SOCKS5 都需要认证。用户名为空或密码不足 8 位时，保存与配置生成都会拒绝；没有免认证开关。修改代理凭据后，保存并点击「应用配置」，再更新客户端。

管理密码设置后，清空环境变量或重建容器仍使用数据卷里的密码，不会重新开放初始化。非空 `PORTPILOT_ADMIN_PASSWORD` 在启动时优先生效，服务器管理员可用它恢复或更换管理密码；环境变量留空后使用最近保存的密码。首次设置只能成功一次，完成后该接口无法再次修改密码。认证文件损坏时程序拒绝启动，不会悄悄回到首次设置。

备份时保留整个数据卷，包括 `admin.json`。通过公网使用首次设置或登录时，应使用 HTTPS 或 SSH 隧道。

新安装可通过 `.env` 的 `PORTPILOT_PROXY_USERNAME` / `PORTPILOT_PROXY_PASSWORD` 自定义初始凭据，留空则自动生成。以下变量**只对首次创建的数据生效**，不会覆盖已有数据：

| 变量 | 默认值 |
| --- | --- |
| `PORTPILOT_PUBLIC_HOST` | `127.0.0.1` |
| `PORTPILOT_CONTROLLER_URL` | `http://127.0.0.1:9090` |
| `PORTPILOT_PROXY_USERNAME` | `portpilot` |
| `PORTPILOT_PROXY_PASSWORD` | 自动随机生成 |

已有安装请在面板修改设置。重建容器不会重置密码、端口分配或已应用配置，也不会把尚未应用的面板改动自动应用到内核。首次启动的空配置不开放任何代理端口。

### 数据与构建

`data` 命名卷同时挂载到两个服务的 `/app/data`；默认项目名为 `portpilot`，卷名为 `portpilot_data`。`state.json`、`admin.json` 与 `mihomo.yaml` 权限为 `0600`，数据目录为 `0700`。该卷独立于容器与镜像；不要执行删除数据卷的命令。`init` 已退出成功是正常状态。

首次构建需要下载 Node、Go 和依赖。如果服务器无法访问 Go 默认依赖源，可在 `.env` 设置：

```text
PORTPILOT_GOPROXY=https://goproxy.cn,direct
```

这只影响镜像构建，Go 的模块校验保持开启。Mihomo 默认固定为 `metacubex/mihomo:v1.19.32`；升级时单独调整 `MIHOMO_IMAGE`，避免面板升级顺带改变内核版本。

```sh
docker compose ps -a
docker compose logs --tail 100 portpilot mihomo
```

本方案按 Linux Engine 验证。Docker Desktop 需要额外启用 [host networking](https://docs.docker.com/engine/network/drivers/host/)，Mac / Windows 用户也可使用 Linux 虚拟机或原生二进制。

### 升级

只更新面板与界面，在替换源码后执行：

```sh
docker compose up -d --no-deps --build portpilot
```

代理内核继续运行，数据卷与代理端口映射保留。固定 `PORTPILOT_IMAGE_TAG` 可保留不同版本镜像用于回退。回退同样用 `up -d --no-deps portpilot`，并确认数据格式与旧版本兼容。

单独更新内核时，调整 `.env` 中的 `MIHOMO_IMAGE` 后执行：

```sh
docker compose pull mihomo
docker compose up -d --no-deps mihomo
```

更新内核会中断现有代理连接。建议先备份，并在维护窗口操作。

### 备份与恢复

为保证两份配置一致，备份时先暂停面板写入；Mihomo 继续转发。以下命令在项目目录执行，备份文件位于已忽略的 `backups/`：

```sh
umask 077
mkdir -p backups
docker compose stop portpilot
docker compose run --rm -T --no-deps --entrypoint tar portpilot \
  -czf - -C /app data/state.json data/mihomo.yaml > backups/portpilot-data.tar.gz
cp -p .env backups/portpilot.env
docker compose start portpilot
```

每次备份前为旧备份改名，或使用带日期的文件名。备份包包含真实密钥，不要上传到公开仓库。

在新安装的**空数据卷**恢复：先还原备份的环境文件为 `.env`，并准备相应版本的面板镜像，然后执行：

```sh
docker compose build portpilot
docker compose run --rm -T --no-deps --entrypoint tar portpilot \
  -xozf - -C /app < backups/portpilot-data.tar.gz
docker compose up -d --wait
```

恢复必须同时包含 `state.json` 和 `mihomo.yaml`。已有节点却缺少已应用配置时，初始化会停止并提示恢复，不会擅自把待应用改动变为线上配置。

### 从原生 systemd 迁移

迁移前构建面板镜像、拉取 Mihomo 镜像，沿用原 `admin.env` 的管理密码，并确认目标 Docker 数据卷为空。切换时需要停止原来的面板和 Mihomo，避免占用相同端口。

1. 备份 `/opt/portpilot/data/` 和 `/opt/portpilot/admin.env`。
2. 在维护窗口停止 `portpilot.service` 与 `portpilot-mihomo.service`。
3. 将原始 `state.json` 和 `mihomo.yaml` 一起导入新卷，保留内容。通过下面的非 root 容器导入时，文件归 UID/GID `10001:10001` 所有。

   ```sh
   tar -czf - -C /opt/portpilot data/state.json data/mihomo.yaml | \
     docker compose run --rm -T --no-deps --entrypoint tar portpilot -xozf - -C /app
   ```

4. 执行 `docker compose up -d --wait`，检查面板、HTTP 和 SOCKS5 认证。已有账号与固定端口不变。
5. 验证成功后关闭旧 systemd 单元的开机启动，防止重启机器后争抢端口。保留旧程序与备份，用于回退。

回退时先停止 Compose 服务，再恢复旧服务启动。迁移与回退会短暂中断代理连接；不要让两套内核同时监听相同端口。

### 独立集成验证

验证脚本只依赖 Python 3 标准库，使用本机测试上游，不需要真实订阅。请在独立项目名下使用全新的空卷，将测试 `.env` 的 `PORTPILOT_LISTEN` 改为 `127.0.0.1:18787`、`PORTPILOT_CONTROLLER_URL` 改为 `http://127.0.0.1:19092`，保留 `PORTPILOT_PUBLIC_HOST=127.0.0.1`。

```sh
docker compose -p portpilot-test up -d --build --wait
python3 tests/docker_smoke.py --base-url http://127.0.0.1:18787 --env-file .env
docker compose -p portpilot-test stop
```

覆盖首次初始化、面板登录、内核连接、配置热加载、拒绝空 / 短密码、HTTP / SOCKS5 的缺失 / 错误 / 正确凭据，以及携带凭据的地址导出。脚本拒绝已有节点或订阅的安装，结束时停用测试节点；不要针对生产面板运行。

## 访问方式

局域网使用可让面板监听内网 IP 并设置管理密码。公网使用推荐在 HTTPS 反向代理后运行，或绑定 `127.0.0.1` 并使用 SSH 隧道：

```sh
ssh -L 8787:127.0.0.1:8787 user@server
```

Mihomo 控制器保持回环监听。代理端口需单独在系统防火墙及云安全组中放行，建议按客户端 IP 限制来源。HTTP / SOCKS5 入口的账号密码认证本身不提供传输加密。

## 备份与升级

备份整个 `data/` 和 `admin.env`，它们含有真实密钥，需单独保护。`state.json` 是端口分配的持久依据，重新导入订阅不能重建历史分配。

升级时先备份并保留私有数据，再用新版本替换程序。不要将 `data/`、`.env`、`admin.env` 或导出的代理配置上传到公开仓库。项目的 `.gitignore` / `.dockerignore` 已排除默认数据和本地构建产物。
