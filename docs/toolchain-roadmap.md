# sparq-tool 工具链路线

本文记录 `sparq-tool` 后续要覆盖的实际需求和实现边界。当前结论是：先保留需求和接口方向，不急于完整实现依赖宿主、插件运行时、前端注册协议的重业务闭环。

## 定位

`sparq-tool` 是 Sparq 的开发、构建、打包、发布和 Artifact 下载工具。生产部署和运行管理由 Sparq 宿主与 Admin 负责：

```text
开发 -> 构建 -> 打包 -> 发布/下载 -> Admin 安装更新 -> 宿主运行管理
```

每一段能力都应该可以单独执行，避免把所有行为压进一个不可拆分的一键命令。

## 当前决策

当前宿主、插件注册、前端挂载、权限和安装记录等协议还在演进中，因此本阶段只做：

- 保持已有基础命令可用。
- 明确后续工具链目标。
- 预留命令结构和配置字段方向。
- 不在工具中写死临时 remote API、前端菜单协议或安装状态机。

等宿主和插件闭环稳定后，再回到 `sparq-tool` 补齐完整开发编排、标准插件包、安装更新和服务守护能力。

## 已有基础

当前已经具备一批确定性能力：

- `plugin init`
- `manifest validate`
- `checksum create / verify`
- `signature keygen / sign / verify`
- `artifact download`
- `builder capsule keygen / create`
- `doctor`
- `watch`
- `dev`
- `local check`
- `local list`
- `local sync`
- `build app`
- `plugin build`
- `plugin package`
- `plugin publish`
- `plugin download`
- `restart app`
- `auth login / status / whoami / logout`

其中 `watch` 是独立的通用文件监听与进程替换命令；`plugin build/package/publish` 已形成构建验证、本地打包和源码审核申请三段流程，`plugin download` 已接入授权下载解析并在落盘前校验大小和 SHA-256。下载结果交给 Sparq Admin 执行安装或更新，工具不持有宿主 Session、CSRF 或部署授权。

## 目标需求

### watch 通用开发运行器

`watch` 通过 `internal/watch` 提供配置解析、递归文件监听、notify/poll 驱动、glob 规则、构建检查、进程树清理、优雅替换、失败退避重启和可选普通 HTTP 健康检查。该模块不依赖 Manifest、开发 token 或 `sparq-protocol`，可以用于插件之外的 Go 服务和其他本地开发进程。`watch --init` 用于生成不会覆盖已有文件的标准配置。

`dev` 的配置、执行计划、插件签名健康检查及开发态生命周期位于 `internal/dev`，并复用 `internal/watch` 的运行引擎；`cmd/internal/dev` 只保留 flags、终端信号和用户输出。第一版支持 Linux、macOS 和 Windows；Windows 已覆盖编译验证，控制台信号和进程树清理仍需在真实 Windows 环境持续回归。

### dev 开发编排器

目标体验类似 `wails3 dev`：一条命令拉起宿主、插件后端和插件前端，并完成开发态联调。

长期需要支持：

- 启动宿主 app。
- 启动插件后端进程。
- 启动插件前端 dev server。
- 聚合 app、插件后端、插件前端日志。
- 等待 app 和插件 health check。
- 自动注册 remote 插件实例。
- 自动注册插件前端 dev URL。
- 由 Runner 随完整 `plugin.Registration` 发布开发态 Route 元数据。
- 注入开发态环境变量和 token。
- 前端继续依赖 Vite HMR。
- 进程异常退出后按策略重启。
- Ctrl+C 后注销插件 Instance、前端入口和对应 Route。

当前已经支持监听插件后端文件、构建成功后优雅重启、重新健康检查，以及构建失败时保留旧进程。通用 `watch` 已提供可配置异常重启策略；`dev` 仍使用“插件异常退出后等待文件变化”的默认策略，后续是否对插件开放自动重启配置应结合注册租约和开发态清理行为单独确认。日志聚合仍未实现。

### 远期：Schema Designer 本地工作流

目标是让插件开发者可以通过 `sparq-tool` 启动前端 Schema Designer，在浏览器中拖拽字段、表单布局、视图和动作配置，并将结果保存为插件目录中的 Schema JSON，减少手工维护作者态规则的成本。

候选命令方向：

```bash
sparq-tool schema design ./plugins/sparq-plugins-cms
sparq-tool schema design ./plugins/sparq-plugins-cms --input schema/article.schema.json
sparq-tool schema design ./plugins/sparq-plugins-cms --output schema/article.schema.json
```

当前只记录方向，不进入本阶段实现。设计器实现继续归属 `sparq-frontend` 的 `@sparqjs/schema-designer`；`sparq-tool` 未来只负责启动本地静态服务、限定插件目录内的文件读写、调用 Schema 校验并衔接 `manifest validate` 与 `plugin build`。即使后续把静态资源嵌入 Go 二进制，也不能把设计器实现倒灌进工具链能力层。

