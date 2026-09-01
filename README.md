# ServerPal

ServerPal 是面向 Ubuntu 和 Node.js 项目的轻量服务器控制面板。控制台使用 React，核心与本地代理使用 Go；PM2 管理项目进程，Caddy 完成域名反向代理、HTTPS 签发和自动续期。

## 已实现

- CPU、负载、内存、磁盘和运行时间监控
- Node 入口文件或 npm script 项目配置
- PM2 启动、停止、重启、集群实例、状态、资源与日志
- 域名绑定、Caddy 配置校验、自动 HTTPS 和证书到期检查
- 项目文件浏览、编辑、上传、下载、重命名和可恢复删除
- 降权到 `serverpal-app` 用户的 WebSocket PTY 终端
- Argon2id 密码、HttpOnly 会话、CSRF、登录限流与操作审计
- Linux `openat2/openat` 目录边界，阻断路径穿越、符号链接换链和覆盖竞态
- Ubuntu systemd 服务、一条命令安装、版本目录更新与健康检查回滚
- 深色与浅色主题、桌面和移动端布局

## 安全边界

```text
Browser
   |
serverpal-web (serverpal 普通用户)
   |
Unix Socket + 随机令牌
   |
serverpal-agent (root，仅结构化白名单操作)
   |-- PM2 (serverpal-app 用户)
   |-- project terminal (serverpal-app 用户)
   |-- /srv/serverpal/apps 文件边界
   `-- Caddy 配置校验后 reload
```

网页服务不以 root 运行。项目终端也不是 root shell。文件接口只能访问 `allowedRoots`，删除操作会移动到项目的 `.serverpal-trash`。
`serverpal-app` 不属于控制面用户组，不能读取 Agent 令牌，也不能访问 Agent Socket。PM2 配置写入和日志读取同样以该业务用户身份执行。

## 一条命令安装

建议为面板准备一个已经指向服务器的域名，然后在全新的 Ubuntu 24.04 或 26.04 LTS 服务器执行：

```bash
curl -fsSL https://raw.githubusercontent.com/778777266/ServerPal/main/install.sh | sudo env SERVERPAL_PANEL_DOMAIN=panel.example.com bash
```

安装器支持 `amd64` 和 `arm64`，会安装并校验 Node.js LTS、固定版本 PM2、Caddy 官方软件包及 ServerPal 发布包。完成后会输出随机管理员密码。

没有面板域名时也可以安装：

```bash
curl -fsSL https://raw.githubusercontent.com/778777266/ServerPal/main/install.sh | sudo bash
```

此时服务只监听 `127.0.0.1:8787`，安装器会输出 SSH 端口转发命令，不会把未加密的管理端口暴露到公网。

> 远程一键安装依赖 GitHub 仓库中存在 Release。推送 `v0.1.0` 这类标签后，工作流会自动构建并发布 Linux `amd64/arm64` 包。若仓库地址不同，先修改 `install.sh`、`scripts/serverpal-update` 和 `packaging/config.example.json` 中的默认仓库名，或传入 `SERVERPAL_REPO=owner/repo`。

首次发布到默认仓库后，推送版本标签即可触发 Release：

```bash
git tag v0.1.0
git push origin main v0.1.0
```

## 更新和恢复

```bash
sudo serverpal-update
```

更新流程会：

1. 下载发布包并验证 SHA-256。
2. 在线生成 SQLite 一致性备份，并备份配置和 systemd 单元。
3. 停止网页控制面，但保持业务 PM2 项目运行。
4. 在新版本目录执行数据库迁移。
5. 原子切换 `/opt/serverpal/current`。
6. 重启控制面并检查 `/api/health`。
7. 任一步失败时恢复数据库、配置、systemd 单元、二进制和旧版本软链接。

更新备份位于 `/var/lib/serverpal/backups/`。重置管理员密码：

```bash
sudo -u serverpal /opt/serverpal/current/serverpal reset-password --config /etc/serverpal/config.json
```

## 本地开发

要求 Node.js 22+ 和 Go 1.23+。

```bash
npm install
npm run build
go run ./cmd/serverpal dev
```

开发账号为 `admin`，首次运行会输出本地开发密码。仅预览界面和演示数据：

```bash
npm run dev:demo
```

生产构建与测试：

```bash
npm run lint
npm run build
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
```

本地构建 Linux 发布包：

```bash
bash scripts/build-release.sh v0.1.0
```

## 主要目录

```text
cmd/serverpal/       CLI 与服务入口
internal/agent/      特权代理、PM2、Caddy、文件、监控与 PTY
internal/api/        登录、审计和浏览器 API
internal/store/      SQLite 数据和迁移
web/                 React 控制台
packaging/           systemd、Caddy 与配置样例
scripts/             发布构建和安全更新
install.sh           Ubuntu 一键安装器
```

生产数据不放在程序目录：配置位于 `/etc/serverpal`，数据库位于 `/var/lib/serverpal`，项目位于 `/srv/serverpal/apps`，程序版本位于 `/opt/serverpal/releases`。
