# sparq-tool 命令说明

## 目标

`sparq-tool` 当前阶段优先完成命令入口和稳定的基础能力，不急于实现依赖尚未稳定的业务闭环。

命令入口集中在 `cmd/internal`，实际执行能力位于顶层 `internal`。命令名、flags 和输出属于 CLI 契约；配置解析、文件操作、构建和进程控制应通过能力包复用。详细边界见 [代码架构](architecture.md)。

命令分为三类：

```text
基础可用：已经有明确本地实现，可直接执行。
简化实现：已有基础流程，但完整业务还需要后续补齐。
预留入口：命令结构已确定，当前只输出提示，具体业务待协议稳定后实现。
```

## 当前命令树

```text
sparq-tool
  artifact
    download
    verify
  auth
    login
    status
    whoami
    logout
  build
    app
  builder
    capsule
      create
      keygen
    request
    status
    download
  checksum
    create
    verify
  dev
  doctor
  install
    plugin
  local
    check
    list
    sync
  manifest
    generate
    validate
  package
    app
  plugin
    init
    add
      frontend
    download
    build
    package
    publish
    list
    search
    info
    status
    enable
    disable
  remove
    plugin
  restart
    app
    plugin
  rollback
    plugin
  signature
    keygen
    sign
    verify
  theme
    list
    search
    info
  update
    plugin
  watch
```

## 基础可用命令

### plugin init

```bash
sparq-tool plugin init --code catalog
sparq-tool plugin init --code catalog --path plugins/sparq-plugins-catalog
```

`--code` 必填。未设置 `--path` 时，命令在当前目录创建 `./<code>`，前端工程位于该插件的 `frontend/` 下。设置 `--path` 后使用指定的相对或绝对目录。目标路径只要已经存在就会报错，不会复用空目录，也不会覆盖已有内容。

默认使用 `--with go --runtime both`。可选参数包括 `--name`、`--module`、`--with-frontend` 和 `--runtime local|http|local,http|both`。`--with go,frontend` 继续作为兼容写法。

最小 Go 脚手架包含：

```text
plugin.yaml
go.mod
README.md
bootstrap.go
app/lifecycle/register.go
app/lifecycle/instance.go
app/manifest/manifest.go
manifest_gen.go
cmd/<plugin>/main.go       # 默认生成；显式 local 时不生成
sparq.dev.example.yaml     # HTTP runtime 的静态联调配置模板
```

`plugin.yaml` 只保存基础应用信息；`metadata.authors` 表示插件发布者或维护者，应由插件开发者显式填写，不与执行安装的宿主用户绑定。组件、Runtime、Capability、Entry 和 Contribution 在 `app/manifest/manifest.go` 的 `Compose` 中按需声明。脚手架只创建协议要求的生命周期实例，不预设 Capability、Provider、Route 或 Controller；出现真实能力后再创建对应目录并补充 Manifest。`http` Runtime 表示后端组件可以通过 Remote HTTP 运行，不等于插件直接提供 HTTP Route。`sparq.dev.example.yaml` 是固定模板，不包含初始化所在 workspace 的路径；按逐字段注释修改后另存为 `sparq.dev.yaml`，再交给 `sparq-tool dev` 使用。

`--with-frontend` 还会生成 Vue/Vite 工程。开发服务器使用 `/plugins/<code>/` 入口；生产构建使用相对 `base`，使入口、路由级 chunk 和延迟加载样式能够由宿主发布到任意 `/{admin_path}/plugin-assets/<code>@<version>/` 目录：

### plugin add frontend

已有 Go 插件需要增加前端时，使用增量命令，不要再次执行 `plugin init`：

```bash
sparq-tool plugin add frontend --path plugins/sparq-plugins-catalog
```

命令会在插件目录内生成独立的 `frontend/` 工程、补充 Manifest 前端组件声明，并重新生成 `manifest_gen.go`。前端工程使用已发布的 `@sparqjs/plugin-sdk` 版本，不依赖 `sparq-frontend` workspace。目标前端目录非空、Manifest 已存在自定义前端声明或无法安全定位 `Compose` 时，命令会拒绝修改并提示手工处理。

```text
frontend/package.json
frontend/tsconfig.json
frontend/vite.config.ts
frontend/README.md
frontend/index.html
frontend/src/App.vue
frontend/src/main.ts
frontend/src/bootstrap.ts
frontend/src/style.scss
```

前端工程是插件目录的一部分，不要求存在独立的 `sparq-frontend` workspace。模板使用已发布的 `@sparqjs/plugin-sdk` 和独立 Vite/TypeScript 配置；放入主仓库 `sparq-frontend` workspace 仅用于联调时复用源码和包管理。当前不支持纯前端和 Lua 初始化。前端页面通过 `@sparqjs/plugin-sdk` 获取宿主上下文，模板不写入 Host Binding、开发服务器地址或示例业务 API。

在插件目录中安装依赖、开发和构建生成的插件前端：

```bash
cd <plugin>/frontend
pnpm install
pnpm dev
pnpm build
```

`build` 只输出插件自身的 `dist/`，不会写入 Admin Shell 的公共目录。Element Plus 等组件的日期选择器和 Tooltip 弹层应保留在插件 iframe 文档内；需要显式容器时使用 SDK 创建的 `overlayContainer`。

