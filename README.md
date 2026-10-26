# sparq-tool 规划

`sparq-tool` 是 Sparq 的官方开发、构建、安装辅助工具。

它是客户端工具，不是运行时，不参与线上请求链路。

## 一、定位

`sparq-tool` 的定位是：

```text
Sparq DevOps Toolkit
```

负责：

```text
插件开发初始化
前端模板初始化
开发模式联调
本地环境检查
插件构建
插件打包
插件安装
插件更新
插件卸载
插件回滚
插件进程管理
app / plugin 重启适配
调用 marketplace / build-service / artifact API
```

不负责：

```text
购买逻辑
授权系统
远程构建队列
产物仓库
插件市场 UI
拖拽生成应用
多端编译
```

这些能力属于平台服务端或后续独立产品。

## 二、与平台服务端的边界

平台服务端负责：

```text
Marketplace API
License API
Build Service API
Artifact Registry API
购买记录
授权校验
远程构建任务
产物签名
产物存储
产物分发
插件市场页面
```

`sparq-tool` 负责：

```text
登录 Sparq Cloud 并保存用户级认证 profile
调用平台 API
创建绑定目标宿主的一次性 Install Intent
调用 Sparq 宿主管理 API
轮询并输出宿主安装操作状态
本地构建
开发态进程启动/停止/重启
输出开发/部署状态
```

Sparq 宿主负责线上 Artifact 下载与校验、ManagedProcess 托管、运行配置持久化、安装记录和更新失败恢复。`sparq-tool` 不直接写宿主数据库、托管插件目录或线上运行配置。

一句话：

```text
平台服务端负责商业和构建服务能力，sparq-tool 负责本地客户端执行。
```

### 2.1 代码边界

命令行实现位于 `cmd/internal`，负责 Cobra 命令树、flags、终端信号和用户输出；可复用实现位于顶层 `internal`，负责配置、构建、脚手架、插件生命周期、进程控制、文件监听和远程客户端。依赖方向固定为：

```text
cmd -> internal capabilities -> sparq-protocol / Go standard library
```

顶层 `internal` 不依赖 Cobra。完整目录和新增代码归属规则见 [代码架构](docs/architecture.md)。当前只提供 `sparq-tool` 二进制；服务端部署与本地开发的职责差异明确后，再决定是否需要独立部署工具。

## 三、命令状态与验证

本节是 `sparq-tool` 命令完成度的唯一状态清单。详细用法继续放在下方各命令说明中；状态发生变化时，应同时更新本表、对应测试和命令说明。

实现状态：

| 状态   | 含义                                        |
|------|-------------------------------------------|
| 已完成  | 当前定义范围内已经实现，不再输出预留提示。                     |
| 已接通  | 客户端已经接入 Website 或外部服务 API，但不表示完整业务闭环已经完成。 |
| 部分完成 | 只有部分模式、driver 或事务步骤可用，剩余范围在说明中列出。         |
| 预留   | 命令入口存在，但当前只输出“命令入口已保留”。                   |

验证状态：

| 状态       | 含义                                        |
|----------|-------------------------------------------|
| 自动化测试通过  | 对应命令或其核心能力已有测试，并已包含在 `go test ./...` 验证中。 |
| 线上只读联调通过 | 已对当前 Website 服务执行不会修改数据的真实 API 请求。        |
| 编译通过     | 命令已进入全量构建，但尚无对应行为测试。                      |
| 待联调      | 客户端或本地实现已经存在，但尚未完成真实服务或真实工作区验证。           |
| 待实现      | 当前没有可验证的真实业务行为。                           |

### 3.1 当前命令清单

