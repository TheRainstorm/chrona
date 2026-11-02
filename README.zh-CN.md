# Chrona

[English](README.md) · [简体中文](README.zh-CN.md)

一个轻量、可自行部署的 ZFS 快照与备份 Web 管理器。直接运行在 NAS 上，用各档保留数量代替繁琐的 cron 表达式，所有配置保存在一份普通 YAML 文件里。

Chrona 是一个 Go 可执行文件，内置 HTML/CSS/JavaScript 界面，无需数据库、Node.js 运行时、容器或外部 CDN。唯一的第三方 Go 依赖是 YAML 解析器，目前 Web 界面使用简体中文。

## 核心原理

- **快照把“保留几个”换算成到期时间。** 每档按“间隔 × 保留数量”计算，写入唯一的 `chrona:expires` 属性，月档按真实日历计算。多档同时触发只创建一张快照，由最长档决定期限，同时覆盖短档；清理时检查是否到期，保留 `keep` 和受保护的备份基点。
- **备份保住两端共同的增量基点。** `zfs send -i` 从两端共有的快照 GUID 出发，只传变化的数据块。新备份快照的两端期限都是“下次备份时间 + 缓冲”，延迟或失败时由 hold 继续保护旧基点，验证新备份成功后才替换。因此，本地快照再频繁，也不会把增量链清掉。

## 能做什么

- **快照管理**：一行显示完整数据集路径、策略名、各档保留数量、上次快照和下次时间，直接查看历史、预览计划、编辑、暂停或创建快照。
- **策略管理**：支持 15 分钟、小时、天、周、月五档，新建策略的数量全部默认为 0。子数据集默认继承最近父级的快照策略，也可以在本级覆盖。
- **备份管理**：按路径排序，每个源和目的地单独一行，展示完整路径、规则、上次成功、下次时间和状态；每个目的地都有独立启用开关。只有叶子数据集执行备份，备份没有继承关系。
- **概览**：展示快照占用和任务摘要，复用服务已有的缓存清单，不额外调用 SSH。
- **文件配置**：在界面编辑或下载同一份 YAML，也可以直接修改文件并自动重载；无效配置不会替换正在使用的有效配置。

## 界面预览

快照页面展示策略、继承关系、保留数量和下次执行时间：

![Chrona 快照管理界面](docs/images/snapshots.png)

备份页面每个目的地独立一行，分别设置目标、时间策略和启用状态：

![Chrona 备份管理界面](docs/images/backups.png)

## 安装 Linux 预编译版本