### manifest validate

```bash
sparq-tool manifest validate [plugin.yaml]
```

用于校验插件清单基础字段、嵌套 contribution 和静态引用，包括：

```text
apiVersion
kind
metadata.id
metadata.code
metadata.name
metadata.version
sparq.protocol
spec.runtime.supported
spec.entries
spec.capabilities
spec.contributions.version
spec.contributions.features
spec.contributions.schema_resources
spec.contributions.menus
```

`spec.contributions.version` 是插件 Contribution repository 使用的单调正整数，不使用 `metadata.version` 的 semver 字符串替代。`features` 按功能场景组织权限动作，`menus` 是扁平数组；菜单中的 `entry_code` 必须引用当前清单 `spec.entries[].name`，`parent_code` 只能引用当前清单声明的 `dir`。页面菜单必须通过 `feature_code + entry_permission_action` 引用 `usage_role: entry` 的权限，同一 Feature 的多个菜单必须共用一个进入权限。`dashboard-widget` 和 `field-editor` capability 必须引用 `component` entry，`admin-page` capability 不能引用 `component` entry。Feature、菜单项和 capability 必须使用稳定 code，每个权限动作必须声明 `usage_role` 和 `data_scope_policy`。

Schema Resource 不写入 YAML，必须使用插件内 JSON 文件：

```yaml
spec:
  contributions:
    schema_resources:
      - file: schema/cms.tag.schema.json
```

外部文件保存完整的单个资源贡献：

```json
{
  "resource": "cms.tag",
  "table_name": "cms_tags",
  "schema": {
    "name": "tag",
    "title": "标签",
    "fields": []
  },
  "action_permissions": {
    "list": "cms.tag.list"
  }
}
```

`file` 必须使用相对插件目录的正斜杠路径并以 `.json` 结尾，不能离开插件目录或通过符号链接读取目录外文件，也不能和内联资源字段混用。工具严格校验外部文件顶层字段，合并全部资源后再校验权限和菜单引用。

清单的 plugin owner 固定来自 `metadata.code`。Realm、Application 和菜单归属由 Manifest 与宿主注册流程确定，不能通过独立宿主配置文件覆盖。因此 `manifest validate` 只校验插件自己的声明，不校验某个宿主是否已经存在对应目标。

### manifest generate

```bash
sparq-tool manifest generate [plugin.yaml]
sparq-tool manifest generate plugin.yaml --check
```

`plugin.yaml` 是插件 Manifest 主清单；它和 `schema_resources[].file` 引用的 JSON 共同组成唯一源文件集合。命令默认在插件根 package 生成 `manifest_gen.go`，也可通过 `--output` 和 `--package` 显式指定输出与 package。生成文件嵌入合并后的确定性 JSON 并提供 `Manifest()`；Local Bundle 与 Remote main 都调用这个函数，不能再维护手写的第二份 Manifest。

`--check` 不写文件，只检查生成文件是否存在且与当前主清单和全部引用文件一致，适合 CI：

```bash
sparq-tool manifest generate plugin.yaml --check
```

### checksum create / verify

```bash
sparq-tool checksum create <file|dir>... --output checksums.txt
sparq-tool checksum verify checksums.txt
```

用于生成和校验 sha256 清单。

### signature keygen / sign / verify

```bash
sparq-tool signature keygen
sparq-tool signature sign <file>
sparq-tool signature verify <file>
```

当前使用 `ed25519`，用于插件产物签名和校验。

### builder capsule keygen / create

```bash
sparq-tool builder capsule keygen --output-dir ./builder-keys

sparq-tool builder capsule create \
  --source ./cms-1.2.3-source.tar.gz \
  --app-code cms \
  --version 1.2.3 \
  --expected-source-sha256 814f...e992 \
  --builder-environment sparq-builder:v1@sha256:6ca1...96f0 \
  --encryption-key-id capsule-encryption-v1 \
  --encryption-public-key ./builder-keys/capsule-encryption-public.pem \
  --signing-key-id marketplace-signing-v1 \
  --signing-private-key ./builder-keys/artifact-signing-private.pem \
  --output ./cms-1.2.3-local.sparq
```

`keygen` 生成 RSA-3072 胶囊加密密钥和 Ed25519 Artifact 签名密钥，拒绝覆盖已有文件。`create` 只接受人工审核通过的源码包及其预期 SHA-256；实际摘要不一致时不会生成胶囊。源码最大 100 MiB，胶囊最大 128 MiB，输出文件同样拒绝覆盖。

该命令属于独立构建器边界，不是开发者上传入口。生产私钥应由 KMS、HSM 或等价受控密钥服务持有，不能通过普通开发者 CLI 分发。完整格式和登记边界见 [Local 加密构建胶囊](plugin-local-build-capsule.md)。

`sparq-tool builder` 只生成离线胶囊，不连接 Sparq Cloud，也不接受 endpoint 或 Service Token。可信构建器向平台登记 Artifact 属于独立平台构建服务，不是用户 CLI 能力。

### artifact download

```bash
sparq-tool artifact download <source>
```