| 命令                                                   | 实现状态 | 验证状态                           | 当前边界或下一步                                                           |
|------------------------------------------------------|------|--------------------------------|--------------------------------------------------------------------|
| `auth login/status/whoami/logout`                    | 已接通  | 自动化测试通过                        | 已接 Website 用户认证 API；生产账号流程和浏览器一次性授权待联调。                            |
| `plugin init`                                        | 已完成  | 自动化测试通过                        | Go、Remote Runner 和可选前端脚手架可用；纯前端和 Lua 插件未支持。                        |
| `plugin list/search/info`                            | 已接通  | 自动化测试通过；`plugin list` 线上只读联调通过 | 使用 `auth login` profile 访问 Marketplace；`search` 当前在类型过滤后的结果中本地匹配。       |
| `plugin build`                                       | 已完成  | 自动化测试和真实 CMS/Remote 插件构建通过      | 构建前端和可选 Remote Runner，运行 Go 测试，并导出完整 `release/manifest.json`。          |
| `plugin package`                                     | 已完成  | 自动化测试和真实 CMS 打包通过              | 校验并消费完整 Manifest，生成本地 package v1、release 元数据和 checksum。                 |
| `plugin publish`                                     | 已接通  | 自动化测试通过；待真实账号联调               | 上传审核源码；前端生成一个候选包，Remote Runner 按六个平台分别构建并上传。                         |
| `plugin download`                                    | 已接通  | 自动化测试通过；待联调                    | 仅下载并校验授权 Artifact，用于离线留存或检查，不是安装前置步骤。                              |
| `theme list/search/info`                             | 已接通  | 自动化测试通过；待联调                    | 使用 `auth login` profile 读取独立 Template 目录；`theme` 只表示宿主运行时操作名。  |
| `theme publish/download`                            | 已接通  | 真实 Studio 打包和命令级自动化测试通过；待真实账号联调 | 首次发布先完整校验主题包，再按当前账户唯一开发者主体自动创建主题应用；后续发布复用该应用。审核通过后 Website 将同一审核快照登记为 `template_source` Artifact。 |
| `theme install/update`                              | 已接通  | 自动化测试通过；待联调                    | 创建绑定宿主的 Install Grant，由 Sparq 下载、校验和安全解压；`--apply` 可安装后立即应用。       |
| `theme apply/remove/rollback`                       | 已接通  | 宿主客户端自动化测试通过；待联调              | 应用已安装版本、卸载未启用版本，或恢复应用当前主题前的主题版本。                                  |
| `manifest generate/validate`                         | 已完成  | 自动化测试通过                        | 支持生成、校验和 `--check`。                                                |
| `checksum create/verify`                             | 已完成  | 自动化测试通过                        | 当前提供 SHA-256 清单生成与校验。                                              |
| `signature keygen/sign/verify`                       | 已完成  | 自动化测试通过                        | 当前提供 Ed25519 文件签名能力。                                               |
| `build app`                                          | 已完成  | 核心能力自动化测试通过                    | 按 `build.yaml` 统一构建配置声明的 app、updater、Admin 和应用前端；未配置的组件自动跳过。 |
| `build app --config <build.yaml>`                    | 已完成  | 核心能力自动化测试通过                    | 按任意应用构建配置构建，不区分默认壳、Website 或二次开发应用。                   |
| `watch`                                              | 已完成  | 自动化测试通过                        | notify、poll、健康检查、进程替换和失败重启策略已有测试。                                  |
| `dev` / `dev plugin`                                 | 已完成  | 自动化测试通过                        | 插件联调入口；`dev` 是兼容写法。完整真实前后端会话仍需按插件逐项联调。                             |
| `dev app`                                            | 已实现 | 配置、进程退出、取消和前端超时回归通过；真实应用待联调 | 只做二次开发应用进程编排，支持用户级工具入口覆盖，不扩展为部署工具。                         |
| `doctor`                                             | 已完成  | 核心能力自动化测试通过                    | 检查本机工具链和插件清单；不同操作系统仍需分别验证。                                         |
| `local check/list/sync`                              | 已完成  | 自动化测试通过                        | 只处理 `go.work` 本地源码插件和工具托管的 Bundle 装配文件。                            |
| `restart app --driver command`                       | 已完成  | 核心能力自动化测试通过                    | 执行用户明确提供的 App 重启命令。                                               |
| `restart app --driver systemd/docker-compose`       | 预留   | 待实现                            | 当前只输出预留提示。                                                         |
| `artifact download`                                  | 已完成  | 自动化测试通过                        | 支持 HTTP 下载和本地复制，不替代授权 Artifact 解析。                                 |
| `artifact verify`                                    | 预留   | 待实现                            | 后续统一组合 checksum 与 signature 校验。                                    |
| `builder capsule keygen/create`                      | 已完成  | 自动化测试通过                        | 可生成密钥并创建加密签名胶囊。                                                    |
| `package app`                                        | 已完成  | 目录、tar.gz 和 zip 自动化测试通过         | 消费已有构建产物，生成包含基础配置、Admin、manifest 和 checksum 的首次安装包。          |

### 3.2 已完成的验证

截至 2026-08-11，当前版本完成过以下验证：