从 [Releases](https://github.com/TheRainstorm/chrona/releases) 下载：x86_64 选 `amd64`，aarch64 选 `arm64`。压缩包内含单文件程序、systemd 服务文件、示例配置、文档和许可证。目标机器需要 Linux、OpenZFS 工具（`zfs`、`zpool`）、systemd，远端备份还需要 SSH 客户端；无需安装 Go。

```sh
chrona_version=0.11.0
chrona_arch=amd64                 # ARM 机器改为 arm64
chrona_archive="chrona_${chrona_version}_linux_${chrona_arch}.tar.gz"
chrona_url="https://github.com/TheRainstorm/chrona/releases/download/v${chrona_version}"
curl -fLO "$chrona_url/$chrona_archive" &&
curl -fLO "$chrona_url/SHA256SUMS" &&
sha256sum --ignore-missing -c SHA256SUMS &&
tar -xzf "$chrona_archive" &&
cd "chrona_${chrona_version}_linux_${chrona_arch}" &&
./chrona -version
```

私有 fork 可使用已登录的 `gh release download` 下载。若想先体验界面，运行 `./chrona -listen 127.0.0.1:8086`，打开 [localhost:8086](http://127.0.0.1:8086)；默认 Demo 使用模拟数据，不操作 ZFS。

首次安装 systemd 服务：

```sh
sudo install -d -m 755 "/opt/chrona/releases/$chrona_version"
sudo install -m 755 chrona "/opt/chrona/releases/$chrona_version/chrona"
sudo ln -sfn "/opt/chrona/releases/$chrona_version" /opt/chrona/current
sudo install -d -m 700 /etc/chrona /var/lib/chrona
sudo test -f /etc/chrona/config.yaml || sudo install -m 600 config.example.yaml /etc/chrona/config.yaml
sudoedit /etc/chrona/config.yaml
sudo sh -c 'umask 077; test -s /etc/chrona/web-password || openssl rand -hex 24 > /etc/chrona/web-password'
sudo install -m 644 chrona.service /etc/systemd/system/chrona.service
sudo systemctl daemon-reload
sudo systemctl enable --now chrona.service
```

启动前先填写本机数据集和计划：[服务文件](deploy/chrona.service) 已开启真实快照、到期清理和备份。默认监听 `127.0.0.1:8086`；如需局域网访问，在 `/etc/chrona/chrona.env` 中写入 `CHRONA_LISTEN=192.168.1.10:8086`（换成本机 IP），然后重启 `chrona`。HTTPS 可通过反向代理提供。

登录账号为 **chrona**，用 `sudo cat /etc/chrona/web-password` 查看自动生成的密码。配置在 `/etc/chrona/config.yaml`，运行状态在 `/var/lib/chrona/state.json`，SSH 复用连接在 `/run/chrona/ssh`。root 服务读取 `/root/.ssh/config`，请事先配置接收端的密钥登录和主机密钥。查看日志：`sudo journalctl -u chrona -n 50 --no-pager`。

## 升级与回退

按上面的方法下载并校验新版本。在新压缩包的解压目录中，等正在运行的备份结束后执行：

```sh
chrona_previous=$(readlink -f /opt/chrona/current)
chrona_backup="/var/backups/chrona/$(date +%Y%m%d-%H%M%S)"
sudo systemctl stop chrona
sudo install -d -m 700 "$chrona_backup"
sudo cp -a /etc/chrona "$chrona_backup/config"
sudo cp -a /var/lib/chrona "$chrona_backup/state"
sudo install -d -m 755 "/opt/chrona/releases/$chrona_version"
sudo install -m 755 chrona "/opt/chrona/releases/$chrona_version/chrona"
sudo ln -sfn "/opt/chrona/releases/$chrona_version" /opt/chrona/current
sudo systemctl start chrona
sudo systemctl status chrona --no-pager
```

升级只替换程序，保留配置、密码和服务设置。保留旧版本目录，如需回退程序：

```sh
sudo ln -sfn "$chrona_previous" /opt/chrona/current
sudo systemctl restart chrona
```

## 从源码构建

需要 Go 1.24 或更新版本：

```sh
git clone https://github.com/TheRainstorm/chrona.git
cd chrona
make build
./bin/chrona -listen 127.0.0.1:8086
```

源码构建同样默认运行 Demo。直接使用 `-mode zfs` 启动时默认只读，加入 `-enable-zfs-writes` 才允许写入；Demo 与 ZFS 模式使用不同状态文件。

## 不写 cron，只设保留数量

定义一个起始时间，再填写每档保留数量。例如：

```yaml
version: 3
timezone: UTC
jitter_seconds: 30
policies: []
datasets:
  - name: tank/data
    snapshot:
      start_at: "2026-01-01T00:00:00Z"
      keep: {quarter_hourly: 0, hourly: 3, daily: 7, weekly: 4, monthly: 12}
backups: []
```

数量为 0 表示关闭该档。较长档位的保留总跨度不能小于较短档位；多档同时触发时只创建一张快照，由最长档决定到期时间。稳定的随机偏移会分散执行时间，月档按真实日历计算。公共策略引用和备份计划的完整格式见示例配置。

已有快照也采用统一的到期规则：

- 快照名称的 `@` 后缀含 `keep` 时永久保留，不区分大小写，并且优先于已有的有限到期属性。数据集路径中的 `keep` 不影响判断。
- 其他快照如果有有效的 `chrona:expires` Unix 时间戳，就遵守这个期限，包括其他工具创建的快照。
- 没有有效到期属性时，默认按**创建时间加 30 天**计算，无需批量改写旧快照的属性。

清理只在成功创建快照或完成复制后处理当前精确数据集。已有 hold 和记录中的增量基点受保护；尚未确认共同基点的备份源会暂缓清理。删除前重新核对快照身份、期限和 hold，不递归、不强制删除。

## 原生增量备份

通过 ZFS send/receive 进行本机跨池复制，或使用 `backup-nas` 这样的 SSH 主机别名传到远端。程序使用运行账号的 SSH 密钥、配置和已验证的主机密钥，并复用连接。本机目的地可设置 `host: local`。

主机填 `nas-pve` 这样的 SSH 别名，端口留空，即使用 `~/.ssh/config` 中的 `HostName`、`User`、`Port` 和 `IdentityFile`。使用提供的 root systemd 服务时，配置文件位于运行 Chrona 的机器上的 `/root/.ssh/config`。也可以直接填写 `backup.example.com:2202` 或 `user@backup.example.com:2202`。显式填写的端口优先于 SSH 配置；两者都没有指定时，OpenSSH 默认使用 22。

公共 `backup_policies` 只定义时间。每条 `backups` 独立设置目标 `host`、可选的 `port`、数据集路径与 `policy` 引用，同一个时间策略可以用于本机和不同远端。选择“自定义时间”可以单独设置这条备份的时间。旧的组合规则会在内存中拆分，任务 ID 和增量基点保持不变；保存时写入新格式。

每周／每月频率只决定**何时执行备份**。备份不再设置周档、月档保留数量：每次新建专用快照，两端到期时间统一为**下一次备份时间 + `grace_minutes`**，默认缓冲 60 分钟，与数据集快照保留策略独立。已有快照期限不会因移除保留数量而缩短；旧配置的 `keep_weekly` / `keep_monthly` 兼容读取但不再生效，下次保存时移除。

核心命令是 `zfs send -w -p -i BASE NEW | zfs receive -u -s DEST`，首次全量省略 `-i BASE`。逐个复制叶子数据集，不使用递归的 `-R`、携带中间快照的 `-I` 或强制接收的 `-F`。

增量传输要求接收端最新快照的 GUID 在源端存在。两端基点至少保留到下次备份加缓冲时间，验证成功后才推进基点；中断传输支持续传。不会为了传输而强制回滚已有目的地，新接收的数据集保持未挂载和只读。

## CI 与自动发布

GitHub Actions 会检查 PR 和 `main` 推送，并交叉编译 Linux `amd64`、`arm64` 静态程序。推送 `v*` 版本标签后，通过检查的两个架构压缩包及 `SHA256SUMS` 会自动发布到 GitHub Releases。包内 `BUILDINFO.txt` 记录版本、commit 和 Go 工具链，运行 `chrona -version` 可查看版本信息。CI 负责发布文件，服务器升级仍由部署操作控制。

在源码仓库中运行 `./scripts/release.sh v0.11.0` 生成同样的发布包，输出在 `dist/`。构建使用 `CGO_ENABLED=0`，不依赖外部 C 运行库。

## 开发

运行 `make test` 执行 Go 测试，`node --check web/app.js` 检查 JavaScript 语法。[scripts/browser-smoke.cjs](https://github.com/TheRainstorm/chrona/blob/main/scripts/browser-smoke.cjs) 使用 Playwright 验证独立 Demo，会修改演示配置，不能指向生产服务。修改 Go 或内置 Web 文件后需要重新构建；修改 YAML 不需要。

## 许可证

[MIT](LICENSE) © 2026 Fuyan Yuan。