这是不处理 Marketplace 授权、版本和 Artifact 选择的低层文件传输命令。下载市场插件应使用 `plugin download`。

支持：

```text
HTTP(S) 产物下载
本地文件复制
Bearer token 注入
```

### auth login / status / whoami / logout

```bash
sparq-tool auth login
sparq-tool auth login -u root
sparq-tool auth status
sparq-tool auth whoami
sparq-tool auth logout
```

认证属于 Sparq Cloud 账号，不属于某一个插件市场。`auth login` 默认连接 `https://sparq.xin`，依次提示登录账号并隐藏读取密码；`-u` 是 `--name` 的短参数，因此本地 `st` 指向 `sparq-tool` 时，`st auth login -u root` 会继续交互读取密码。本地开发或私有部署可以通过 `--url` 覆盖地址。`--type` 默认为 `auto`：含 `@` 的账号按邮箱登录，7 至 15 位数字（可带 `+` 前缀）按手机号登录，其他输入按用户名登录；存在歧义时可显式指定 `email`、`username` 或 `mobile`。非交互环境必须显式传入账号，并让标准输入只承载密码：

```bash
sparq-tool auth login --name developer@example.com --password-stdin
```

`auth login` 调用 `POST /cloud-api/usercenter/v1/sessions`。`status` 和 `whoami` 会调用 `/me` 验证服务端 Session，`logout` 会先调用
`DELETE /cloud-api/usercenter/v1/session` 撤销远端 Session，再删除本地凭据。凭据按 profile 保存在
`~/.sparq/sparq-tool/auth.json`，只包含 endpoint、Session Token、过期时间和账号摘要，不保存密码，也不会写入插件目录。`SPARQ_HOME` 是 Sparq 产品体系统一的用户级数据根目录，可以覆盖默认的 `~/.sparq`；sparq-tool 的用户级持久化路径统一从该根目录派生。`--config` 的优先级高于 `SPARQ_HOME`；`--local` 可在远端不可达时仅删除本地凭据。

### local check

```bash
sparq-tool local check [workspace-root]
```

用于检查当前 app 接入本地源码插件所需的基础结构：

```text
go.work
sparq-app/go.mod
sparq-app/cmd/sparq/main.go
plugins/
```

`sparq-app/cmd/sparq/plugins_gen.go` 当前作为提示项，不作为阻塞失败。

### local list

```bash
sparq-tool local list [workspace-root]
```

读取 `go.work` 的 `use` 项，识别其中包含 `plugin.yaml` 且支持 `local` runtime 的插件源码目录，并输出插件 code、版本、Go module 和源码路径。

### local sync

```bash
sparq-tool local sync [workspace-root]
```

读取 `go.work` 的 `use` 项，识别其中包含 `plugin.yaml` 且支持 `local` runtime 的插件源码目录。命令先检查每个 Local 插件的 `manifest_gen.go` 未过期，再生成 `sparq-app/cmd/sparq/plugins_gen.go`。生成文件显式导入插件根 package，并把插件 `Manifest()` 与 `Register` 组成 `plugin.PluginPackage`。Manifest 包含插件名称、版本、说明、作者、包级 `depends_on`、runtime、前端入口、前端 capability、权限 contribution、Schema Resource、贡献目标和菜单 contribution；Local 与 Remote 使用插件根 package 返回的同一个完整协议对象。

默认壳通过统一 Reconciler 从唯一 PluginRegistry 的完整快照同步 Builtin、Local 和 Remote 插件状态。Event Consumer 与 Contribution 都按快照对账；Contribution 在首个有效实例出现时 Enable，Manifest 升级时 Upgrade，最后一个有效实例消失时 Missing。

插件不需要 `/local` 适配 package，也不通过自身 `init()` 修改全局目录。生成文件只声明当前 App 的 Package 列表，入口通过 `sparq.WithPluginPackages` 显式传入。PluginHost 创建 Registration Draft、调用共用的 `Register`，并接管必需的 `plugin.Instance`；Provider、Route 和 Event Consumer 作为一个完整 Registration 原子提交。官方插件没有公开发布版本，本地源码模块由 `go.work use` 绑定；不要在 `sparq-app/go.mod` 中为它们添加无法解析的 `v0.0.0` 依赖。当前 `local sync` 只读取 `go.work`，不自动执行 `go work use`、`go work edit`、`go mod tidy` 或 `go work sync`。

### build app

`build app` 统一按构建配置执行，不区分默认壳、Website 或其他二次开发应用。配置中的 `app` 必须存在，`updater`、`admin`、`frontend` 只有设置 `path` 时才参与构建。

```bash
sparq-tool build app [workspace-root]
```

默认配置文件为工作区根目录的 `build.yaml`，也可以通过 `--config` 指定其他文件，`--env dev` 会选择同目录的 `build.dev.yaml`。所有构建产物都写入配置的 `output.dir`，目录固定为 `sparq`、可选 `sparq-updater`、`public/admin`、`public/admin/__app__` 和 `config`；`copy` 中声明的应用运行资源目录会发布到输出目录；已有配置不会被覆盖。

