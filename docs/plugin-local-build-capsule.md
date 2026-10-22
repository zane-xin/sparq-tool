# Local 加密构建胶囊

Local 加密构建胶囊用于让已审核插件源码进入后续受控的用户侧宿主编译流程，同时避免把审核源码包作为普通归档直接交给用户。当前已经实现 `sparq.plugin.source-capsule.v1` 的生成、加密、签名、可信构建器登记、授权解析和下载；密钥释放、隔离编译和宿主安装事务尚未实现。

## 生成胶囊

独立构建器初始化时可以生成开发密钥：

```bash
sparq-tool builder capsule keygen --output-dir ./builder-keys
```

该命令生成 RSA-3072 加密密钥对和 Ed25519 签名密钥对。私钥权限为 `0600`，公钥权限为 `0644`，任何目标文件已存在时都会拒绝执行。

使用人工审核记录中的应用代码、版本号和源码 SHA-256 生成胶囊：

```bash
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

`--expected-source-sha256` 必须与输入文件的实际 SHA-256 完全一致。输出文件已存在时命令拒绝覆盖。源码包最大 100 MiB，完整胶囊最大 128 MiB。

`keygen` 只适合本地开发和独立构建器初始化。生产环境的加密私钥和签名私钥应由 KMS、HSM 或等价的受控密钥服务托管，不能进入普通开发者 CLI、源码仓库或 Website 运行目录。

## 安全目标

第一阶段采用 `sandboxed` 保护等级，目标是：

- 源码不以明文文件进入用户工作区。
- 解密源码、Go 构建缓存和临时文件只存在于一次性隔离环境。
- 插件不能提供任意构建命令、Shell 脚本或外部网络访问。
- 构建只导出宿主二进制、必要前端产物和构建证明。
- Build Session 与用户授权、插件版本、源码摘要、应用实例和构建环境摘要绑定。

该等级增加源码提取成本并避免普通文件泄漏，但不能对抗完全控制宿主并能够调试进程内存的管理员。运行心跳用于授权观察和兼容性诊断，不是源码保密机制。

## Artifact 边界

```text
kind                 = local_source_capsule
runtime              = local
target_os            = ""
target_arch          = ""
format               = sparq.plugin.source-capsule.v1
protection_level     = sandboxed
source_sha256        = 人工审核源码摘要
builder_environment  = 签名构建环境摘要
```

Local 胶囊与 Remote 二进制共用 Marketplace Artifact、授权和 Storage 下载边界。胶囊不按 OS/Arch 生成多个变体；目标平台由本地构建环境处理。

胶囊是未压缩 TAR，固定包含：

```text
capsule.json
payload/source.archive.enc
keys/source-key.rsa-oaep-sha256
keys/signing-public.pem
signatures/release.ed25519
```

源码使用随机 32 字节数据密钥和 AES-256-GCM 按 1 MiB 分块加密；数据密钥使用 RSA-OAEP-SHA256 包装。Ed25519 签名覆盖清单、包装后的数据密钥、内嵌签名公钥和密文摘要。协议解析器会拒绝未知归档条目、重复条目、大小不一致、摘要不一致、签名失败和归档尾部附加数据。

## 交由独立构建服务登记

平台管理员先创建可信构建器、登记 Ed25519 公钥，并签发构建器 Service Token。Token 明文只在签发响应中返回一次，服务端只保存 SHA-256；默认有效期为 90 天，最长 365 天。Artifact 登记需要 `marketplace.artifact.register` scope。

`sparq-tool` 到生成胶囊为止，不提供平台 Artifact 登记命令，也不保存或读取构建器 Service Token。胶囊必须交给独立部署的平台构建服务；该服务完整校验胶囊，并使用胶囊 SHA-256 作为 `Idempotency-Key`：

```http
POST /builder-api/v1/artifacts
Authorization: Bearer <builder-service-token>
Idempotency-Key: <capsule-sha256>
Content-Type: multipart/form-data

file=<cms-1.2.3-local.sparq>
```

具备 `website.marketplace.review` 权限的平台管理员仍可使用应急内部接口：

```http
POST /api/internal/admin/marketplace/versions/{id}/artifacts/local-source-capsule
Content-Type: multipart/form-data

file=<cms-1.2.3-local.sparq>
```

Website 在写入 Storage 前校验胶囊协议，并确认版本为 `approved`、存在对应人工审核通过记录，且胶囊中的应用代码、版本号、源码大小和 SHA-256 与审核快照一致。胶囊签名的 `key_id` 和公钥 DER SHA-256 必须匹配当前有效的可信公钥；构建器接口还要求公钥属于当前 Token 对应的构建器。文件以 `private` 可见性和 `marketplace-artifact` 分组写入 Storage，随后登记 `local_source_capsule` Artifact 并更新版本主 Artifact。

同一版本、类型、runtime 和目标平台构成 Artifact 变体。相同变体和相同胶囊 SHA-256 的重复请求返回已有记录；相同变体但摘要不同会返回冲突，不会覆盖已登记文件。数据库登记失败或并发幂等请求产生了多余 Asset 时，Website 会归档本次新上传的资源。

Website 只校验和登记胶囊，不生成胶囊、不持有解密私钥，也不执行构建。普通开发者登录态不能登记 `verified` Artifact。

## 历史设计（不属于当前部署流程）

```text
sparq-tool plugin download <code>
  -> 下载并校验授权 Artifact
  -> 交给 Sparq Admin 执行安装或更新事务
  -> 由宿主保存当前插件包并启动托管进程
```

构建环境默认禁止外部网络，不运行 `go generate`，不执行插件自定义脚本。依赖只能来自构建策略允许的内容寻址缓存。已安装插件在 Cloud 暂时不可用时继续运行；重新构建和升级需要新的 Build Session。

`sparq-tool` 不负责生产安装、更新、卸载、启停或重启，也不保存宿主 Session、CSRF 或部署授权。以上流程只描述未来需要单独评审的受控构建能力，不是当前 CLI 的安装命令。

## 与源码上传包的区别

开发者上传的源码包用于平台校验和人工审核，不能直接提供给最终用户。Local 胶囊由独立构建能力基于审核后固定的 `source_sha256` 生成和加密。Marketplace 下载解析会再次确认胶囊绑定的源码摘要与当前版本一致。
