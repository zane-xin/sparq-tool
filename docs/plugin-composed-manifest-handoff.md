# 插件完整 Manifest 构建交接

## 目标

让使用 Go `manifest.Compose` 的插件通过标准命令完成构建和打包，不再依赖插件私有发布脚本或手工传递 `--manifest-json`：

```sh
sparq-tool plugin build <plugin-root>
sparq-tool plugin package <plugin-root>
```

CMS 的 `scripts/release.sh` 和 `cmd/manifest` 已删除。标准工具链必须直接调用插件根包 `Manifest()`，不能依赖插件仓库提供第二个发布命令。

## 当前状态

CMS 的 Manifest 由以下来源共同组成：

```text
plugin.yaml
    -> manifest_gen.go
    -> manifest.Compose
    -> applyFrontendArtifacts
    -> plugin.Manifest
```

- `plugin.yaml` 只保存插件身份、介绍和协议兼容范围。
- `manifest.Compose` 加入组件、Capability、Schema Resource、菜单、设计扩展和其他宿主 Contribution。
- `frontend/artifact.json` 保存 Designer Module 构建产物的版本、入口和文件摘要；`manifest generate` 会把它写入 `manifest_gen.go`。
- CMS 根包公开 `Manifest()`，这是工具取得最终 `plugin.Manifest` 的唯一 Go 入口。
- `plugin build` 会在前端构建完成后生成并检查 `manifest_gen.go`，并将完整协议 Manifest 原子写入 `release/manifest.json`。有 Go module 时工具临时调用根包 `Manifest()`，没有 Go module 时使用基础协议清单。
- `plugin package` 默认优先读取 `release/manifest.json`；只有兼容旧流程时才回退到 `plugin.yaml`，使用 `manifest.Compose` 的插件缺少构建产物会拒绝打包。

执行 `plugin build` 后再执行 `plugin package`，CMS 包内的 `manifest.json` 来自根包 `Manifest()` 的完整结果，包含 Go `manifest.Compose` 提供的运行时声明。

## 已移除入口

`plugins/sparq-plugins-cms/scripts/release.sh` 原来依次执行前端构建、Manifest 生成、Go Manifest 导出、后端构建和旧版打包命令；`cmd/manifest` 只负责把根包 `Manifest()` 编码为 JSON。两者都属于插件私有构建入口，现已删除。

清理私有入口不表示完整 Manifest 问题已经解决。在标准工具链通过本文验收前，不能把从基础 `plugin.yaml` 直接生成的包作为 CMS 发布产物。

## 推荐实现

### 1. 将完整 Manifest 定义为构建产物

`plugin build --target all` 完成后，在插件目录生成：

```text
release/manifest.json
```

该文件必须是经过 `plugin.ValidateManifest` 校验的最终 `plugin.Manifest`，不能只是 `plugin.yaml` 的协议转换结果。

对于具有 `go.mod` 的 Go 插件，工具读取模块路径和根包名称，在插件目录内创建一次性 Go exporter。Exporter 导入插件根包、调用 `Manifest()` 并把结果编码到标准输出；工具使用 `go run -buildvcs=false <temporary-main.go>` 执行，完成后删除临时目录。插件根包为 `main`、不能导入或没有生成的 `Manifest()` 时应明确失败。

临时 exporter 只由工具生成，不能写入插件源码目录的稳定文件集合，也不能进入发布包或源码审核包。工具捕获标准输出、限制最大字节数、解析为 `plugin.Manifest`，校验 code 和 version 与 `plugin.yaml` 一致，然后原子写入 `release/manifest.json`。Exporter 的标准输出只能包含 JSON，诊断信息应写入标准错误。

对于不存在 `go.mod` 的声明式插件，工具继续使用 `manifest.Manifest.ProtocolManifest()` 生成标准协议对象，并写入相同路径。这样 `plugin package` 不需要根据插件类型选择两套输入来源。

### 2. 固定构建顺序

`--target all` 必须遵循以下顺序：

```text
加载并校验 plugin.yaml
    -> 构建 frontend/dist 和 frontend/artifact.json
    -> 生成并检查 manifest_gen.go
    -> 执行 Go 测试
    -> 构建 Remote Runner
    -> 导出并校验 release/manifest.json
```

不能在前端构建之前生成最终 `manifest_gen.go`。否则 Designer Module 的入口、版本或文件摘要仍是上一次构建结果，Remote Runner 和包内 Manifest 会携带过期数据。

目标模式建议按以下规则处理：

- `--target all`：执行完整顺序，并生成可供打包的 `release/manifest.json`。
- `--target go`：使用当前已有的前端产物生成 Manifest，再测试和构建 Go，最后导出完整 Manifest；缺少声明所需的前端 artifact 时明确失败。
- `--target frontend`：只构建前端并更新 Manifest 生成文件，不把已有 `release/manifest.json` 宣称为本次完整构建结果。