Admin Shell 发布时先写入构建资源，最后原子替换 `index.html`。已有哈希资源会保留，避免正在运行的旧页面丢失延迟加载文件；运行期的 `plugin-assets` 不会被覆盖或清理。历史构建遗留的 `platforms`、`plugins` 目录会在发布前删除，构建产物如果重新包含这些二次开发目录、`plugin-assets`、符号链接或非普通文件，发布会直接失败。

```text
sparq-admin/dist
  -> <output>/public/admin
sparq-frontend/packages/public/captcha-runtime
  -> <output>/public/assets/captcha
  -> <output>/sparq
  -> <output>/sparq-updater
```

单平台构建的 Runtime 和 Updater 使用配置中的原始名称；多平台构建会追加 `-<os>-<arch>`，例如 `sparq-linux-amd64`、`sparq-windows-amd64.exe`。安装包内部仍使用不带平台后缀的运行时名称。

可选参数：

- `--target`：指定 `os/arch`，可以重复传入；默认当前平台，`--target all` 构建六个正式支持的平台。Admin 与平台无关，只构建一次。
- `--version`：写入 Runtime 版本和默认官方更新策略，默认 `dev`。
- `--tags`：传给 Go 组件的构建标签，默认 `production`，包含 MySQL 和 PostgreSQL，排除 SQLite；`--tags ""` 显式构建开发版本。自定义生产标签使用 `--tags production,custom`。
- `--debug`：保留 Go 调试符号、关闭优化与内联，并让 Admin 关闭压缩、生成 source map。默认构建使用 `-trimpath` 和 `-s -w -buildid=` 缩小二进制。
- `--upload`：根据配置生成正式更新发行矩阵并上传 Website，要求配置声明完整的 Runtime、Updater 和 Admin。

普通构建默认生成当前 `GOOS/GOARCH` 的配置组件。使用 `--target all` 或 `--upload` 时会构建 `linux/darwin/windows` 的 `amd64/arm64` 六个平台，通过当前 auth profile 完成发布。更新日志固定读取构建配置目录的 `CHANGELOG.md`，签名环境读取 `SPARQ_RELEASE_SIGNING_KEY_ID` 和 `SPARQ_RELEASE_SIGNING_PRIVATE_KEY`；更新发行产物写入构建配置目录的 `release/updates/<version>`。

### build app --config

```bash
sparq-tool build app --config path/to/build.yaml
sparq-tool build app --config path/to/build.yaml --env release \
  --go-command /opt/go/bin/go --go-arg=-mod=vendor
```

`app`、`updater`、`admin`、`frontend`、`copy` 和 `toolchain` 均从配置读取，路径以配置文件目录为基准解析。`copy.source` 相对于 `app.path`，`copy.target` 相对于 `output.dir`，只允许输出目录内的相对路径。前端组件执行配置的 `commands`（默认 `build`），自动识别 pnpm/npm 并要求生成非空 `dist`，随后发布到固定的 `public/admin` 或 `public/admin/__app__`。`--version`、`--git-hash` 优先于配置值；Git Hash 默认从配置目录的 Git 仓库读取，并写入 `build-manifest.json`。完整字段见仓库根目录 `build.yaml`。

### plugin build

```bash
sparq-tool plugin build [path] --target all|go|frontend
```

读取并校验 `plugin.yaml`，先构建前端，再重新生成并校验 `manifest_gen.go`，最后按目标执行 Go 阶段。`all`/`go` 目标会生成并校验 `release/manifest.json`：有 Go module 时工具临时调用根包 `Manifest()`，无 Go module 的声明式插件则从 YAML 生成协议清单。临时导出器会在命令结束后删除。

```text
all      自动执行存在的 Go 和前端阶段
go       执行 go test，再构建存在的 cmd/<code> Remote Runner
frontend 执行 package script，并要求 frontend/dist 非空
```

插件根目录可以使用 `sparq.build.yaml` 覆盖默认约定：