| 验证项                                                     | 结果         | 覆盖范围                                         |
|---------------------------------------------------------|------------|----------------------------------------------|
| 在 `sparq` 执行 `go test ./...`                            | 通过         | 安装记录、Remote 托管、主题安装、生命周期和 Admin API 回归。     |
| 在 `sparq-tool` 执行 `go test ./...`                       | 通过         | 全部 CLI 与顶层 `internal` 包。                     |
| 在 Website app 执行 `go test ./...`                        | 通过         | Marketplace 类型过滤和相关 Website 后端回归。            |
| Studio 主题双清单打包、multipart 发布请求和 Website 审核 Artifact 测试     | 通过         | `theme publish` 的版本同步、ZIP、SHA-256、上传协议，以及审核后 `template_source` 登记。 |
| 主题 Grant 安装和宿主生命周期测试                                  | 通过         | `theme download/install/update/apply/remove/rollback` 的本地核心能力。 |
| 构建临时 `sparq-tool` 并检查根命令、`plugin --help`、`theme --help` | 通过         | 命令树不再包含 `marketplace`，plugin/theme 命令面与本表一致。 |
| `plugin build ../plugins/sparq-plugins-remote-echo --target go` | 通过         | 真实插件 Go 测试和 Remote Runner 构建；验证后已清理生成的二进制。 |
| `plugin list --config <auth.json> --json`               | 通过，返回 `[]` | 使用已登录 profile 完成 Website 插件列表只读联调。             |
| `git diff --check`                                      | 通过         | 当前实现和文档不存在空白字符错误。                            |

“自动化测试通过”不代表已经完成真实账号、真实授权、真实 Artifact 或宿主安装事务联调。每完成一个真实流程，应在本节追加验证记录，并更新上方对应命令的验证状态。

### 3.3 开发初始化

在需要保存插件目录的位置执行初始化：

```bash
sparq-tool plugin init --code cms --path plugins/sparq-plugins-cms
```

已有 Go 插件增加前端时使用增量命令：

```bash
sparq-tool plugin add frontend --path plugins/sparq-plugins-cms
```

该命令在插件目录内生成独立 `frontend/` 工程，补充 Manifest 前端声明并重新生成 `manifest_gen.go`，不依赖 `sparq-frontend` workspace。

`--code` 必填。省略 `--path` 时，命令在当前目录创建与 code 同名的子目录，例如 `--code cms` 写入 `./cms`，前端工程位于该插件的 `frontend/` 下。显式 `--path` 可以指定其他相对或绝对目录；目标路径只要已经存在就会失败，不会向已有目录写入文件。

可以同时声明显示信息、运行时和前端初始化范围：

```bash
sparq-tool plugin init --code cms --path plugins/sparq-plugins-cms \
  --name "CMS" --module sparq.xin/sparq-plugins-cms --runtime both --with-frontend
```

默认 `--with go --runtime both`，生成最小中性脚手架：

```text
plugin.yaml                 # 只保存身份、介绍和协议兼容范围
go.mod
README.md
bootstrap.go                # 根包公开 PluginCode 和 Register
app/lifecycle/register.go   # 按依赖顺序装配插件能力
app/lifecycle/instance.go   # 插件 Start 和 Stop 生命周期
app/manifest/manifest.go    # Compose 声明组件、入口和宿主贡献
manifest_gen.go             # 确定性生成，直接调用 manifest.Compose
cmd/<plugin>/main.go        # 默认生成，启动 Remote HTTP 组件
frontend/                   # --with-frontend 时生成
sparq.dev.example.yaml      # HTTP runtime 的静态联调配置模板
```

初始化不会覆盖已有目标文件。脚手架不预设 Capability、Provider、Route、Controller 或示例业务；这些目录和声明应在插件出现真实能力后按目录规范添加。`--runtime http` 只表示后端组件可以通过 Remote HTTP 运行，因此额外生成 Runner 和静态 `sparq.dev.example.yaml`，不表示插件直接提供 HTTP Route。该配置模板不推导当前 workspace 路径，需按注释配置宿主、插件、密钥和可选前端后另存为 `sparq.dev.yaml`。前端只有统一的 `/plugins/{code}/` 根入口，逻辑页面后续通过 Manifest Entry 和前端模块自身区分。

Go+frontend 初始化不要求目标目录位于某个前端 workspace；前端工程随插件目录生成并可独立安装依赖、开发和构建。源码总仓可以自行配置 workspace 联调，但脚手架不会生成对该总仓的路径或 `workspace:*` 依赖。`--with go,frontend` 继续作为兼容写法。当前初始化器不支持纯前端或 Lua 插件；相应 Loader 和运行时边界接入后再提供独立初始化入口。

`plugin.yaml` 与 `app/manifest/manifest.go` 共同组成唯一 Manifest 来源：YAML 保持稳定的基础信息，Go `Compose` 按需加入 Runtime、Capability、Entry、Schema Resource 和其他宿主贡献。Local Bundle 与 Remote Runner 都使用生成后的同一个 `Manifest()`。生成器仍兼容已有插件的根 `manifest/` 目录。

### 3.4 独立文件监听

