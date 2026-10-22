# 插件发布审核源码包

插件发布审核源码包用于 `sparq-tool plugin publish` 向平台提交源码。平台收到包后校验归档与清单，并创建 72 小时的上传会话；只有候选产物全部上传并通过 finalize 后，版本才进入人工审核。正式 Artifact 的重新构建和签名由独立可信构建器负责。源码包不是本地安装包，也不直接交给 `install/update` 使用。

## 与发布包的区别

两类包的用途不同：

```text
源码上传包：开发者 -> 平台，用于审核，并作为后续远程构建的输入。
发布包：构建结果 -> 宿主，用于本地安装和更新。
```

源码上传包包含源代码和构建输入；发布包包含运行产物和校验信息。

## 命令边界

```bash
sparq-tool plugin publish [path]
```

命令执行前需要通过 `sparq-tool auth login` 登录 Sparq Cloud。登录态保存在用户级认证 Profile 中，不写入插件目录。`[path]` 默认为当前目录，必须包含有效的 `plugin.yaml` 和 Go module。命令在修改版本和构建前检查 Profile；登录态缺失或过期时不会改动插件目录。

命令按以下顺序执行：

1. 确定本次发布版本，写回 `plugin.yaml` 并重新生成 `manifest_gen.go`。
2. 执行完整 `plugin build`，生成并校验 `release/manifest.json`。
3. 纯前端插件生成一个 `frontend_bundle`；包含 Remote Runner 的插件按六个平台分别生成同时包含 Runner 与 `frontend/` 构建产物的 `managed_remote` 组合包。
4. 生成临时 `tar.gz` 审核源码包并计算 SHA-256。
5. 上传审核源码包并取得 `source_upload_id` 和平台记录的源码 SHA-256。
6. 逐个上传第 3 步的 package v1 候选包，并绑定同一源码上传记录和源码摘要。
候选包使用确定性归档元数据；源码、Manifest 和构建产物未变化时，重复执行发布会得到相同的候选包 SHA-256，从而复用平台幂等结果。
7. 调用 finalize 原子核对并冻结源码与候选清单，使版本从 `validating` 进入 `review_pending`。
8. 返回源码上传记录、候选 Artifact 数量，以及每个候选包的 ID、目标平台、摘要、校验状态和信任决策。

上传完成后，CLI 会核对平台登记的源码版本、源码 SHA-256，以及每个候选包的来源记录、版本记录、SHA-256、格式、类型、Runtime 和目标平台。任一字段与本地构建结果不一致时，命令不会报告发布成功。

在交互终端中不传版本参数时，命令显示当前版本，并提供保持版本、递增 Patch/Minor/Major 和自定义版本五种选择；直接回车默认递增 Patch。脚本和 CI 必须显式使用以下一种版本策略：

```bash
sparq-tool plugin publish . --bump patch -m "新增文章审核"
sparq-tool plugin publish . --version 2.0.0-beta.1 -m "重构插件运行时"
sparq-tool plugin publish . --version 1.2.3
```

`--bump` 和 `--version` 互斥。`--version` 接受完整 SemVer；需要沿用当前版本时显式传入 `plugin.yaml` 中的当前版本。`plugin.yaml` 仍是版本号、应用代码和 Sparq 兼容约束的唯一事实来源，命令行参数会在构建前真实更新该文件。版本写入后，即使构建、打包或上传失败也不会自动回滚，也不会自动创建 Git commit 或 tag。首个版本可以省略版本说明，平台保存为“首次发布”；应用已有其他版本时使用 `-m/--message` 提供版本说明。

发布命令固定使用认证配置中的当前 profile。源码上传幂等键由本地发布会话自动生成和维护；候选构建包使用自身 SHA-256 作为独立幂等键，因此两类文件可以分别重试。这些状态不作为公开命令行参数，避免调用方破坏恢复关系。

命令在改写版本号之前，将目标版本、attempt key、源码摘要和 `source_upload_id` 等恢复元数据写入 `$SPARQ_HOME/sparq-tool/publish-state/`。目录权限为 `0700`，状态文件为 `0600`，不保存 Token 或包内容。中断后再次执行同一目录、endpoint 和 profile 的发布命令，会恢复目标版本、查询服务器已有候选并只补传缺失项，不会重复 bump。finalize 成功后状态文件自动删除。