```yaml
version: 1
go:
  enabled: true
  test_packages: ["./..."]
  test_tags: ""
  tags: production
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

没有配置时，有 `go.mod` 就启用 Go 阶段，有 `cmd/<code>` 才构建 Remote Runner，有 `frontend/package.json` 就启用前端阶段。`go.test_tags` 只传给 `go test`，默认不加标签，保留 SQLite、MySQL 和 PostgreSQL 测试支持。`go.tags` 传给 Remote Runner 及跨平台发布构建，默认 `production`；本地调试可显式设置 `tags: ""`，生产扩展标签使用 `tags: production,custom`。`production` 与运行环境、版本号及优化选项独立，旧的数据库专用标签已移除。`go.optimization` 默认是 `size`，会为 Remote Runner 加入 `-trimpath` 和 `-ldflags "-s -w"`；设置为 `debug` 时保留 Go 默认调试信息。其他值会在构建前被拒绝。包管理器只支持 `pnpm` 和 `npm`，按显式配置、`packageManager` 字段或锁文件判断；工具不会自动安装依赖。

### doctor

```bash
sparq-tool doctor
```

检查：

```text
Go
Node
npm / pnpm
Git
目标平台
工作区
插件清单
```

### plugin / theme list、search、info

```bash
sparq-tool plugin list
sparq-tool plugin search [keyword]
sparq-tool plugin info <code>
sparq-tool theme list
sparq-tool theme search [keyword]
sparq-tool theme info <code>
```

这些命令使用 `auth login` 保存的 profile 访问 Cloud Marketplace，endpoint 和 Bearer token 均来自 profile。插件命令读取 `GET /cloud-api/v1/applications` 和对应详情，并只展示 `delivery_mode=sparq_plugin` 的 Application；主题命令读取 `GET /cloud-api/v1/templates` 和对应详情。`search` 在各自目录中按 code、名称和简介过滤。命令支持 `--config`、`--profile`、`--timeout` 和 `--json`，不接受独立平台 URL。CLI 对外保留 `theme`，因为它表达宿主主题操作，不代表服务端继续把 Template 建模为 App 类型。

### watch

```bash
sparq-tool watch
sparq-tool watch --init
sparq-tool watch --file sparq.watch.yaml --dry-run
```

独立监听任意开发进程，不依赖 Manifest、插件 token 或 `sparq-protocol`。`--init` 生成标准配置，已有目标文件不会被覆盖。配置示例：

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

`run.command` 必填，`build.command` 和 `health` 可选。配置文件目录是 `cwd` 的解析基准，`watch.paths` 再相对 `cwd` 解析；`--dry-run` 校验配置并打印实际命令、监听规则、退出等待、重启策略和健康检查目标。

`include` 支持 `*`、`**`、`?`，存在时取代兼容字段 `extensions/files`；`exclude_globs` 用于排除已经命中的文件。默认 `mode: notify` 使用文件系统通知，网络文件系统可以显式选择 `mode: poll`，工具不执行不可靠的自动探测。目录符号链接不会递归，符号链接根目录会被拒绝。

构建失败时保留当前运行进程；没有当前进程时等待下一次文件变化，不跳过构建运行旧产物。`restart.policy: never` 在异常退出后等待文件变化，`on-failure` 按配置执行指数退避；任何文件变化都会取消等待中的失败重启并优先重新构建。构建和运行进程先等待 `stop.grace_timeout` 优雅退出，仍未结束时清理进程树并等待 `stop.kill_timeout`。

## 简化实现命令

### dev

~~~bash
sparq-tool dev
sparq-tool dev plugin
sparq-tool dev --file sparq.dev.yaml --dry-run
~~~

`dev` 和 `dev plugin` 都读取并严格校验 sparq.dev.yaml，然后按宿主、插件后端、开发态 token、入口覆盖、插件前端的顺序完成本地联调。保留 `dev` 入口用于兼容现有插件项目；新脚本可以使用语义更明确的 `dev plugin`。配置文件目录是相对 cwd、Manifest 和其他相对路径的解析基准。

配置至少需要：

~~~yaml
app:
  endpoint: http://127.0.0.1:19090
  dev_secret: local-sparq-development-token-secret-32-bytes

plugin:
  id: cms
  component: content
  manifest: plugins/sparq-plugins-cms/plugin.yaml
  endpoint: http://127.0.0.1:19091
  command: go run ./cmd/cms
  secret: 0123456789abcdef0123456789abcdef
  reload:
    enabled: true
    paths: [.]
    extensions: [.go]
    files: [go.mod, go.sum]
    exclude: [.git, frontend, storage]
    debounce: 300ms
    build: go build ./...

frontend:
  enabled: true
  cwd: plugins/sparq-plugins-cms/frontend
  command: pnpm dev
  url: http://127.0.0.1:5177/plugins/cms
  health: /src/frontend.ts
~~~

app.command 可选；省略时使用已经运行的宿主。plugin.manifest 省略时默认为配置文件同目录下的 plugin.yaml，并且始终校验 metadata.code。`plugin.component` 表示当前 Remote Runner 的组件；省略时从完整 Manifest 自动选择唯一支持 HTTP runtime 的 backend/worker，多个候选必须显式指定。plugin.secret 必须至少 32 字节，工具只注入插件进程；宿主必须在 `dev.plugins.<plugin>` 中预先配置相同值。`app.dev_secret` 必须与宿主 `dev.token_secret` 相同，用于申请开发生命周期 session。存在 `frontend/src/frontend.ts` 时自动启用前端并登记该唯一 root；多个插件同时联调时通过 `frontend.url` 和对应启动命令分配不同端口。

启动过程中会重试健康检查；未启用 `plugin.reload` 时任一进程退出都会结束本次开发会话。宿主、前端退出或收到 Ctrl+C 时，工具会停止本次启动的进程并调用 /dev/auth/revoke。--dry-run 只打印计划，不启动进程。前端环境变量为 VITE_SPARQ_HOST、VITE_SPARQ_PLUGIN_ID、VITE_SPARQ_DEV_TOKEN；插件后端环境变量包含 SPARQ_APP_URL、SPARQ_PLUGIN_ID 和 SPARQ_<PLUGIN>_APP_URL、BASE_URL、ADDR、COMPONENT_ID、PLUGIN_SECRET。

启用 `plugin.reload` 后，`dev` 复用 `internal/watchrun` 的通用监听和进程替换能力，只在适配层增加 Sparq 插件签名健康检查。`paths` 相对 `plugin.cwd` 解析。工具启动插件前先执行 `build`；文件变化经过 `debounce` 合并后再次构建，失败时保留旧插件，成功时优雅停止旧进程、启动新进程并重新等待健康检查。插件自身异常退出不会停止宿主和前端，watcher 会等待下一次文件变化后重新构建。第一版不监听宿主，前端继续使用 Vite HMR，也不对异常进程执行无限自动拉起。

宿主的开发 token 路由只有在 dev.enabled=true 且非 production 环境注册；`/dev/auth/token` 要求以 `dev.token_secret` 作为 Bearer 凭证，并签发只拥有 `dev.frontend` 的 1 小时内存 token。工具在启动前端前调用 `/dev/frontend` 注册进程内 root 覆盖，不修改正式 Manifest；每个 token 独立拥有自己的覆盖。`frontend.entries` 已删除，iframe Sandbox Entry 不进入该覆盖。宿主按已安装的可信运行时 Manifest 校验 root 归属。工具会在 token 剩余有效期过半时再次调用 `/dev/frontend`，宿主校验 token 与插件归属后续租该 token；心跳失败会结束当前开发会话，root 加载失败也不会静默回退正式来源。退出、撤销或定时过期清理只移除对应 token 的覆盖。

### dev app

```bash
sparq-tool dev app
sparq-tool dev app --file sparq.dev.yaml --dry-run
```

`dev app` 用于 Sparq 二次开发应用，不读取插件 Manifest，也不请求插件开发 token 或调用 `/dev/frontend`。它读取项目 `sparq.dev.yaml`，并自动合并 `$SPARQ_HOME/sparq-tool/config.yaml` 中的用户级工具链默认值。`frontend.mode` 为 `source` 或 `source-dev` 时，工具先按 `backend.watch_file` 启动后端 watch 会话并等待后端健康检查，再启动前端开发进程并观察前端 URL 是否返回 2xx；`artifact` 或 `disabled` 时只启动后端；`source-build` 会明确报错。

`frontend.timeout` 到期但前端进程仍在运行时，工具会输出就绪警告并继续保持本次开发会话，避免 Vite 首次依赖预构建或页面首次编译较慢时连带停止已经就绪的后端。任一进程真正退出或收到 Ctrl+C 时，工具会停止本次启动的另一进程和文件监听器。

```yaml
backend:
  watch_file: app/config/sparq.watch.yaml