第一阶段输出范围应限定为：

- Schema Resource JSON。
- `plugin.yaml` 的 `schema_resources` 引用或对应片段。
- 前端规范化和后端 Schema 校验结果。

不通过拖拽结果生成 Go Service、Repository、数据库迁移或 `ResourceExecutionPlan`。这些内容依赖业务领域规则、`migration_owner` 和 Runtime 边界，必须由插件开发者和 `sparq-schema` 相关实现维护。

进入实现前需要确认：

- Designer 静态包的版本绑定、发布和离线分发方式。
- 本地服务的随机端口、目录访问白名单、原子写入和关闭清理策略。
- Schema JSON 的格式化、稳定排序和写回策略。
- 设计器扩展、插件专属 Designer Module 和自定义组件的加载边界。
- 前端校验与后端 `manifest validate`、`plugin build` 的责任划分。

### plugin build / package 构建与本地打包

目标是生成可校验、可发布、可安装的标准插件包。

标准插件发布包采用 `package v1`，详细结构见 [插件发布包格式](plugin-package-format.md)。压缩包解开后根目录直接包含 `plugin.yaml`、`manifest.json`、`release.json`、`checksums.txt`、`bin/` 和 `frontend/` 构建产物，不再包一层插件目录。安装阶段先解压到 staging 目录，校验包内文件后再切换到目标插件目录。

长期需要支持：

- 构建 Go 插件后端。
- 构建插件前端 `dist`。
- 校验 `plugin.yaml`。
- 在前端构建后重新生成 `manifest_gen.go`，并导出完整 `release/manifest.json`。当前已支持。
- 生成标准包结构。当前 `plugin package` 已生成无签名 `package v1`。
- 生成 `release.json`。当前已支持。
- 生成 `checksums.txt`。当前已支持包内清单。
- 生成 `signatures/release.ed25519`。
- 从 `frontend/dist` 读取并打包到 `frontend/`，排除 `.vite/`，不打包 `frontend/src`。
- `plugin package` 优先消费完整构建 Manifest，Compose 插件缺失时拒绝打包。当前已支持。
- 支持 CI/CD 中的确定性构建。

构建打包阶段不负责启动进程和注册插件 Instance。

### TODO：公开站点主题打包与数据初始化

公开站点 Blade 主题后续由 `sparq-tool` 负责打包和上传。主题作者在应用主题目录中完成模板、CSS、图片及其他主题资源后，工具在打包阶段校验并固化主题包包含的资源清单，安装端只消费经过校验的主题产物。

主题包需要覆盖以下内容：

- `theme.yaml` 中的主题身份、版本、外观和终端能力；模板标识直接从 `views/` 扫描，不维护业务视图映射。
- `views/` 下的 Blade 模板。
- `assets/` 下的 CSS、图片、字体和允许发布的前端脚本。
- 主题首次安装需要使用的初始化数据资源。

初始化数据不在当前阶段定义，也不作为 `packages/sparq/site` 的普通数据库迁移执行。后续设计打包协议时，需要由 `sparq-tool` 确定可导出的资源范围并写入主题包；安装主题时，再按包内声明调用对应模块的数据初始化入口。这样主题开发者可以在本地完成 CMS 分类、导航等配套内容，再随主题一起打包交付，不需要在安装端手工重建。

进入实现前仍需确认：

- 主题资源清单和初始化数据在包内的格式。
- CMS、导航等模块如何声明可导出和可初始化的资源。
- 初始化数据之间如何使用稳定标识建立引用，避免携带开发数据库主键。
- 重复安装、版本升级和已存在数据冲突时的处理规则。
- 主题卸载是否保留已经初始化并可能被用户修改的数据。

在以上协议确认前，主题管理只处理主题文件的安装、启用、切换和卸载，不提前加入数据导出或初始化实现。

### plugin publish 发布申请

`plugin publish` 依次完成 build、package，再把审核源码包和本地 package v1 候选包分别提交平台。审核源码包进入基础校验和人工审核，候选包绑定同一源码摘要供平台校验或重建参考。审核源码包结构见 [插件源码上传包](plugin-source-upload.md)。

候选包与审核源码包是两个独立对象；`release.json` 和 `checksums.txt` 只存在于候选包，不作为源码输入。Website 会校验候选包，但普通开发者身份不能使它成为可信 Artifact，当前统一标记为 `rebuild_required`。Local 胶囊仍在审核通过后由独立构建角色生成并登记；自动构建、Remote 二进制签名和服务身份认证继续由后续独立方案负责。

