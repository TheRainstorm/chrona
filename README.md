# Chrona ZFS

每机独立运行的 ZFS 快照与备份管理器，保留独立 Demo 模式。Go 服务内置 HTML/CSS/JavaScript，唯一的第三方 Go 依赖是 YAML 解析器，运行时无需 Node.js、Docker 或数据库。

界面采用参考 shadcn/ui 与 Radix 的浅色控制台风格，使用自绘 SVG 图标和系统字体，无外部 CDN 请求。支持桌面和手机布局。

## 源码与版本管理

本地源码与独立 Git 仓库位于 `/home/yfy/Repo/chrona`，主分支为 `main`，当前部署版本标记为 `v0.6.0`。NAS 上的源码仓库为 `/root/Repo/chrona`，本地的私有 Git 远程地址为 `nas-pve:/root/Repo/chrona`。旧目录 `/home/yfy/repo/neuro/homelab/26_chrona_zfs_demo` 保留为指向新仓库的符号链接。

Git 管理 Go 服务、Web 界面、测试、示例配置、systemd 单元及部署脚本。运行数据、部署报告、构建产物和密码文件不入库。NAS 接收 push 后同步工作目录，有未提交的修改时会拒绝覆盖。NAS 服务运行 `/opt/chrona/current/chrona`；修改或拉取源码后，需要重新构建并部署才会更新服务。

```sh
git status
git log --oneline
git push origin main --tags
ssh nas-pve 'git -C /root/Repo/chrona log --oneline -5'
```

## 启动

```sh
cd ~/Repo/chrona
make run
```

打开 http://127.0.0.1:8086。已有构建可直接运行 `./bin/chrona`。首次启动会生成 `data/config.yaml` 和演示状态；示例配置见 `config.example.yaml`。`bin/chrona` 可以单独复制到另一台 Linux amd64 主机运行，网页已经嵌入二进制。

默认命令运行 Demo，只修改本地模拟状态。nas-pve 已部署真实 ZFS 模式，独立服务为 `chrona.service`，地址 `http://192.168.35.253:8086`，账号 `chrona`，密码存于 NAS 的 `/etc/chrona/web-password`。

## 界面

只有 **快照、备份、配置** 三个入口，以数据集为主体。

- **快照**：支持「有快照策略 / 全部数据集」切换，默认显示有策略的数据集；全部清单来自本机 ZFS，可以直接给任意数据集添加策略。筛选保存在浏览器中，查看清单不会自动启用调度。数据集按父子层级排列。父级设置规则后，已有和新发现的子数据集默认继承；每行显示实际生效的规则与来源。
- 每个数据集可选「继承父级」「自定义」或公共策略。本级覆盖优先，未覆盖的后代继续继承；「恢复继承」会重新跟随父级。暂停只作用于当前数据集。
- 每行显示现有快照数量，可切换「现有快照 / 快照计划」。计划同时给出五个档位的下次触发和未来 16 次合并后的执行及到期时间。支持立即创建；移除管理只删除配置。
- **备份**：一行一个数据集，默认收起；展开显示目的地、继承来源、上次传输方式、接收端快照与到期时间。父级只定义默认规则，只有叶子节点执行传输。
- 公共策略放在页面下方的折叠区，可新建与修改。仍被引用的公共策略无法删除。
- **配置**：编辑、保存或下载同一份 YAML，手动修改文件后约 5 秒重新加载。无效配置不会替换有效的运行配置。

## 配置格式

`version: 3` 的快照配置由数据集持有：`datasets[].snapshot` 保存自定义规则，`datasets[].policy` 引用公共策略。没有本级规则时默认继承最近的父级；两者只能选一种。

只需给父级写一条配置，程序就会从本机 ZFS 清单中自动发现后代，无需把每个子数据集写入 YAML。新子数据集在下一次清单刷新后加入，真实模式每 30 秒刷新一次。

```yaml
datasets:
  - name: Saturn
    policy: fine
  - name: Saturn/APP/Immich
    snapshot:
      start_at: "2026-11-01T00:00:00+08:00"
      keep: {quarter_hourly: 12, hourly: 24, daily: 7, weekly: 4, monthly: 12}
```

此时 `Saturn/Archive`、`Saturn/APP/Seafile` 等数据集继承 `fine`；Immich 及其没有单独覆盖的后代使用自定义规则。把 Immich 的 `snapshot` 删除，就恢复父级继承。继承按完整路径段解析，`tank/a` 不会作用于 `tank/ab`。