### 3. 让 package 自动消费构建产物

`plugin package` 应优先读取 `<plugin-root>/release/manifest.json`，执行现有的协议校验和身份一致性检查，再交给 `packager.PackagePlugin`。

建议保留 `--manifest-json` 一个兼容周期：

- 显式参数存在时使用参数指定文件。
- 未提供参数且 `release/manifest.json` 存在时自动使用构建产物。
- Go 插件的完整 Manifest 构建产物缺失时拒绝打包，并提示先执行 `plugin build`。
- 声明式插件的构建产物缺失时，可以暂时回退到 `plugin.yaml` 转换结果，保持无 Go 插件兼容。

`plugin package` 不应自行执行 `go run`、前端构建或 Go 编译。该命令只消费已经完成的构建产物，避免打包阶段产生未展示的代码执行和产物变化。

## 建议修改位置

- `sparq-tool/internal/plugin/buildpipeline/build.go`
  - 调整 frontend、Manifest 生成和 Go 构建顺序。
  - 增加临时 exporter、完整 Manifest 校验和原子写入。
  - 在 `Result` 中返回 Manifest 产物路径。
- `sparq-tool/internal/plugin/manifest/generate.go`
  - 复用或提取现有 Go module 路径和根包识别逻辑，避免 build pipeline 再实现一套解析。
- `sparq-tool/cmd/internal/plugin/build.go`
  - 输出完整 Manifest 产物位置。
- `sparq-tool/cmd/internal/plugin/package.go`
  - 自动解析 `release/manifest.json`。
  - 保留并明确 `--manifest-json` 的覆盖和兼容行为。
- `sparq-tool/internal/plugin/buildpipeline/build_test.go`
  - 覆盖构建顺序、导出失败、身份不一致和普通声明插件。
- `sparq-tool/cmd/internal/plugin/package_test.go`
  - 覆盖自动消费、显式覆盖、缺失构建产物和错误 Manifest。
- `plugins/sparq-plugins-cms/README.md`
  - 保持 `plugin build`、`plugin package` 为唯一发布入口。

## 验收条件

### 工具单元测试

1. 普通声明插件没有 `go.mod` 时，`plugin build` 生成可校验的 `release/manifest.json`。
2. Go 组合插件只公开根包 `Manifest()` 时，工具通过临时 exporter 获得组合结果，而不是使用基础 YAML 转换结果。
3. 导出结果不是 JSON、超过大小限制、协议校验失败或 code/version 不一致时构建失败。
4. 前端构建先于 Manifest 生成；测试应检查命令和文件生成顺序。
5. `plugin package` 默认消费构建产物，显式 `--manifest-json` 按约定覆盖。
6. Go 插件的构建产物缺失时，打包不能静默退回基础 Manifest。
7. 临时 exporter 在成功和失败后都会清理，并且不会进入发布包或源码审核包。

### CMS 集成验收

在 CMS 插件目录只执行：

```sh
../../bin/sparq-tool plugin build .
../../bin/sparq-tool plugin package .
```

解开 `release/cms-0.1.0.tar.gz` 后检查 `manifest.json`，至少确认：

- `content`、`admin` 两个组件存在。
- `content.host_dependencies` 包含 `database`。
- Capability、Provider 和 Route 声明存在。
- 11 个 CMS Schema Resource 全部存在。
- 栏目、文档模型、文档和专题资源保留 `runtime_owner=domain`。
- 菜单、设计扩展和 trusted frontend root 声明存在。
- Frontend root artifact 的入口、版本和文件 SHA-256 与本次 `frontend/dist` 一致。
- 包内 `checksums.txt` 覆盖 `manifest.json`、Remote Runner、前端文件和 Manifest 源文件。

还应运行：

```sh
cd sparq-tool && go test ./cmd/internal/plugin ./internal/plugin/buildpipeline
cd plugins/sparq-plugins-cms && go test ./...
```

## 非目标

- 不在 `plugin package` 中重新构建源码。
- 不让安装端执行临时 exporter 或任何插件源码。
- 不改变 `plugin.yaml` 只保存基础元数据的当前 CMS 结构。
- 不把 Go `manifest.Compose` 的内容复制回 YAML 形成双重事实来源。
- 不在插件仓库重新增加稳定的 Manifest 导出命令；导出适配完全属于 `sparq-tool` 构建阶段。

## 完成后的文档清理

实现完成后同步更新：

- CMS README：工具验收通过后删除当前阻塞提示，只保留两个标准命令及产物说明。
- `sparq-tool/docs/commands.md`：说明 `plugin build` 生成完整 Manifest，`plugin package` 默认消费构建结果。
- `sparq-tool/docs/plugin-package-format.md`：把 `release/manifest.json` 明确为打包输入产物。
- `sparq-tool/docs/toolchain-roadmap.md`：将“明确 manifest 拷贝策略”更新为已实现行为。
