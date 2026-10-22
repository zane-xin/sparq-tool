# sparq-tool 文档

当前文档用于记录 `sparq-tool` 的命令边界、实现状态和后续业务补齐计划。

## 文档列表

- [代码架构](architecture.md)：说明 CLI 私有适配层、可复用能力层、依赖方向和二进制边界。
- [命令说明](commands.md)：说明当前命令树、已具备基础实现的命令、预留入口和后续业务完善方向。
- [工具链路线](toolchain-roadmap.md)：记录开发、打包、部署、更新和运行守护的一条龙目标，以及当前暂缓完整实现的决策。
- [插件发布包格式](plugin-package-format.md)：定义 package v1 的根目录、必需文件、校验和签名规则。
- [插件发布审核源码包](plugin-source-upload.md)：说明 `plugin publish` 向平台提交源码时的包内容、安全边界和参数。
- [Local 加密构建胶囊](plugin-local-build-capsule.md)：说明受控本地编译的安全目标、胶囊交付结构和后续实现边界。