```bash
sparq-tool watch
sparq-tool watch --init
sparq-tool watch --file path/to/sparq.watch.yaml --dry-run
```

`watch` 是不依赖 Sparq 插件协议的通用开发命令。`--init` 排他创建标准配置，目标文件已经存在时直接报错，不覆盖用户命令和路径规则。配置文件中的相对路径先相对配置文件目录解析 `cwd`，监听目录再相对 `cwd` 解析。

```yaml
cwd: .

build:
  command: go build ./...

run:
  command: go run ./cmd/app
  env:
    APP_ENV: dev

watch:
  paths: [.]
  include: ["**/*.go", go.mod, go.sum]
  exclude: [.git, tmp, vendor, storage]
  exclude_globs: []
  mode: notify
  poll_interval: 500ms
  debounce: 300ms

stop:
  grace_timeout: 3s
  kill_timeout: 2s

restart:
  policy: never
  initial_delay: 500ms
  max_delay: 10s
  reset_after: 30s

# health:
#   url: http://127.0.0.1:8080
#   path: /healthz
#   timeout: 30s
```

`run.command` 必填，`build.command` 和 `health` 可省略。未配置构建命令时，文件变化会直接重启运行命令；配置普通 HTTP 健康检查后，新进程只有在接口返回 2xx 后才算就绪。日志使用 `build`、`run`、`watch`、`health` 阶段前缀，并在启动时打印解析后的监听规则、退出等待和重启策略。

`include` 支持 `*`、`**` 和 `?`，配置后取代兼容字段 `extensions/files`；`exclude_globs` 在包含规则之后排除文件。`notify` 是默认监听模式；网络文件系统无法稳定提供变更通知时可显式改为 `poll`，并通过 `poll_interval` 控制扫描间隔。工具不自动探测文件系统类型，也不递归目录符号链接；监听根目录本身是符号链接时会拒绝启动。

`restart.policy` 默认为 `never`，运行进程异常退出后等待下一次文件变化。设置为 `on-failure` 时按 `initial_delay` 到 `max_delay` 指数退避；进程稳定运行达到 `reset_after` 后恢复初始延迟。文件变化会取消等待中的失败重启并优先执行构建，构建失败时保留当前进程；没有当前进程时也不会跳过失败构建直接运行旧产物。

### 3.5 开发联调

~~~bash
sparq-tool dev
sparq-tool dev plugin
sparq-tool dev --file path/to/sparq.dev.yaml --dry-run
~~~

`dev` 和 `dev plugin` 是相同的插件联调编排器，保留无子命令入口用于兼容现有项目。配置文件中的相对路径以 `sparq.dev.yaml` 所在目录解析。执行顺序固定为：

~~~text
可选启动宿主 app
  -> 等待宿主健康检查
  -> 可选执行插件热更新首次构建检查
  -> 启动插件后端并注入 Remote Runner 环境变量
  -> 等待插件健康检查
  -> 启动前获取开发态 token
  -> 启动插件前端并注入 VITE 环境变量
  -> 宿主或前端退出、或 Ctrl+C 时停止本次启动的进程并撤销 token
~~~

配置示例：

~~~yaml
app:
  command: go run ./sparq-app/cmd/sparq
  cwd: .
  endpoint: http://127.0.0.1:19090
  dev_secret: local-sparq-development-token-secret-32-bytes
  health: /__healthz__
  timeout: 30s

plugin:
  id: cms
  component: content
  manifest: plugins/sparq-plugins-cms/plugin.yaml
  cwd: plugins/sparq-plugins-cms
  endpoint: http://127.0.0.1:19091
  health: /healthz
  command: go run ./cmd/cms
  secret: 0123456789abcdef0123456789abcdef
  reload:
    enabled: true
    paths:
      - .
      - ../../sparq-kernel
      - ../../sparq-protocol
    extensions: [.go]
    files: [go.mod, go.sum]
    exclude: [.git, frontend, storage]
    debounce: 300ms
    build: go build ./...
  timeout: 30s

frontend:
  enabled: true
  cwd: plugins/sparq-plugins-cms/frontend
  command: pnpm dev
  url: http://127.0.0.1:5177/plugins/cms
  health: /src/frontend.ts
  timeout: 15s
  env:
    BROWSER: none
~~~

plugin.manifest 省略时默认读取配置文件同目录下的 plugin.yaml，并校验清单 metadata.code 与 plugin.id 一致。`plugin.component` 表示当前命令启动的 Remote 组件；省略时工具会从完整 Manifest 中选择唯一支持 HTTP runtime 的 backend/worker，存在多个候选时必须显式配置。plugin.secret 至少 32 字节，工具只注入插件进程；宿主开发配置必须在 `dev.plugins.<plugin>` 中预先配置同一值。`app.dev_secret` 必须与宿主至少 32 字节的 `dev.token_secret` 相同，工具用它申请开发生命周期 session，并在启用前端时认证 token 颁发请求；生产环境禁止开启 `dev.enabled`。