源码包上传成功但候选构建包或 finalize 失败时，命令不会伪装为完整成功，源码记录仍保持 `uploading`。错误会输出 `source_upload_id` 和 `source_sha256`；修复可重试问题后执行相同命令即可继续。

同一版本已经进入校验或审核流程后，平台不会用不同幂等键提交的新源码静默替换审核快照。CLI 会显示插件 code、版本号，并提示提升 `plugin.yaml` 版本号；版本被审核驳回后，可以修复源码并重新提交原版本。

## 源码包命名

默认文件名：

```text
<plugin-code>-<version>-source.tar.gz
```

示例：

```text
cms-0.1.0-source.tar.gz
```

`plugin-code` 和 `version` 仍然来自 `plugin.yaml`，不从目录名推导。

## 源码包根目录

源码包解开后保留一层顶级目录：

```text
<plugin-code>/
├── plugin.yaml
├── manifest_gen.go
├── go.mod
├── go.sum
├── bootstrap.go
├── internal/
├── cmd/
├── schema/
├── frontend/
│   ├── package.json
│   ├── pnpm-lock.yaml
│   ├── vite.config.ts
│   └── src/
└── README.md
```

源码上传包保留顶级目录，是为了平台解压多个源码包时不会互相覆盖。发布包不保留顶级目录，是为了安装阶段直接在 staging 根目录读取 `plugin.yaml`。

## 必须包含

- `plugin.yaml`
- `manifest_gen.go`
- `go.mod`
- Go 源码目录，例如 `cmd/`、`internal/`、根 package 文件
- `schema_resources[].file` 引用的 JSON 文件
- 插件前端源码目录；如果插件声明 frontend entry，则至少包含 `frontend/package.json` 和 `frontend/src/`

## 可以包含

- `README.md`
- `docs/`
- 示例配置
- 测试文件
- 前端锁文件，例如 `pnpm-lock.yaml`
- 可选的 `.sparqignore`

## 不应包含

- `bin/`
- `release/`
- `frontend/dist/`
- `frontend/node_modules/`
- `.git/`
- `.env` 和非示例环境文件
- `*.log`、`*.sqlite`、`*.sqlite-shm`、`*.sqlite-wal`
- `checksums.txt`
- `release.json`
- `signatures/`

## 使用 `.sparqignore`

插件根目录可以创建 `.sparqignore`，额外排除不属于审核构建输入的本地文件。规则逐行处理，支持空行、`#` 注释、`!` 反选、以 `/` 锚定插件根目录、以 `/` 结尾匹配目录，以及 `*`、`?`、`**` 通配符：

```text
.idea/
coverage/
tmp/**
docs/**
!docs/publish-notes.md
```

后出现的规则覆盖先前规则。为了避免审核源码不可构建，`.sparqignore` 不能排除 `plugin.yaml`、`manifest_gen.go`、`go.mod`、`go.sum`、`sparq.build.yaml`、Manifest 引用的 Schema 文件和 `.sparqignore` 本身。`.sparqignore` 必须是插件根目录中的普通文件，不能是符号链接。内置安全规则也不能通过 `!` 反选：`.env`、`.npmrc`、`.pypirc`、`.netrc`、常见凭据 JSON、SSH 私钥、`*.key`、`*.p12`、`*.pfx`、`.git/`、`.ssh/`、`.aws/` 和 `.docker/` 始终不会进入源码包。需要作为插件资源交付的公开证书不应使用私钥扩展名。

## 平台构建职责

当前平台收到源码上传包后负责：

- 校验 ZIP、TAR 或 TAR.GZ 归档边界和大小。
- 从 `plugin.yaml` 解析应用代码、版本号和 Sparq 兼容约束。
- 校验当前登录用户是应用所属开发者的启用成员。
- 在同一事务内创建版本、登记源码并进入 `validating/uploading`。
- finalize 时原子冻结候选清单，完成后才进入 `review_pending`。

源码上传包不承诺可直接运行。package v1 先保存到 `marketplace_candidate_artifacts`。平台开发者主体上传的 `frontend_bundle` 或完整六平台 `managed_remote` 通过结构校验后进入 `internal_review`；人工审核同时核对源码摘要，审核通过后将候选包提升为 `trusted` 并登记到 `marketplace_artifacts`。普通开发者主体上传的候选包仍为 `rebuild_required`，由后续平台构建服务基于同一源码摘要重新构建。`plugin_package` 兼容候选包不会直接提升。
