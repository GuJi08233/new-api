# 2026-10-22 安全修复与公开漏洞复核

本次以 `9d35792e0` 为基线，修复 eMudhra 证书信任更新遗漏的构建入口，并查询 Node.js、Bun、GitHub Advisory Database 和 Go Vulnerability Database。检查对象是当前工作区源码、锁文件和本地构建，不包含线上部署、历史发布包或生产数据。

## 构建信任修复

- `go.mod`、Docker 和正式发布工作流统一使用 Go **1.27.3**。使用强制最低版本，避免只有 CI 更新、源码仍能用 Go 1.25.1 编译。选择 1.27.3 也排除了尚未包含修复的 1.27.0–1.27.2。
- Electron 构建使用 Node **24.22.1**，本地 `engines`、`engine-strict` 与打包 hook 共同检查。Node 24.22.0 不是此次官方安全发布版本；Electron 的独立回移修复不能用来推断宿主 Node 已修复。
- Docker 前端构建及发布工作流固定 Bun **1.4.4**。该版本于 2026-10-22 发布，更新 NSS 3.131.1 根证书，并包含 TLS、Brotli 和包管理器等安全修复。Go/Bun Docker 摘要从 Docker Registry 实际查询。
- Electron 在打包前及复制后检查后端可执行文件的 Go build info，拒绝旧版本、预发布版本和无法验证的元数据。检查不运行后端，也不要求手工打包者安装 Go。
- Electron ZIP 使用锁定 npm 包内置的 SHA-256 校验值，缓存与新下载均校验；校验失败中止构建，不回退到从同一下载站取得的 `SHASUMS256.txt`。

本次只使用临时目录中的工具和测试产物，没有升级系统全局 Node/Bun，也没有覆盖工作区已有的 `new-api.exe` 或 `new-api-dev.exe`。

## Go 依赖

使用 `govulncheck v1.9.0` 检查生产入口 `.`，避免将前端依赖目录内的 Go 示例误算作应用入口。初始 Linux/amd64 与 Windows/amd64 结果均命中 20 份有效公告，其中 11 份具有保守静态调用链。

| 模块 | 修复前 | 修复后 |
| --- | --- | --- |
| github.com/gorilla/websocket | v1.5.0 | v1.5.3 |
| golang.org/x/image | v0.41.0 | v0.45.0 |
| golang.org/x/net | v0.55.0 | v0.60.0 |
| golang.org/x/crypto | v0.52.0 | v0.57.0 |
| golang.org/x/text | v0.37.0 | v0.42.0 |
| github.com/klauspost/compress | v1.18.3 | v1.18.7 |
| go.opentelemetry.io/otel、otel/trace | v1.41.0 | v1.44.0 |

`x/sync`、`x/sys` 等模块按上述模块的兼容依赖要求一并更新。没有根据扫描结果盲目升级整个模块图。

修复后 Linux/amd64 与 Windows/amd64 生产入口扫描都只剩 **GO-2026-5932**：`golang.org/x/crypto/openpgp` 已弃用且没有修复版本。当前应用不导入该包，扫描记录仅包含模块及版本，没有包或符号调用链。因此这是模块层面的残留公告，不是已证实包含在应用中的漏洞。

初始扫描也有需要区分的适用条件：WebP `DecodeConfig` 不等同于完整图像解码；部分公告仅影响 32 位平台；PRECIS `Nickname` 与数据库认证使用的 `OpaqueString` 不同；HTTP/2 接口调用图不证明当前 HTTP 监听配置启用了受影响的实现。已升级这些依赖，但不将初始 20 份公告表述成 20 个可利用漏洞。

## 前端依赖

以 `web/classic/bun.lock` 为权威锁文件，`bun audit --json` 从 **42 个受影响包、118 个去重公告** 降至 **1 个包、1 个公告**。

| 主要依赖 | 修复前锁定版本 | 修复后 |
| --- | --- | --- |
| DOMPurify | 3.4.13 | 3.4.17 |
| Mermaid | 11.6.0，嵌套 11.14.0 | 11.17.2 |
| React Router DOM | 6.28.1 | 7.18.4 |
| Vite | 5.4.11 | 6.4.5 |
| KaTeX | 0.16.22，嵌套 0.16.45 | 0.18.2 |
| PostCSS | 8.4.49 / 8.5.3 | 8.5.31 |
| 嵌套 Axios | 1.16.0 | 1.20.0 |
| decode-uri-component | 0.4.1 | 0.5.0 |
| ws | 多个旧 8.x 副本 | 8.22.0；WalletConnect 的 7.x 分支保留 7.5.13 |

