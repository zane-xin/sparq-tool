# 插件发布包格式

本文定义 `sparq-tool plugin package` 生成、`install/update/rollback` 后续要消费的标准插件包。当前 `plugin package` 已生成无签名的 `package v1` 压缩包；签名校验和安装侧消费还未接入。

## 适用范围

插件发布包用于本地安装、更新和后续 marketplace 下载后的离线校验。它不是源码仓库备份，也不是开发模式工作目录。宿主更新失败时只恢复本次事务中的旧包；成功更新后不保留本地历史版本，需要主动恢复旧版本时重新获取对应 Artifact。

该格式承载预构建运行产物。需要在用户侧把源码编译进宿主的 Local 商业插件使用独立的 `sparq.plugin.source-capsule.v1` 加密胶囊，不把审核源码包改名后作为发布包。胶囊边界见 [Local 加密构建胶囊](plugin-local-build-capsule.md)。

发布包只保存运行和安装需要的文件：

- 插件 Manifest 源文件集合。
- 由 Manifest 转换得到的稳定协议清单。
- 后端可执行产物。
- 前端构建产物。
- 校验和签名文件。
- 描述本次发布的元数据。

不进入发布包的内容：

- `frontend/src`、`node_modules`、`.vite`、测试文件和开发配置。
- `.git`、本地缓存、临时目录、日志和数据库文件。
- 宿主绑定配置，例如 realm、application、菜单挂载点和插件密钥。

## 压缩包命名

默认文件名：

```text
<plugin-code>-<version>.tar.gz
<plugin-code>-<version>-frontend.tar.gz
<plugin-code>-<version>-<goos>-<goarch>.tar.gz
```

示例：

```text
cms-0.1.0.tar.gz
```

`plugin-code` 来自 `plugin.yaml` 的 `metadata.code`，`version` 来自 `metadata.version`。工具不从目录名推导这两个值。

## 压缩包根目录

压缩包解开后，文件直接位于根目录，不再包含一层 `<plugin-code>/` 包裹目录。

原因是安装阶段会先解压到临时 staging 目录，再读取 staging 根目录的 `plugin.yaml`。这样 installer 不需要猜测压缩包顶层目录名称，也能避免包名和 Manifest code 不一致时出现歧义。

```text
.
├── plugin.yaml
├── manifest.json
├── release.json
├── checksums.txt
├── signatures/           # 可选，签名接入后生成
│   └── release.ed25519
├── schema/
│   └── *.schema.json
├── bin/
│   └── <plugin-code>
├── frontend/
│   ├── assets/
│   └── modules/
└── README.md
```

## 必需文件

### `plugin.yaml`

插件 Manifest 主清单。它和 `schema_resources[].file` 引用的 JSON 文件共同组成 Manifest 源文件集合。

安装和更新阶段必须重新读取 `plugin.yaml` 并执行 `manifest validate`。不能只信任 `release.json` 或文件名。

### `manifest.json`

由 Manifest 源文件集合转换得到的 `packages/sparq-protocol/plugin.Manifest` JSON。

用途：

- 让安装阶段可以快速读取稳定协议对象。
- 让 release 审计不需要重新理解 YAML 输入模型。
- 让后续 marketplace 或远程构建服务可以基于同一协议对象做索引。

生成规则：

- 必须由 `plugin.yaml` 和引用文件生成。
- 字段顺序由 Go JSON 编码结果决定，不手写维护。
- `manifest.json` 与 `plugin.yaml` 不一致时，安装阶段必须拒绝该包。

### `release.json`

描述本次发布包本身，不替代 Manifest。

第一版结构：

```json
{
  "format": "sparq.plugin.package.v1",
  "plugin": {
    "code": "cms",
    "name": "CMS",
    "version": "0.1.0"
  },
  "protocol": {
    "range": ">=0.1.0"
  },
  "artifacts": {
    "manifest": "manifest.json",
    "backend": "bin/cms"
  },
  "delivery": {
    "kind": "managed_remote",
    "runtime": "http",
    "target_os": "linux",
    "target_arch": "amd64"
  },
  "source_files": [
    "plugin.yaml",
    "schema/cms.documents.schema.json"
  ],
  "created_at": "2026-08-02T00:00:00Z",
  "tool": {
    "name": "sparq-tool",
    "version": "dev"
  }
}
```

字段语义：