### install / update / remove / rollback 部署闭环

目标是让插件安装、更新、移除和回滚具备状态记录和失败恢复能力。

长期需要支持：

- 解包插件产物。
- 校验 checksum 和 signature。
- 校验协议版本兼容性。
- 写入安装记录。
- 迁移插件配置。
- 启动插件进程。
- 等待 health check。
- 注册插件 Instance，以及它包含的 Provider、Route 和 frontend entry。
- 更新失败自动回滚。
- 移除时停止进程、注销插件 Instance、清理对应 Route。
- 回滚时恢复安装记录并重新确认 health。

在安装状态机稳定前，不应把复杂行为提前固化到工具中。

### service / supervisor 运行守护

目标是提供插件和宿主的运行管理能力，但不第一版自研完整守护系统。

长期命令方向：

```bash
sparq-tool service start
sparq-tool service stop
sparq-tool service restart
sparq-tool service status
sparq-tool service logs
```

长期 driver 方向：

```yaml
service:
  driver: local
  # driver: systemd
  # driver: windows-service
  # driver: docker-compose
```

建议实现顺序：

1. 先支持本地轻量进程管理。
2. 再接入 systemd。
3. 再接入 Windows Service。
4. 最后考虑 Docker Compose 或远程部署 driver。

### 认证与 Marketplace

平台交互依赖外部服务协议。认证、源码上传、市场列表/详情和授权 Artifact 下载已经接入；除开发模式和宿主交互外，`sparq-tool` 的平台请求统一使用 `auth login` profile。

长期需要支持：

- `plugin list/search/info` 和 `theme list/search/info` 线上资源浏览（已实现）。
- 插件授权 Artifact 下载（已由 `plugin download` 实现）；Local 胶囊解密编译和宿主安装仍待实现。
- Sparq Cloud 用户登录态和 profile 管理（已由 `auth` 命令实现）。认证不归属于 Marketplace，也不绑定单个插件。
- 远程可信构建由独立平台服务承担，不在 `sparq-tool` 中维护 Service Token 或独立 endpoint。
- 产物校验、签名验证和安装衔接。

当前 `auth login` 使用内置 Cloud endpoint 和终端隐藏密码完成账号登录。后续交互登录应改为浏览器一次性授权，使 CLI 不再接触账号密码；CI/API 上传使用带明确 scope 的开发者访问令牌，不复用交互式用户密码和长期 Session。

插件相关命令已经按对象收敛：源码发布入口归 `sparq-tool plugin publish`，授权交付下载归 `sparq-tool plugin download`。在线安装、更新和回滚产生宿主副作用，应由 `sparq` 宿主安装服务拥有事务规则，`sparq-tool` 只作为调用入口。Marketplace 只负责版本、授权和制品获取适配，不写入 `sparq` 核心。

## 配置方向

未来 `sparq.dev.yaml` 可以表达完整开发编排计划：

```yaml
app:
  command: go run ./sparq-app/cmd/sparq
  endpoint: http://127.0.0.1:8080

plugin:
  id: remote-echo
  command: go run ./plugins/sparq-plugins-remote-echo/cmd/remote-echo
  endpoint: http://127.0.0.1:19091
  health: /health
  invoke: /invoke

frontend:
  enabled: true
  command: pnpm dev
  cwd: plugins/sparq-plugins-remote-echo/frontend
  url: http://127.0.0.1:5173
```

当前 `plugin.yaml` 已支持：

- runtime 支持模式。
- capabilities 提供和依赖。
- frontend entry。
- Permission、Schema Resource、Menu contribution 及其单调版本。
- 使用插件内 JSON 文件保存并由主清单引用 Schema Resource。

仍需逐步补充：

- `plugin.Registration` 中的 Route 元数据。
- settings schema。
- service 启动策略。

## 暂缓实现项

以下能力等宿主和插件协议稳定后再实现：

- `dev` 自动插件 Instance 注册和注销。
- 插件安装记录和版本状态机。
- 运行中实例的本地控制 Socket（当前 `sparq plugin` 适用于本机宿主维护）。
- 更新失败自动回滚。
- 完整进程守护。
- systemd / Windows Service / Docker Compose driver。
- Marketplace 购买、更新检查和完整交付 API。
- 远程构建完整协议。
- 一键部署。

## 建议实现顺序

```text
1. 保持基础命令稳定，完善测试和文档。
2. 等宿主注册协议稳定后，增强 dev 为开发编排器。
3. 为 `plugin package` 补齐签名，并接入 Admin 安装侧可信校验。
4. 完善宿主本地插件控制和进程守护。
5. 接入 systemd、Windows Service、Docker Compose driver。
6. 接入 Marketplace 购买、更新检查和 remote builder 完整服务。
```