`disabled: true` 仅暂停当前数据集。`snapshot_mode: none` 会停止本分支的快照继承，后代仍可显式指定规则重新启用。`snapshot_mode: inherit` 与省略本级规则等效，不能与 `policy` 或 `snapshot` 同时填写。继承解析在内存中完成，调度按每个数据集单独执行，支持子级覆盖；清理只处理当前精确数据集。

备份中 `private: true` 表示本级自定义规则，否则可以引用公共规则。`backups[].datasets` 中的源可以是父数据集，目的路径作为前缀；例如源 `Saturn/APP`、目的 `vault/APP` 会展开为 `Saturn/APP/Immich → vault/APP/Immich`。中间路径不执行传输，已有和新发现的叶子节点自动采用最近一级的目的地。子级定义自己的目的地时覆盖全部父级目的地；选择「继承父级」会清除本级覆盖。`backup_mode: none` 停止本分支继承，后代仍可显式设置规则。各映射的 `disabled: true` 暂停该规则。

`grace_minutes` 为下一次备份后的缓冲，省略时为 60 分钟。源基点的到期时间至少为「下次备份＋缓冲」；接收端快照同样有 `chrona:expires`，同时遵守备份保留数量，不会早于这个下限。月任务使用真实日历的下次时间，不能把月份视为固定 30 天。修改备份时间时也会延长现有共同基点的保护期。

Demo 在 `demo-state.json` 的 `replicas` 中保存接收端快照。只有两端都存在相同源快照的副本才标记增量，否则模拟全量。新快照接收和到期信息全部成功后，才一起推进基点并释放上一版本的 hold；失败保留上次两端基点。过期但仍为当前基点的快照受 hold 保护，避免失败或延期破坏增量链。源和目的使用相同快照名的后缀；真实接入仍需使用 ZFS GUID 验证共同快照。

旧版 `version: 1`、`version: 2` 文件首次启动时自动迁移，并保存 `config.yaml.v1.bak` 或 `config.yaml.v2.bak`。原有数据集分配、暂停状态、快照与到期时间保持不变。

## 调度与清理规则

1. `quarter_hourly`、`hourly`、`daily`、`weekly` 分别按起点加 900、3600、86400、604800 秒的整数倍触发。日/周是固定时长；有夏令时的时区会随当地钟表时间变化。默认 Asia/Shanghai 不受此影响。
2. 月档按起点所在时区的日历月计算。1 月 31 日后依次是 2 月最后一天、3 月 31 日，不逐月漂移。
3. 保留数量为 0 表示不启用该档。启用档位之间的总保留跨度必须单调不减。月跨度以公历 400 年周期内的最短实际跨度校验，不能简单按 30 天估算。
4. 各档同一逻辑触发点只创建一张快照，最高档决定过期时间。更高档同时满足当时命中的低档需求。
5. 每个策略由 ID 和 dataset 确定一个稳定的 0～`jitter_seconds` 秒偏移，所有档位共用该偏移。偏移不累计，重启不会漂移。
6. 每张真实快照仅设置一个自定义属性：`chrona:expires=<Unix 时间戳>`。档位、任务等信息不写入 ZFS 自定义属性。真实快照命名为 `chrona_YYYYMMDDTHHMMSSZ`，时间是计划触发点；ZFS 自带 `creation` 仍为实际创建时间。
7. 创建成功后才清理当前精确 dataset 中已经到期、带本程序前缀及过期属性、没有 ZFS hold 的快照。不递归删除子 dataset，不处理手动或旧脚本快照；清理失败保留并记录。
8. 手动快照使用最细启用档的保留跨度。调度器不伪造停机期间的历史数据；重启直接计算下一个触发点。
9. 本 Demo 以快照创建时写入的过期时间为准。修改策略只影响新快照，不追溯修改已有快照的到期时间。

## 真实 ZFS 模式与 nas-pve 部署

nas-pve 的正式服务是 `chrona.service`，开机自启，二进制位于 `/opt/chrona/current/chrona`。配置唯一来源为 `/etc/chrona/config.yaml`，真实状态为 `/var/lib/chrona/state.json`；不能加载 Demo 状态。Web 只监听管理网地址 `192.168.35.253:8086`，HTTP Basic 用户名为 `chrona`，密码在 `/etc/chrona/web-password`，文件仅 root 可读。SSH 复用套接字位于 `/run/chrona/ssh`，使用本机 root 已有的 SSH 配置和已验证主机密钥。