同时更新相关 Babel、lodash、minimatch、brace-expansion、minimist、js-yaml、immutable、nanoid 和 Rollup 传递依赖。只对上游固定的旧副本使用 overrides；没有修改业务 JSX。

**剩余：CVE-2026-93687 / GHSA-vfj7-8cjw-p6xm，braces ≤ 3.0.3。** 官方公告的修复版本为 None，npm 最新版本仍为 3.0.3。深度嵌套的攻击者可控 brace/glob 表达式可能使递归 AST 处理耗尽调用栈。当前依赖链来自 Tailwind、Sass/watch 工具；应用 `src` 未导入该解析器，未发现线上请求输入可达路径。本次没有隐藏审计告警，也没有为消除告警私自引入未审查的替代实现。构建源文件及 glob 配置应继续由仓库维护者控制，后续跟进上游补丁。

React Router 是本次需要注意的主版本升级：现有入口已启用相关 v7 future flags，React 18 满足依赖要求；生产构建、参数路由匹配和 MemoryRouter/Routes/Link 渲染检查通过。

## Electron 构建依赖的剩余公告

Electron npm 审计仍显示 9 条依赖链告警，但它们来自两个根公告，不是 9 个独立漏洞。这些依赖用于构建，当前打包白名单不将它们作为应用运行依赖分发。

| 根公告 | 核实结果与处理 |
| --- | --- |
| CVE-2026-97058 / GHSA-hp3w-g68c-fv3c，sprintf-js ≤ 1.1.3 | 无上游修复版本。利用需要控制格式字符串；当前 global-agent/roarr 链使用固定格式、网络数据作为结构化参数，未发现线上用户输入可达的格式字符串入口。不采用 audit 建议的 electron-builder 降级 |
| CVE-2026-93748 / GHSA-ch52-4w7c-c8xp，http-cache-semantics ≤ 4.2.0 | 维护者明确争议该报告，说明 RFC 允许缓存带 Set-Cookie 的响应，禁止共享应使用 private。本项目此处也不是面向多用户的共享响应缓存。没有通过改成 4.3.0 仅消除版本告警后声称修复；保留原锁项及争议说明 |

Electron 44.9.3 已含此次证书事件的独立回移补丁；本次没有仅因它仍报告 Node 24.22.0，就把宿主 Node 24.22.1 的所有 CVE 都认定为 Electron 可利用漏洞。官方发布说明与 npm 公告不能替代具体代码路径分析。

## 项目公开公告与当前代码对照

通过 GitHub 公共 API 查询到 15 份项目安全公告，并按公告给出的具体路径核对。以下为源码对照结果，不表示对所有可能变体完成了渗透测试。

| 公告 | 当前代码证据 |
| --- | --- |
| CVE-2026-71479：计费溢出 | 图片数量、视频时长和 max_tokens 族已有上界，额度转换集中到 `common/quota_math.go`；本次未重新穷举所有适配器变体 |
| CVE-2026-64865：Redis 额度覆盖 | `UpdateSelf` 使用字段级设置更新；`updateUserCache` 失效快照，额度从账务主库读取 |
| CVE-2026-64868：匿名支付回调无界读取 | Stripe/Creem/Waffo 路由已使用 `AnonymousRequestBodyLimit` |
| CVE-2026-64866：管理员重置高权限 Passkey | `AdminResetPasskey` 在凭据操作前调用 `canManageTargetRole` |
| CVE-2026-64859：用户列表泄露访问令牌 | 用户列表、搜索及普通详情查询均显式排除 `access_token`；不是仅依据项目版本号判断 |
| CVE-2026-41432：Stripe 空密钥与跨网关充值 | 回调先检查配置启用条件，检查 paid 状态，结算校验支付来源 |
| CVE-2026-42339：0.0.0.0 SSRF | `common/ssrf_protection.go` 已拒绝 0.0.0.0/8、IPv6 未指定地址等保留网段 |
| CVE-2026-44342：邮箱绑定 CSRF | 邮箱/微信绑定为 POST，且受登录身份及安全验证约束 |
| CVE-2026-32879：Passkey 二次验证绕过 | `UniversalVerify` 不接受仅声明 method=passkey；独立 WebAuthn 流程完成后才签发操作凭证 |
| CVE-2026-33655：通知域名解析绕过 | 默认开启域名 IP 检查，Webhook/Bark/Gotify 使用 SSRF 防护客户端 |
| CVE-2026-30886：视频跨用户访问 | `VideoProxy` 使用 `GetByTaskId(userID, taskID)` |
| CVE-2026-25802：Markdown HTML 预览 XSS | HTML 预览在不允许脚本的 sandbox iframe 内；DOMPurify/Mermaid 等依赖另行升级 |
| CVE-2026-25591：Token 搜索通配符 DoS | 搜索模式经过校验/转义，限制通配符、结果数量和用户令牌规模 |
| CVE-2025-62155：重定向 SSRF | HTTP 客户端对重定向目标重新检查 |
| CVE-2025-59146：初始 URL SSRF | 用户可控文件/通知 URL 使用 URL 校验与受保护的获取客户端 |