支持 --manifest、--app-command、--plugin-command 和 --frontend-command 覆盖配置。前端会收到 VITE_SPARQ_HOST、VITE_SPARQ_PLUGIN_ID 和 VITE_SPARQ_DEV_TOKEN，插件后端会收到 SPARQ_APP_URL、SPARQ_PLUGIN_ID 以及按插件 code 生成的 SPARQ_<PLUGIN>_* 变量，其中包含解析后的 COMPONENT_ID。

当前工具负责进程编排、健康检查、开发态 token 生命周期和清理。插件目录存在 `frontend/src/frontend.ts` 时会自动启用前端，缺省使用 `frontend/`、`pnpm dev` 和端口 5175；显式 `frontend.*` 配置用于覆盖命令、目录或端口。启动前端前会通过宿主 `/dev/frontend` 注册 dev-only 的唯一 trusted root URL，不修改正式 Manifest。`frontend.entries` 已删除，iframe Sandbox Entry 不属于这项覆盖。root 的 Host API 和 contribution 归属来自宿主已安装的可信 Manifest。

宿主收到 `/dev/frontend` 登记后会根据已安装的可信 Manifest 校验 root 归属。工具会在 token 剩余有效期过半时重新登记覆盖，宿主收到有效登记后续租同一个 token，避免长时间运行时静默切换加载来源。token 撤销、心跳失败、过期和宿主进程退出都会清理覆盖或结束开发会话；root 加载失败时不会回退生产来源。

`plugin.reload.enabled=true` 时，`dev` 复用独立 `watch` 命令使用的 `internal/watchrun` 引擎，并在其上增加插件签名健康检查。工具会在启动插件前执行一次 `reload.build`。监听目录和构建命令都以 `plugin.cwd` 为工作基准；`paths` 可以显式包含 workspace 中的共享 Go module。文件变化会按 `debounce` 合并，构建失败时保留当前插件进程，构建成功后才向旧进程发送退出信号、启动新进程并重新执行签名健康检查。插件异常退出后宿主、前端和 watcher 继续运行，下一次匹配的文件变化会重新构建并启动插件；第一版不做无条件异常重启。前端源码由 Vite HMR 处理，不应加入插件后端监听目录。

二次开发应用使用独立的 `dev app` 模式，不需要插件 Manifest、插件 secret 或开发态 token：

```bash
sparq-tool dev app --file path/to/sparq.dev.yaml --dry-run
```

