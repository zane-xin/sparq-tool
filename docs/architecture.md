# sparq-tool 代码架构

## 目标

`sparq-tool` 将命令行适配与可复用能力分开组织：

```text
cmd -> internal capabilities -> sparq-protocol / Go standard library
```

`cmd/` 负责命令树和终端交互，顶层 `internal/` 负责不依赖 Cobra 的本地工具能力。两层都属于 module 私有实现，不作为公开 Go API 承诺兼容性。

## 目录

```text
cmd/
  internal/                 私有 Cobra 命令适配器
  sparq-tool/               sparq-tool 二进制入口

internal/
  artifact/                 下载、checksum 和 signature
  dev/                      插件开发配置、计划与联调会话
  plugin/
    installer/              插件安装
    lifecycle/              插件移除与恢复
    localplugins/           本地源码插件 Bundle 生成
    manifest/               Manifest 主清单解析、外部源文件合并、校验与生成
    scaffold/               插件和前端脚手架
    updater/                插件更新
  process/                  本地进程操作
  release/
    build/                  app 和插件构建
    packager/               插件主清单、引用文件与产物打包
  remote/
    builder/                远程构建服务客户端
    marketplace/            marketplace 会话
  watch/                    文件监听、进程替换和失败重启
  workspace/                本地源码插件接入环境检查
```

## 分层规则

`cmd/internal` 可以包含：

- Cobra 命令构造和子命令装配。
- 参数、flag 和默认 CLI 值绑定。
- Ctrl+C、SIGTERM 等终端生命周期转换。
- 面向用户的计划、状态和结果输出。
- 尚未实现命令的占位提示。

顶层 `internal` 可以包含：

- 配置解析、默认值合并和执行计划校验。
- 文件、构建、打包、安装和工作区操作。
- watcher、进程控制、健康检查和开发编排。
- 协议对象转换及远程服务客户端。

顶层 `internal` 不导入 Cobra，也不创建命令或直接解释 CLI flags。新增命令时应先确认是否已有可复用能力；新增执行逻辑时应先进入对应能力包，再由 CLI 适配器调用。

## 二进制边界

当前只提供 `sparq-tool` 二进制。服务端部署与本地开发在配置来源、权限、进程托管和发布流程上的差异尚未确定，因此暂不提供 `sparq-deploy`。部署产品边界明确后，再决定扩展现有命令树还是建立独立工具。

## 验证

目录调整后至少执行：

```bash
go test ./...
go test -race ./internal/watch ./internal/dev
GOOS=windows GOARCH=amd64 go test -exec /usr/bin/true ./...
```

同时使用以下检查保证 Cobra 不反向进入能力层：

```bash
rg 'github.com/spf13/cobra' internal
```

预期无匹配。