frontend:
  cwd: frontend
  command: pnpm dev
  url: http://127.0.0.1:5176/platforms/sparq-website/
  timeout: 30s
  stop:
    grace_timeout: 3s
    kill_timeout: 2s
```

配置文件目录是 `backend.watch_file` 和 `frontend.cwd` 的解析基准。被引用的 watch 配置必须提供 `health.url`，否则工具无法确认后端就绪后再启动前端。`frontend.url` 会通过 `SPARQ_DEV_FRONTEND_URL` 传给应用后端，应指向应用注册给 Admin Shell 的开发入口；它不需要在应用运行配置中重复声明。超时警告不表示 URL 已经可访问，仍需根据前端命令输出排查编译失败或地址配置错误。

`dev app` 只负责进程编排，不动态修改生产配置，也不向稳定协议增加开发字段。后端开发命令仍由 `sparq.watch.yaml` 明确声明；用户级 `toolchain.go` 不会改写其中的任意 shell 命令。应用正式构建使用独立的 `build.yaml`，不读取 `frontend.mode`。`--dry-run` 只解析、校验和打印执行计划，不检查端口占用，也不启动进程。

### plugin package

```bash
sparq-tool plugin package [path] [--manifest-json <path>]
```

当前生成无签名的 `package v1` 本地插件包。压缩包始终包含 `plugin.yaml`、`manifest.json`、`release.json` 和包内 `checksums.txt`，并自动包含 `schema_resources[].file` 引用的 JSON，保证安装阶段能够重新加载并校验完整 Manifest 源文件集合。

默认情况下，工具优先读取 `release/manifest.json`，再回退到 `plugin.yaml` 转换结果。使用 Go `manifest.Compose` 的插件如果缺少该构建产物会拒绝打包，并提示先运行 `plugin build`。`--manifest-json` 可在兼容场景显式覆盖默认路径；所有 JSON 都会执行协议校验，并要求完整 Manifest 的插件 code、version 与 `plugin.yaml` 一致。`plugin package` 不执行 Go、前端或其他插件源码命令。

已生成的包内结构：

```text
release.json
manifest.json
checksums.txt
bin/<plugin-code>
frontend/
```

`signatures/release.ed25519`、`manifest generate --check` 前置检查和安装侧校验后续接入。完整结构见 [插件发布包格式](plugin-package-format.md)。

### plugin download

```bash
sparq-tool plugin download <code>
sparq-tool plugin download <code> --version 1.2.3
sparq-tool plugin download <code> --mode remote --os linux --arch arm64
sparq-tool plugin download <code> --mode frontend
sparq-tool plugin download <code> --output ./downloads/
```

命令使用当前 Auth Profile 调用 `POST /cloud-api/v1/application-downloads/resolve`。默认请求 `local_source_capsule`；`--mode remote` 请求目标 OS/Arch 的 `managed_remote`；`--mode frontend` 请求与操作系统无关的 `frontend_bundle`。平台只返回当前用户已授权、版本已发布、来源摘要仍匹配审核源码并通过 Storage 一致性检查的 Artifact。独立可信构建器产物要求签名；平台开发者主体经人工审核提升的产物使用 `platform_account` 保护等级。

下载先写入输出目录中的临时文件，文件大小和 SHA-256 都通过后才原子写入最终路径。目标文件已经存在时命令拒绝覆盖。当前命令不创建授权激活，不解密 Local 胶囊，不编译宿主，也不执行安装；包内 Ed25519 签名由后续安装流程验证。

仓库当前可以生成并通过可信构建器接口登记 Local 胶囊 Artifact，但不会在源码审核通过后自动生成；管理员内部接口只作为应急入口。版本尚未登记对应交付物时，命令会明确返回 `artifact_unavailable`。

### plugin publish

```bash
sparq-tool plugin publish [path]
sparq-tool plugin publish [path] -m "更新说明"
sparq-tool plugin publish [path] --bump patch
sparq-tool plugin publish [path] --version 2.0.0-beta.1
```

在交互终端执行且没有指定版本参数时，命令先显示 `plugin.yaml` 当前版本，并提供保持版本、递增 Patch/Minor/Major 和自定义版本五种选择；直接回车默认递增 Patch。自定义版本必须是完整 SemVer，例如 `2.0.0` 或 `2.0.0-beta.1`。

脚本和 CI 中必须显式选择一种版本策略：`--bump patch|minor|major` 或 `--version <semver>`。两个参数互斥，缺少参数时非交互发布会直接失败，不会默认沿用或递增版本；需要沿用当前版本时显式把当前版本传给 `--version`。

命令先解析并检查 Auth Profile；认证缺失或过期时不会修改插件版本，也不会执行构建。确定版本后，命令先保存本地恢复状态，再将结果原子写回 `plugin.yaml`，清理旧的 `release/manifest.json` 并重新生成 `manifest_gen.go`，再执行 `plugin build --target all`。前端构建产物只打成一个 `frontend_bundle`；存在 Remote Runner 时按 Linux、macOS、Windows 的 amd64/arm64 六个平台分别交叉构建并打成同时包含前后端的 `managed_remote` 组合包。没有这两类产物时生成一个 `plugin_package` 兼容候选包。命令最后生成临时审核源码包，创建或恢复上传会话，补传候选包并调用 finalize。平台响应中的版本、来源摘要、候选摘要和交付目标必须与本地结果一致，否则命令失败。

`plugin.yaml` 是应用代码、版本号和 Sparq 兼容约束的唯一事实来源。版本一旦写入，即使构建、打包或上传失败也不会自动回滚，命令也不会创建 Git commit 或 tag；再次执行会从 `$SPARQ_HOME/sparq-tool/publish-state/` 恢复同一目标版本，不会重复 bump。首个版本可以不填版本说明；已有其他版本时必须使用 `-m/--message`。发布固定使用认证配置中的当前 profile，幂等键由本地发布会话自动生成和维护，不提供命令行覆盖。

命令依次调用源码上传、发布会话详情、候选上传和 finalize 接口。源码上传后版本保持 `validating`，finalize 原子核对并冻结完整候选清单后才进入 `review_pending`。当前登录用户必须是 Application 所属开发者的启用成员。平台开发者主体的 `frontend_bundle` 与完整六平台 `managed_remote` 进入 `internal_review`，人工审核通过后在同一事务内提升为正式 Artifact；普通开发者主体返回 `rebuild_required`。`plugin_package` 只作兼容候选，不会直接提升。

候选包上传失败时源码记录已经创建，CLI 会在错误中保留 `source_upload_id` 和源码 SHA-256，不声称整个发布成功。

平台返回 `404/40400` 时，CLI 会显示当前插件 code、认证 endpoint 和 profile，并提示检查 `plugin.yaml` 与 Marketplace 管理端的应用登记。该提示只用于排查，不会自动创建应用。

### theme publish

```bash
sparq-tool theme publish [path]
sparq-tool theme publish [path] -m "更新说明"
sparq-tool theme publish [path] --bump patch
sparq-tool theme publish [path] --version 1.0.0
sparq-tool theme publish [path] --keep-version
```

主题目录必须包含 `template.yaml`、`theme.yaml`、`views/` 和 `assets/`。两份清单的 code 和 version 必须一致；`theme.yaml` 必须声明至少一个 `formFactors`（仅 `desktop/tablet/mobile`）、至少一个 `appearance.colorSchemes`（仅 `light/dark`）以及 `#RRGGBB` 格式的 `appearance.palette.primary`。`theme.yaml` 不配置业务视图映射；`views/` 至少包含一个 `.html` 或 `.tpl` 模板，供 Resolver 选择的页面入口应使用顶层安全模板标识。主题 ZIP 只允许包含双清单、可选 `cover.png`、模板和静态资源，不会包含源码依赖、构建目录或其他文件。