- `format`：包格式版本。第一版固定为 `sparq.plugin.package.v1`。
- `plugin`：来自 Manifest metadata，用于安装前展示和路径选择。
- `protocol.range`：来自 `plugin.yaml` 的 `sparq.protocol`。
- `artifacts.manifest`：稳定协议清单路径，固定为 `manifest.json`。
- `artifacts.backend`：后端可执行文件路径；纯前端插件可为空。
- `artifacts.frontend`：前端构建产物目录；无前端 entry 时可为空。
- `delivery.kind`：发布候选变体。当前支持 `frontend_bundle` 和 `managed_remote`；未设置时表示兼容整包。
- `delivery.runtime`：`managed_remote` 固定为 `http`，并且必须同时包含后端和前端构建产物。
- `delivery.target_os/target_arch`：Remote Runner 的目标平台；前端包为空。
- `source_files`：Manifest 源文件集合，至少包含 `plugin.yaml`。
- `created_at`：构建时间，使用 UTC RFC3339。未由受控构建环境显式提供时固定为 Unix Epoch，避免同一源码和构建产物重复打包后产生不同 SHA-256。
- `tool`：生成工具信息，用于排查包来源。

### `checksums.txt`

发布包内文件的 SHA-256 清单。格式与常见 checksum 文件一致：

```text
<sha256>  <relative-path>
```

规则：

- 路径必须使用正斜杠。
- 路径必须相对发布包根目录。
- 不包含 `checksums.txt` 自身。
- 不包含 `signatures/*`。
- 必须包含 `plugin.yaml`、`manifest.json`、`release.json` 和所有发布产物。

安装阶段先校验 `checksums.txt`，再读取 Manifest。

## 可选文件和目录

### `signatures/release.ed25519`

对 `checksums.txt` 的签名。第一版使用 Ed25519。

签名对象选择 `checksums.txt`，不是直接签整个 tar.gz。这样安装阶段解压后仍能对包内文件逐项校验，并能支持后续镜像或缓存重打包。

没有签名文件时，本地安装可以继续，但应显示“未签名包”的警告。来自 marketplace 的包后续应要求必须签名。

### `bin/`

插件后端可执行文件目录。默认文件：

```text
bin/<plugin-code>
```

每个 `managed_remote` 只包含一个目标平台的可执行文件和同版本的前端构建产物，后端统一为 `bin/<plugin-code>`；Windows 文件为 `bin/<plugin-code>.exe`。目标平台记录在 `release.json.delivery`，不从包内目录猜测。

### `frontend/`

插件前端构建结果。构建阶段默认从 `frontend/dist` 读取，但发布包会把其内容提升到 `frontend/` 根目录，不把 `dist` 作为运行时目录层级。

`.vite/` 只用于构建期的 Vite Manifest 解析，不属于运行时资源，发布包明确排除该目录。如果插件声明了 frontend entry，但包内没有 `frontend/` 或清单声明的文件，安装阶段应拒绝或给出明确错误。

### `README.md`

插件说明文档。可选，但建议保留，供本地安装前查看。

### `schema/`

`plugin.yaml` 中 `spec.contributions.schema_resources[].file` 引用的 JSON 文件必须进入发布包。目录名不强制为 `schema`，但路径必须和 Manifest 中的相对路径完全一致。

## 路径和安全规则

发布包内路径必须满足：

- 使用正斜杠。
- 不能是绝对路径。
- 不能包含 `..` 逃逸。
- 不能通过符号链接离开包根目录。
- 不能覆盖同一路径。
- 文件权限只保留运行必要权限；普通文件默认 `0644`，后端可执行文件默认 `0755`。

安装阶段必须先解压到 staging 目录，校验通过后再切换到目标插件目录。

## 与安装状态的关系

发布包不直接保存安装状态。安装状态属于目标宿主环境，后续由 install/update 写入本地记录。

发布包只回答：

```text
这个包是什么插件、什么版本、包含哪些文件、这些文件是否完整可信。
```

安装记录回答：

```text
这个宿主安装了哪个插件、当前使用哪个版本、上一版本在哪里、是否启用、运行进程如何管理。
```

## 后续实现顺序

当前 `plugin package` 已完成：

1. 优先读取并校验 `plugin build` 生成的 `release/manifest.json`，再生成包内 `manifest.json`；Compose 插件缺少构建产物时拒绝打包。
2. 使用包内 `checksums.txt`，不再生成外置 `.sha256`。
3. 从 `frontend/dist` 读取构建结果，发布包写入 `frontend/`，排除 `.vite/` 构建元数据。
4. 将 `release/bin/<plugin-code>` 或 `bin/<plugin-code>` 归一为包内 `bin/<plugin-code>`。

后续继续按以下顺序落地：

1. `install/update` 读取 `release.json`、校验 `checksums.txt`，再校验 Manifest。
2. 接入 `signature sign/verify`，对 `checksums.txt` 生成和验证 Ed25519 签名。