```sh
ssh nas-pve 'systemctl status chrona.service'
ssh nas-pve 'journalctl -u chrona.service -n 50 --no-pager'
ssh nas-pve 'cat /etc/chrona/web-password'
```

服务支持本机跨池复制（host 为 `local` 或本机 hostname），以及通过 SSH 发送到其他主机。读取真实的 snapshot GUID，并要求目标的最新快照在源端存在相同 GUID；已有目标不能无基点覆盖。使用精确叶子的 `zfs send -w -p -i`，不递归发送中间快照，不使用 `recv -F` 自动回滚目标。新的接收数据集不自动挂载，设为只读；zvol 不暴露设备。对现有接收数据集保留挂载设置，成功后设只读。中断传输使用 `receive_resume_token` 恢复，再发送新的增量。[OpenZFS send](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-send.8.html)、[receive](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-receive.8.html)。

源与接收端均设置 `chrona:expires` 并保留 `chrona:<任务 ID>` hold。接收后先核对 GUID、到期属性与 hold，再持久化推进基点，最后释放该任务的旧 hold。失败或中断保留旧版本；修改备份计划时异步延长现存共同基点期限，离线失败会保留 hold 并显示警告。删除或更改目标时只释放本服务持有的标签。清理只处理精确数据集里本程序创建、到期且没有 hold 的快照，不递归删除。Web 会显示任务失败原因和清理警告。

NAS 原来 6 条启用的快照 cron 已在验证通过后注释，由服务接管。原始 crontab 和脚本备份位于 `/root/chrona-deploy-backup-20261101-044808/`。沿用 Demo 中用户调整过的策略，补上原 rpool 日快照范围，并给 `Saturn/backup` 设置停止快照与备份继承，避免自动快照干扰接收端增量链。已有日期命名快照没有改名或批量删除；仅在它作为共同基点时增加保护。

原 n5105 的快照 cron 指向不可执行的旧路径，本次未修改该机器的其他任务。以后若启用接收端独立快照，需要排除备份接收目录；不相干的最新快照会导致任务安全停止并显示冲突，不会自动销毁它们。

真实端到端验证脚本为 `scripts/deploy/verify-native.py`：测试本机与 SSH 的全量和增量、zvol、两端 GUID/期限/hold、克隆恢复文件校验、真实中断与续传，以及重启持久化。它只创建 UUID 临时命名空间，并在成功后移除。测试报告保留在 NAS `/var/lib/chrona-verification/`，不包含密码。部署激活脚本为 `scripts/deploy/activate-service.py`，启用失败时会恢复 crontab 并退回只读服务。

## 文件与服务

```text
cmd/chrona/             服务入口与命令行参数
internal/chrona/        配置、调度、过期清理、API、ZFS 适配与测试
web/                   原生页面、样式、交互；通过 go:embed 打包
config.example.yaml    可编辑配置示例
chrona.service.example systemd 部署示例，生产 systemd 示例
data/config.yaml       配置唯一来源（启动后生成）
data/demo-state.json   演示快照、备份状态和运行记录（启动后生成）
artifacts/             浏览器截图与验证结果
```

修改 Go 或 web 文件后重新 `make build` 并重启程序。只改 YAML 无需重新构建。每个配置路径应只运行一个服务实例。

## 验证

```sh
make test
node --check web/app.js
```

测试覆盖五档合并、固定偏移、月末和闰年、保留跨度倒挂、无效 YAML 不覆盖有效配置、文件重载、重启持久化、受保护快照、创建失败不清理、单过期属性和只读模式等。真实命令写入测试使用隔离的假 ZFS 可执行文件，不访问物理池。

UI 自动化脚本为 `scripts/browser-smoke.cjs`，在独立 Demo 实例上验证数据集独立配置、公共策略引用与更新、复制后隔离、单数据集启停、就地快照浏览、备份配置和 YAML 保存。`scripts/inheritance-smoke.cjs` 额外验证快照继承；`scripts/backup-plan-smoke.cjs` 验证快照计划、默认折叠、叶子备份、目的路径继承、接收端期限和增量基点切换。浏览器验证工具仅用于开发，不是运行依赖。