应用配置通过 `backend.watch_file` 引用后端监听配置，可选 `admin` 启动独立目录的 Admin Shell，`frontend` 启动业务前端。工具合并用户级工具链默认值，等待后端就绪后启动所有启用的前端；Shell 的 API 代理可由后端地址推导，业务入口仍只取 `frontend.url`。前端健康超时只输出警告，任一进程退出或 Ctrl+C 会清理本次启动的全部进程。用法见 [dev app 命令参考](docs/commands.md#dev-app) 和 [完整配置示例](sparq.dev.example.yaml)。默认壳实际配置位于 `sparq-app/sparq.dev.yaml`，Website 配置位于 `platforms/sparq-website/sparq.dev.yaml`。

### 3.6 环境检查

```bash
sparq-tool doctor
```

检查：

```text
Go 版本
Node 版本
pnpm / npm
Git
目标 GOOS / GOARCH
网络访问
插件目录权限
app 是否标准模板
本地插件 Bundle 装配文件是否由工具托管
```

### 3.7 构建与打包

默认 App 的官方版本可以在完成 Admin Shell 和 Runtime 构建后直接打包并上传：

```bash
sparq-tool auth login
SPARQ_RELEASE_SIGNING_KEY_ID=official-v1 \
SPARQ_RELEASE_SIGNING_PRIVATE_KEY=/secure/release-private.pem \
sparq-tool build app . --version 0.2.0 --upload
```

上传模式会构建 `linux`、`darwin`、`windows` 的 `amd64/arm64` 六个平台 Runtime 和 Updater，Admin `dist` 只构建一次。工具分别生成更新归档、计算 SHA-256、写入 `sparq.release.manifest.v1`，再使用 `auth login` 当前 profile 上传完整发行矩阵。更新日志固定读取 workspace root 的 `CHANGELOG.md`，签名密钥从受信构建环境变量读取，更新产物写入 `sparq-app/release/updates/<version>`。

`build app` 默认读取 `build.yaml`，也可通过 `--config` 和 `--env` 选择环境配置。所有产物写入配置的 `output.dir`，Admin 和应用前端分别发布到 `public/admin/`、`public/admin/__app__/`；运行文件和目录必须通过 `copy` 显式声明，未声明的 `config/` 文件不会自动发布。前端自动识别 pnpm/npm 并执行 `commands`（默认 `build`）。版本和 Git Hash 统一写入 Go Runtime、前端构建环境和 `build-manifest.json`。

单平台构建的 Runtime 和 Updater 使用配置中的原始名称；多平台构建会追加 `-<os>-<arch>`，例如 `sparq-linux-amd64`、`sparq-windows-amd64.exe`。安装包内部仍使用不带平台后缀的运行时名称。

首次安装包与在线更新包分开生成：

```bash
sparq-tool build app --config build.yaml --target linux/amd64 --version 0.2.0
sparq-tool package app . --target linux/amd64 --version 0.2.0
```

`package app` 默认在 Unix 平台生成 `tar.gz`，在 Windows 平台生成 `zip`；使用 `--archive none` 可以只生成解压目录。安装包包含 Runtime、Updater、基础配置、Admin、manifest 和 checksum，不包含任何运行数据、插件、主题或上传文件。

```bash
sparq-tool plugin build [path] --target all
sparq-tool plugin package [path]
sparq-tool plugin publish [path]
sparq-tool plugin publish [path] --bump patch
sparq-tool plugin download <code>
```

职责：

```text
构建插件后端二进制
构建插件前端资源
拷贝 plugin.yaml 及其引用文件
生成 release 目录
生成 release.json
生成 checksums.txt
后续生成签名信息
```

`plugin build` 默认按约定识别 Go module、`cmd/<code>` Remote Runner 和 `frontend/package.json`，也可通过插件根目录的 `sparq.build.yaml` 覆盖。Remote Runner 默认使用 `size` 优化，加入 `-trimpath` 和 `-ldflags "-s -w"` 移除本地路径、符号表和 DWARF 调试信息；需要调试构建时可显式设置 `go.optimization: debug`。它会在前端构建后重新生成 `manifest_gen.go`，临时调用插件根包 `Manifest()` 并原子写入 `release/manifest.json`。`plugin package` 优先消费该完整 Manifest，生成无签名的本地可安装 package v1，包内包含 `manifest.json`、`release.json` 和 `checksums.txt`。

`plugin publish` 先检查当前 Auth Profile，再在终端中显示当前版本，并让开发者选择保持版本、递增 Patch/Minor/Major 或输入自定义 SemVer；回车默认递增 Patch。认证无效时不会修改版本或启动构建。目标版本先写入 `$SPARQ_HOME/sparq-tool/publish-state/`，再原子更新 `plugin.yaml` 并重新生成 `manifest_gen.go`。纯前端插件只生成一个 `frontend_bundle`；存在 Remote Runner 时，为六个平台生成同时包含前后端的 `managed_remote` 组合包。没有前端和 Remote Runner 的插件保留一个 `plugin_package` 兼容候选包。脚本和 CI 必须显式使用 `--bump patch|minor|major` 或 `--version <semver>`，两个参数互斥；需要沿用当前版本时将当前版本传给 `--version`。

版本写入后，即使构建、打包或上传失败也不会自动回滚；再次对同一目录、endpoint 和 profile 执行命令会恢复原目标版本、查询服务器已有候选并补传缺失项，不会重复 bump。源码和候选全部上传后，CLI 调用 finalize 原子冻结清单；只有 finalize 成功才进入审核并删除本地恢复状态。命令不会创建 Git commit 或 tag。平台开发者主体上传的可交付候选包进入 `internal_review`，其他开发者主体返回 `rebuild_required`。

```yaml
version: 1

go:
  enabled: true
  test_packages: ["./..."]
  remote_entry: ./cmd/cms
  output: release/bin
  optimization: size

frontend:
  enabled: true
  cwd: frontend
  package_manager: pnpm
  script: build
  output: dist
```

没有配置文件时，有 `go.mod` 就运行 Go 阶段，有 `cmd/<code>` 才构建 Remote Runner，有 `frontend/package.json` 就运行前端阶段。`go.optimization` 只接受 `size` 或 `debug`，省略时为 `size`。前端依赖必须事先安装，发布流程不会自动联网安装依赖；构建结束后 `frontend/dist` 必须存在且非空。`--target frontend` 或 `--target go` 显式请求不存在的阶段时会报错，`--target all` 会跳过不存在的阶段。

产物示例：

```text
release/
  manifest.json
  cms-0.1.0.tar.gz
```

压缩包解开后：

```text
.
  plugin.yaml
  manifest.json
  release.json
  bin/
    cms
  frontend/
    dist/
  checksums.txt
  signatures/        # 后续接入签名后生成
    release.ed25519
```

### 3.8 插件部署边界

`sparq-tool` 不负责生产插件安装、更新、卸载、启停或重启，也不保存宿主 Session、CSRF 或部署授权。开发者完成构建和发布后，由运行中的 Sparq Admin 或本地宿主命令管理部署：

```bash
sparq plugin status
sparq plugin enable <code>
sparq plugin disable <code>
sparq plugin start <code>
sparq plugin stop <code>
sparq plugin restart <code>
```

宿主负责下载授权 Artifact、事务更新、Unix Socket 进程管理、生命周期 revision、权限和审计。`enable/disable` 修改持久期望状态，`start/stop/restart` 只管理 Managed Runtime 当前实例。`sparq plugin` 连接运行中的 `run/sparq.sock` 执行本机控制；Windows 或不支持 Unix Socket 时使用包含随机控制令牌的 loopback TCP endpoint 文件。恢复旧版本时重新下载目标 Artifact，不从 `sparq-tool` 或插件目录读取历史版本。

### 3.10 主题交付与安装

主题目录必须包含两份职责不同的清单：`template.yaml` 是 Website 上架与兼容性清单，`theme.yaml` 保存 Sparq 运行时主题身份、外观和终端能力。二者的 code 和 version 必须一致。

```text
template.yaml
theme.yaml
cover.png            # 可选
views/
assets/
```

```yaml
# template.yaml
apiVersion: sparq.io/v1
kind: Template
metadata:
  id: studio
  code: studio
  version: 1.0.0
sparq:
  protocol: ">=0.1.0 <0.2.0"
```

```yaml
# theme.yaml
code: studio
name: Studio
version: 1.0.0
appearance:
  colorSchemes: [light, dark]
  palette:
    primary: "#2563EB"
    accent: "#F97316"
formFactors: [desktop, tablet, mobile]
```

`formFactors` 只能使用 `desktop`、`tablet`、`mobile`，`colorSchemes` 只能使用 `light`、`dark`，且 `palette.primary` 必须是 `#RRGGBB`。`theme.yaml` 不维护业务视图映射；业务 Resolver 返回稳定模板标识，宿主从当前主题的 `views/` 解析同名文件。Website 扫描并保存包内模板标识及终端、色彩能力等技术事实。主题分类、适用行业和风格标签属于平台运营资料，不写入主题包。

发布与独立下载：

```bash
sparq-tool theme publish ./my-theme -m "首次发布"
sparq-tool theme download studio --version latest --output ./downloads/
```

`theme publish` 与插件发布使用相同的版本选择和中断恢复方式：交互终端默认递增 Patch，也支持 `--bump patch|minor|major`、`--version <semver>` 和 `--keep-version`。版本变化时同时更新 `template.yaml` 与 `theme.yaml`，随后按“准备发布版本、校验并打包主题、创建或恢复源码会话、完成提交并进入审核”四个阶段输出进度。

`./my-theme` 表示自行准备的主题源码目录，须包含上述双清单、`views/` 和 `assets/`；以下下载和宿主管理示例假定主题 code 为 `studio`。

命令校验两份清单后，只打包运行期需要的封面、模板和静态资源，生成确定性 ZIP 和 SHA-256，再提交平台审核。首次发布会在完整校验主题包后，按当前账户所属的唯一开发者主体自动创建主题应用；code 已存在时必须仍属于该主体且内容类型为主题。Website 只接受这种可直接安装的主题 ZIP，并把扫描得到的模板标识、支持终端、色彩模式和色板保存到该源码版本的清单快照；审核通过时直接把审核过的同一份字节登记为 `template_source`，不再经过构建或二次转换。后台另外维护主题分类、适用行业和风格标签，其中分类和至少一个行业是上架条件，但不会阻塞首次发布。自动化测试在临时目录创建主题，验证打包与上传请求；真实账号的上传、后台审核、发布和下载需在运行中的 Website 上逐项验证。

宿主安装与生命周期：

```bash
sparq-tool theme install studio --version latest --host-url http://127.0.0.1:19090
sparq-tool theme update studio --version 1.1.0 --apply --host-url http://127.0.0.1:19090
sparq-tool theme apply studio 1.1.0 --host-url http://127.0.0.1:19090
sparq-tool theme remove studio 1.0.0 --host-url http://127.0.0.1:19090
sparq-tool theme rollback studio --host-url http://127.0.0.1:19090
```

安装和更新默认不切换公开站点主题；显式传入 `--apply` 或之后执行 `theme apply` 才会切换。应用动作保存切换前的主题 code/version，供 `theme rollback` 恢复。当前启用主题和受保护主题不能卸载。

### 3.11 重启

```bash
sparq-tool restart app
```

已实现的 restart driver：

```text
command
```

`command` 要求调用方通过 `--command` 明确提供实际重启命令。`systemd` 和 `docker-compose` 当前只保留入口并输出预留提示。

暂不支持：

```text
Kubernetes
远程 SSH
蓝绿发布
滚动发布
```

## 四、插件使用方式

### 4.1 Remote Service Mode

默认推荐模式。

流程：

```text
用户购买/获取插件
  ↓
sparq-tool 下载插件二进制包或源码包
  ↓
解压/编译插件服务
  ↓
启动插件进程
  ↓
插件主动注册到 app
  ↓
app health check
  ↓
插件可用
```

优点：

```text
宿主无需重新编译
支持运行时安装
商业插件不暴露源码
失败影响面小
可独立升级；更新失败可恢复本次事务前的包
```

### 4.2 Local Bundle Mode

源码和宿主一起编译，最终一个二进制。

流程：

```text
用户购买/获取插件
  ↓
拉取宿主源码和插件源码
  ↓
生成本地插件 Bundle 装配文件
  ↓
重新编译 app
  ↓
替换二进制
  ↓
重启 app
```

构建方式：

```text
1. 平台服务端远程编译后下载产物
2. 用户本地源码编译
```

本地源码编译需要 `sparq-tool doctor` 检查环境。

## 五、后续完善顺序

后续开发按真实业务闭环推进，不再按已经过期的 v0.x 命令清单判断完成度：

1. 对插件安装、更新、卸载和回滚执行 Website + Sparq 真实环境联调，并记录 License、Artifact 和失败恢复结果。
2. 对插件状态、启停和 restart 执行 Website + Sparq 真实环境联调，并记录并发 revision 和失败恢复结果。
3. 对主题上传、审核、授权下载、安装、应用、卸载和回滚执行 Website + Sparq 真实环境联调。
4. 补齐 Artifact 签名验证：实现 `artifact verify`，统一校验签名、可信公钥和目标兼容性。
5. 独立平台构建服务稳定 Build Session、密钥重包装和构建证明；不在 `sparq-tool` 中引入第二套平台凭据。
6. 增加真实运行环境验证：分别覆盖 Linux、macOS、Windows，以及后续 systemd 和 docker-compose driver。

每完成一步，必须先补自动化测试，再在“已完成的验证”中记录真实联调命令、环境和结果，最后更新命令状态表。

## 六、代码结构

```text
sparq-tool/
  cmd/sparq-tool/            # 二进制入口
  cmd/internal/<command>/    # Cobra 命令、flags 和终端输出
  internal/                  # 可复用能力，不依赖 Cobra
  docs/                      # 详细命令、架构和专项流程文档
```

命令层只负责解析输入和输出结果。文件操作、构建、插件生命周期、进程控制和远程客户端必须放在顶层 `internal`；Website API 的商业规则、授权判断和 Artifact 管理不进入工具链。

## 七、当前约束

- CLI 不再提供 `marketplace` 一级命令，线上资源按 `plugin` 和 `theme` 组织。
- Website 内部仍保留 Marketplace 模块；CLI 的 `theme` 暂时映射到 API 的 `template` 类型。
- 主题上传包必须是同时包含 `template.yaml` 和 `theme.yaml` 的可安装 ZIP；主题不进入构建器流程。
- `plugin publish/download` 是独立的发布申请或产物获取命令，不负责部署当前宿主。
- Local 源码 Artifact 当前只返回 `rebuild_required`，不会在运行中的宿主内构建或替换自身二进制。
- `dev app` 只用于本地进程编排，暂不扩展为部署、环境管理或复杂 DevOps 平台。
- 当前不实现市场购买、Kubernetes 部署、远程 SSH、蓝绿发布或滚动发布。

## 单仓构建与发布

当前目录可独立建立 Git 仓库，Go module 路径保持不变。`make test` 和 `make build` 可用于本地开发；`make check-release` 始终使用 `GOWORK=off` 验证正式依赖、测试、静态检查和构建。

```sh
make check-release
make release VERSION=0.1.0
```

依赖中的 `v0.0.0` 是尚未发布的占位版本，需要在依赖仓库发布真实 tag 后执行 `GOWORK=off go get <module>@<version>` 更新 go.mod/go.sum。本地 replace 不允许进入正式发布。源码库的 release 仅执行发布门禁，之后由维护者提交并创建版本 tag；sparq-tool 额外生成包含 CLI 与 Updater 的平台归档及 SHA256。