交互终端未指定版本参数时，命令提供保持、Patch、Minor、Major 和自定义版本选择，默认递增 Patch。脚本和 CI 必须显式使用 `--bump`、`--version` 或 `--keep-version`。版本变化时命令同时原子更新 `template.yaml` 与 `theme.yaml`；第二份清单写入失败会恢复第一份，避免主题目录进入双清单版本不一致状态。

发布过程固定输出四个阶段：准备发布版本、校验并打包主题、创建或恢复源码会话、完成提交并进入审核。工具生成确定性 ZIP 和 SHA-256，上传成功后调用 Template finalize；只有 finalize 成功才报告发布申请已提交。Theme 使用与 Plugin 相同的本地恢复目录和不重复 bump 语义。Website 扫描 `views/` 并把模板标识、支持终端、色彩模式和色板保存为该发布版本的只读事实；模板分类、适用行业和风格标签由后台单独维护。平台审核通过后，Website 将这份审核 ZIP 原样登记为 `template_source` Artifact。

发布自行准备的主题源码目录：

```bash
sparq-tool theme publish ./my-theme --keep-version -m "主题发布联调"
```

将 `./my-theme` 替换为包含双清单、`views/` 和 `assets/` 的实际主题源码目录。首次发布时平台会在完整校验主题包后，按当前 Cloud 用户所属的唯一开发者主体和清单中的 code 自动创建主题应用；后续发布要求该 code 仍属于同一开发者主体。当前 Cloud 用户必须是启用中的开发者成员。该命令会真实创建应用（首次发布）、版本源码记录，不是只读检查。