另外核对搜索结果中的 CVE-2026-9305（充值搜索）与 CVE-2026-9306（Midjourney 图片）：充值搜索使用参数绑定及模式校验；图片代理在查询任务前校验绑定任务行 ID 的签名，并非仅凭公开任务 ID 访问。

## 验证与限制

- 已复现旧 Go 1.25.1 被新的 `go.mod` 拒绝。
- Go 1.27.3 及升级后的依赖下，`go test ./common ./middleware ./controller ./model ./service ./relay/... ./setting -count=1 -timeout=300s` 通过，覆盖全部 relay 适配器测试。
- 前端 frozen lockfile 安装、完整生产构建、格式检查，以及路由、数学公式、严格模式 Mermaid、URI 解码定向行为检查通过。最终使用 Bun 1.4.4 与 Node 24.22.1 再次完成 frozen 安装和生产构建，复扫仍仅有 braces 公告。
- 包含新前端资源的 Windows/amd64 与 Linux/amd64 后端均完整构建成功，产物元数据显示 Go 1.27.3；Linux 构建使用与 Docker 一致的 `CGO_ENABLED=0` 和 `GOEXPERIMENT=greenteagc`。没有在本机执行 Docker 镜像构建。
- Electron 用真实 Go 1.25.1 / 1.27.3 编译的 PE、ELF、Mach-O 夹具执行 5 项检查，全部通过且没有跳过；旧后端被真实 electron-builder 调用拒绝，安全测试后端在独立目录完成 Windows dir 打包。该测试产物不是可发布的应用。
- Electron 离线 `npm ci`、安装 hook、固定校验值的缓存 ZIP 使用及旧 Node 拒绝检查通过。未在 Windows 宿主生成 macOS/Linux 完整安装包，未覆盖 universal fat Mach-O。
- 测试使用临时/内存 SQLite；没有连接生产 MySQL、PostgreSQL 或 Redis，也不将 SQLite 测试表述为三种数据库实测。
- 本地审计阶段没有发布、推送、部署或改动系统级软件。已有部署和旧安装包需要使用新配置重新构建后才能获得这些修复。

## 公开来源

- [Node.js 2026-10-21 安全公告](https://nodejs.org/en/blog/vulnerability/october-2026-security-releases)
- [Go 官方发布元数据](https://go.dev/dl/?mode=json)
- [Bun 1.4.4 安全与根证书更新](https://bun.com/blog/bun-v1.4.4)
- [new-api 官方安全公告](https://github.com/QuantumNous/new-api/security/advisories)
- [Go Vulnerability Database](https://vuln.go.dev/)
- [Gorilla WebSocket GO-2026-6278](https://pkg.go.dev/vuln/GO-2026-6278)
- [弃用 OpenPGP GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)
- [Vite 编辑器命令注入 GHSA-587h-7wmj-h5pm](https://github.com/advisories/GHSA-587h-7wmj-h5pm)
- [React Router GHSA-wrjc-x8rr-h8h6](https://github.com/advisories/GHSA-wrjc-x8rr-h8h6)
- [KaTeX GHSA-238p-pmpm-9mq7](https://github.com/advisories/GHSA-238p-pmpm-9mq7)
- [braces 尚无修复的公告](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
- [sprintf-js 尚无修复的公告](https://github.com/advisories/GHSA-hp3w-g68c-fv3c)
- [http-cache-semantics 公告](https://github.com/advisories/GHSA-ch52-4w7c-c8xp)
- [http-cache-semantics 维护者争议说明](https://github.com/kornelski/http-cache-semantics/issues/56#issuecomment-5975759591)
