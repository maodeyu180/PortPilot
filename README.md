# PortPilot

**一个节点，一个固定入口。**

[![CI](https://github.com/maodeyu180/PortPilot/actions/workflows/ci.yml/badge.svg)](https://github.com/maodeyu180/PortPilot/actions/workflows/ci.yml)

蓝白主题、轻量自托管的 Mihomo 订阅管理面板。把订阅与节点分享链接映射为带账号密码的 HTTP / SOCKS5 端口，在浏览器、脚本和工具里直接使用。

```text
订阅 / 节点 → PortPilot → Mihomo
                           ├── :20001 → 节点 A
                           ├── :20002 → 节点 B
                           └── :20003 → 节点 C
```

每个端口都是 mixed 入口，同时支持：

```text
http://用户名:密码@服务器:20001
socks5h://用户名:密码@服务器:20001
```

## 功能

- 服务概览展示节点数量、测试通过率、最近一次延迟分布、可用入口与操作记录，支持点击指标筛选节点。
- 通过 URL 或粘贴内容导入订阅，自动识别 Clash / Mihomo YAML / JSON、节点分享链接和 Base64，按小时自动刷新。
- 添加节点支持粘贴或选择文件，可先转换为 YAML 预览、编辑，再批量分配端口。
- 订阅和独立节点的增删改查，按测试结果、协议和来源筛选，支持搜索、分页与批量启停。
- 按「订阅 ID + 来源节点名」保存固定端口，刷新和排序不改变端口。
- 源中消失的节点保留端口、标记失效；再次出现时恢复原映射。
- 两种协议共用账号密码，支持包含特殊字符的凭据和 IPv6 入口。
- 导出 HTTP / SOCKS5H 地址，预览和下载 Mihomo YAML。
- 对接同机 Mihomo 控制器、应用配置、真实连接延迟测试。
- 操作日志、管理密码登录、首次访问设置密码、原子文件存储、敏感数据与源码分离。
- 蓝白主题、可收起的侧边栏与手机布局，地址复制同时兼容本机和局域网 HTTP 面板。
- Go 后端 + React 前端，前端嵌入单个可执行文件，运行时不依赖 Node。
- Docker Compose 包含面板、独立 Mihomo 内核与一次性初始化，首次启动自动生成随机凭据。

## Docker 启动（Linux，推荐）

安装 Docker Engine 和 Docker Compose v2 后，在项目目录执行：

```sh
sh scripts/setup.sh
docker compose up -d --build --wait
```

打开 `http://服务器IP:8787`。默认首次访问会引导设置管理密码：输入至少 12 个字符的密码并再次确认，即可进入控制台，没有共用的默认密码。部署后应及时完成首次设置；也可提前指定管理密码。

也可提前在 `.env` 中填写 `PORTPILOT_ADMIN_PASSWORD`，跳过首次设置。脚本不覆盖已有 `.env`；已设置的密码保存在数据卷内，留空环境变量或重建容器不会重置密码。

1. 在「运行配置」设置客户端能访问的入口 IP / 域名。
2. 导入订阅，点击「应用配置」。Mihomo 已由 Compose 自动启动，无需手动安装。
3. 在「访问认证」查看代理账号密码，测试节点后导出地址。

**每个代理端口都强制使用账号密码，HTTP 与 SOCKS5 共用一组凭据。** 代理密码首次自动生成，与面板管理密码独立；不提供免认证模式。

数据保存在 Compose 的 `data` 命名卷中（默认卷名 `portpilot_data`），不会写入镜像。`init` 服务完成后退出，面板和内核分别运行。升级、备份、迁移与依赖下载源设置见 [部署说明](docs/deployment.md)。

正式版本支持从 GHCR 拉取 amd64 / arm64 镜像，也可配置 Docker Hub 同步发布。镜像由版本标签触发 GitHub Actions 发布；首次发布和包公开完成后可使用，见 [版本发布与镜像安装](docs/releasing.md)。

## 本地启动

构建需要 Go 1.24+ 与支持 Vite 7 的 Node.js（建议 22.12+ / 24 LTS）。包管理器使用 npm。

```sh
make install
make build
./bin/portpilot -listen 127.0.0.1:8080 -data ./data
```

打开 `http://127.0.0.1:8080`。默认只接受本机访问；首次访问时直接设置并确认管理密码。

开发界面：

```sh
# 终端 1：构建并启动后端
make build
./bin/portpilot

# 终端 2：前端热更新，API 代理到 8080
cd web
npm run dev
```

## 让代理实际工作

PortPilot 管理配置；[Mihomo](https://github.com/MetaCubeX/mihomo) 负责代理转发。Compose 已包含内核；以下手动安装步骤供原生二进制部署使用。

1. 导入订阅，或粘贴节点分享链接、Base64、Clash / Mihomo 配置。
2. 在「运行配置」填写客户端能够访问的 IP / 域名与端口范围。
3. 在「访问认证」设置代理凭据。
4. 下载生成的配置，保存为数据目录中的 `mihomo.yaml`。
5. 检查配置并启动同机内核（路径请按实际调整）：

   ```sh
   mihomo -t -d ./data -f ./data/mihomo.yaml
   mihomo -d ./data -f ./data/mihomo.yaml
   ```

6. 回到面板检测连接，点击「应用配置」，再运行连接测试并导出地址。

配置中的控制器地址与 secret 必须与面板一致。面板仅连接本机 HTTP 控制器，默认地址 `http://127.0.0.1:9090`。首次配置下载后，控制器参数已经自动匹配。

公网 / 局域网客户端需要能访问已启用的代理 TCP 端口。UDP 默认关闭；启用后需要相应 UDP 放行及客户端支持。管理端口和代理端口是两回事。

## 部署

优先使用上面的 Docker Compose 方式；也支持原生二进制 + systemd，见 [部署说明](docs/deployment.md)。

```sh
PORTPILOT_ADMIN_PASSWORD='请替换为至少12位的管理密码' \
  ./bin/portpilot -listen 0.0.0.0:8787 -data ./data
```

管理密码留空时进入首次设置，完成前所有管理数据与操作接口均不可访问。管理密码与代理密码互相独立。通过公网访问面板时，使用带 HTTPS 的反向代理或 SSH 隧道。

## 数据与端口约定

- 原生数据位于 `-data` 指定目录，默认 `./data`；容器内为 `/app/data`，映射到持久命名卷。
- `state.json` 保存订阅 URL、节点配置、代理认证信息与端口映射；`mihomo.yaml` 保存最近一次成功应用的配置；`admin.json` 保存管理密码加盐哈希或待初始化状态。文件权限为 `0600`，数据目录默认 `0700`，备份时一起保存。
- 自动同步只更新面板，**需要手动应用**后才更新内核，避免订阅自动改变正在使用的出口。
- 节点来源名变更视为新节点，会分配新端口；旧映射保留为失效项，可自行删除。
- 显示别名和启用状态在订阅刷新时保留。
- 「剩余流量」「距离下次重置剩余」「套餐到期」等订阅信息项单独展示，不占用代理端口。
- 删除订阅会一并删除该来源的映射。单独删除订阅节点后，下次刷新会重新导入；长期停用请用开关。
- 修改端口范围不会自动重排已有端口；新范围必须包含已有映射。
- 仅提取节点，不执行来源中的规则、脚本、远程 UI 或 TUN 设置。使用 `dialer-proxy` 的链式节点暂不支持。
- 支持单个 YAML / JSON 节点、`proxies` 配置、节点数组、每行一条的分享链接，以及这些内容的标准 / URL-safe Base64（允许换行和省略补位）。单次上限 5 MiB、2000 个节点。
- 分享链接支持 VLESS、VMess（Base64 JSON）、Shadowsocks（SIP002 / 旧版）、SSR、Trojan、AnyTLS、Hysteria、Hysteria2 / HY2、TUIC v5、HTTP / HTTPS、SOCKS / SOCKS5 / SOCKS5H。VLESS / VMess / Trojan 支持 TCP、WS、HTTP、H2、gRPC、HTTPUpgrade；保留适用协议的 TLS / Reality、SNI、ALPN、指纹与认证参数。其他协议和传输方式请使用 Mihomo YAML。
- 「添加节点」的“识别并转换”只预览，不保存或连接上游；可跳过转换直接导入。端口留空时自动分配，填写时按输入顺序连续分配。格式错误、重名或端口冲突会取消整批操作，不会保存部分节点。
- 连接测试测量内核访问测试 URL 的延迟，**不代表公网端口已放行或出口 IP 已核实**。
- 控制器地址或密钥变更后，需同步修改内核启动配置。后续应用通过 `PUT /configs` 热加载；不管理内核进程的启动、停止和升级。

## 测试

```sh
make test
```

覆盖订阅与分享链接解析、Base64 兼容、端口保留、失效恢复、批量导入回滚、并发存储、YAML 路由、凭据转义、认证、跨站请求、订阅 / 节点管理及内核配置应用。生产构建执行 `npm run build`。Docker 集成检查还会把 18 个分享链接样例转换后交给真实 Mihomo 加载，并验证 HTTP / SOCKS5H 的认证与转发。

本地已有 Mihomo 时，可指定 `PORTPILOT_MIHOMO_TEST_BINARY=/绝对路径/mihomo make test`，额外检查转换配置与内核的兼容性。

## 目录

```text
cmd/portpilot/      程序入口
internal/control/  HTTP API、订阅同步、存储、配置及内核交互
web/src/           React 界面
examples/          示例配置与 systemd 单元
docs/              部署说明
scripts/           环境文件初始化
tests/             Docker 启动与 HTTP / SOCKS5H 认证集成检查
data/              私有运行数据（已忽略）
```

## 开源

PortPilot 使用 [MIT License](LICENSE)。Mihomo 为独立项目，按其自己的 GPL-3.0 许可证分发。PortPilot 的 UI、代码与发布包不包含用户订阅、账号密码或 Mihomo 二进制。

本项目使用 Mihomo 原生 [mixed 监听入口](https://wiki.metacubex.one/config/inbound/listeners/mixed/)、[直接出口绑定](https://wiki.metacubex.one/config/inbound/listeners/) 和 [控制器 API](https://wiki.metacubex.one/api/)。