### 插件部署

```bash
sparq plugin status
sparq plugin enable <code>
sparq plugin disable <code>
sparq plugin start <code>
sparq plugin stop <code>
sparq plugin restart <code>
```

`sparq-tool` 不负责生产插件部署。安装、更新、卸载由 Sparq Admin 处理；`enable/disable` 修改宿主持久期望状态，`start/stop/restart` 只管理 Managed Runtime 当前实例。

`sparq plugin` 只连接当前宿主的本机控制通道：Unix/macOS 使用 `run/sparq.sock`，Windows 或 Unix Socket 不可用时使用 `run/sparq.addr` 中带随机控制令牌的 loopback TCP endpoint。宿主未运行时命令会明确失败，不会自行启动第二个 App 实例。

### restart app / plugin

```bash
sparq-tool restart app
```

`restart app` 的 `command` driver 执行显式 shell 命令。插件重启由宿主自身的生命周期管理器执行。

以下 driver 已预留入口：

```text
systemd
docker-compose
```

## 其他命令入口

以下小节包含仍在预留阶段的入口。生产插件生命周期不由 `sparq-tool` 管理。

### artifact verify

```bash
sparq-tool artifact verify <artifact>
```

后续用于统一执行 checksum 和 signature 校验。

### package app

```bash
sparq-tool package app [workspace-root] --version 0.2.0
```

`package app` 不重新编译源码，它读取 `build app` 生成的 Runtime、Updater、`public/admin/` 和 `public/assets/captcha/`，为每个目标平台组装可直接解压安装的完整 app bundle。默认在 Unix 平台生成 `tar.gz`，在 Windows 平台生成 `zip`：

```bash
sparq-tool build app . --target linux/amd64 --version 0.2.0
sparq-tool package app . --target linux/amd64 --version 0.2.0

# 只生成解压后的目录，便于检查内容
sparq-tool package app . --target linux/amd64 --version 0.2.0 --archive none
```

主要参数：

- `--input`：`build app` 的二进制输出目录，默认 `release/bin`。
- `--output`：bundle 输出目录，默认 `release/packages/<version>`，已有目录不会被覆盖。
- `--target`：目标平台，规则与 `build app` 相同。单平台输入使用原始文件名，多平台输入使用 `<binary>-<os>-<arch>`（Windows 带 `.exe`）；旧版 `<input>/<os>/<arch>/<binary>` 目录仍可读取。
- `--archive`：可选 `auto`、`none`、`tar.gz` 或 `zip`，默认 `auto`。
- `--version`：bundle manifest 和文件名使用的版本，默认 `dev`。

每个归档只包含目标平台的 `sparq`、`sparq-updater`、`config/app.yaml`、`config/app.example.yaml`、构建后的 `public/`、安装说明、`manifest.json` 和 `checksums.txt`。其中 `public/admin/__app__/` 是二次开发应用前端及其动态 chunk 的固定目录，会随 Admin Shell 一起进入安装包；运行期的 `public/admin/plugin-assets/`、`run/`、`storage/`、插件、主题、数据库、日志与上传文件不会进入安装包。

## 后续实现顺序建议

建议按稳定性从高到低补业务：

```text
1. plugin package 签名与宿主安装侧消费
2. local use / unuse
3. artifact verify
4. restart systemd/docker-compose
5. plugin publish 后续 CI scoped token
6. dev 注册和清理流程
7. marketplace 购买、安装计划和更新检查 API
8. builder Build Session 和远程构建完整协议
```

## 当前约束

```text
1. 业务协议未稳定前，不在工具中写死临时远程 API。
2. 预留入口必须保持中文描述和中文输出。
3. 可执行命令优先覆盖本地确定性能力。
4. 涉及 app 源码修改的命令必须先做 local check。
5. 生产插件安装和更新必须由 Sparq Admin 执行，不应自动覆盖部署方源码。
```
