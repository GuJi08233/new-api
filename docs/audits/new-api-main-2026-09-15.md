# new-api 与 main 分支修复审计

审计日期：2026-09-15。结论针对本地分支快照，未 fetch 远端。
main：37197dbca4192ff223fdada2426185efc39bdcd5
new-api：69a50029819a26c53e6babd276d49cfe2f8880ad
共同祖先：a63364d156cf2a64f1c3d1ee4923d73d5f3222a1

new-api 在共同祖先后有249个提交，main独有261个提交。已逐条筛查这249条提交记录与变更文件范围，并对候选安全、资金、稳定性和协议修复读取相关diff、追踪main实际调用链。大重构只深入与main现有行为有关的部分，不能将此报告理解为249个提交所有文件均已逐行安全审计。末尾索引为每条提交给出判断和审查边界。

本次仅审计和创建此报告；没有merge/cherry-pick、修改业务代码、切换分支、升级依赖、访问生产数据或执行迁移。hhhl未参与对比。已有未跟踪.playwright-mcp目录保持原样。

## 主要结论

原清单的七个高风险主题均能在main找到未完整处理的路径，但问题范围和风险描述需要修正；另外还发现会话撤权、登录第二因子、敏感操作证明和旧用户快照等应优先处理的问题。应按main现有结构移植修复机制，不能整提交批量cherry-pick。

| 优先修复主题 | 上游依据 | main证据入口 | 触发条件和边界 |
| --- | --- | --- | --- |
| 钱包/令牌并发初始预扣、缓存覆盖 | ccd535ef8 | service/billing_session.go:375；service/quota.go:393；model/token_cache.go:11 | 并发检查再扣减；无Redis也存在，缓存/批量记账/副本读会扩大窗口 |
| epay重复/漏入账 | 50e5377ea | controller/epay_notify.go:61 | 订单成功与加余额分离；多实例/人工补单并发可重入账，单实例崩溃或失败也会漏充 |
| tiered跨组重试错费/免费切付费 | df43f8015、cfaba1dd6 | controller/relay.go:195；service/tiered_settle.go:97 | 冻结旧分组倍率；免费转付费未建立预扣会话；main独立分组表达式还需另行适配 |
| Suno重复退款 | e0d515611、bc14c18f6 | service/task_polling.go:280 | 退款在持久化之前；main有轮询租约，不能说多实例必然重叠；任务级CAS仍缺 |
| 响应头等待长期挂起 | b518d0033 | service/http_client.go:74、165、210 | 无总超时且context未取消，上游连接不结束也不返回响应头 |
| classic时间规则全天套倍率 | ac381acf4 | web/classic/src/pages/Setting/Ratio/components/requestRuleExpr.js:214 | 普通同日区间生成OR恒真式；已用当前函数复现，9–17全天倍率2 |
| 注销账户无重新认证 | 6f2333990 | controller/user.go:936；router/api-router.go:99 | 有效会话即可软删账号；HttpOnly阻止脚本读Cookie，但同源脚本可代发请求 |
| 封禁/降权后旧Cookie仍有效 | 31d70fca3 | middleware/auth.go:45、278 | 普通控制台会话信任30天快照；只查status/role还不足以实现改密后撤销所有旧会话 |
| 第三方登录绕过本地TOTP | 6f2333990 | controller/oauth.go:134；controller/wechat.go:124；controller/telegram.go:111 | 账户已开TOTP，但第三方主认证直接签发会话；main邀请码补登记路径也须统一 |
| 敏感绑定/因子替换、proof复用 | 45c3fbe8a、0973dc2b8 | controller/passkey.go:531；controller/secure_verification.go:145；controller/user.go:149 | 被劫持会话可绑定攻击者身份；验证标记不绑定操作且可被下一登录身份继承 |
| 旧User快照恢复并发安全状态 | d7992672a、0cd9dc85e | controller/oauth.go:175；controller/user.go:435；model/user.go:638 | 绑定/轮换token可能覆盖新role/status/password等；main EmailBind亦须只更新email |

“高优先级”是按账户/资金/服务连续性影响排序，不是声称这些都能由匿名远程用户直接利用；具体前提见各专题。Midjourney未收费任务仍带退款额度（58d4e9bd3）及重复工具调用（1086038f5）在使用相关能力时也应尽快处理，不能只按“中等统计/协议错误”忽视实际资金或工具副作用。

## 对原清单的关键纠正

1. 分叉附近上游有classic/default两套，但31d70fca3已经删除classic并将default移入web/。new-api当前顶端是web/src；main仍只有web/classic。不能把路径不存在当作业务修复不适用。
2. 注销是GORM软删除，并非已证实物理永久删除；Cookie已有HttpOnly。“172小时后OOM”是上游提交记载，不是main实测。
3. 0973dc2b8不能整体算已修：main已校验原密码和OAuth state，但敏感绑定、因子替换、重新认证和proof作用域仍缺。
4. 057f71c23不止reject_reason；普通错误日志还可能返回other中的channel_id/name/type。仅恢复一行delete不完整。
5. DOMPurify顶层3.4.11确实落后；公开公告所需IN_PLACE/hooks/custom-element等利用条件，在main当前直接调用中未确认。保留依赖维护建议，不写成已证明classic XSS。
6. 顶层图片n已参与按次预扣，JSON、multipart及Ali adaptor也已有128上界。缺口是Ali数量/倍率在预扣之后才解析、最终出站参数与预扣不一致及上游ImageCount缺界限。
7. 原生Responses已复制cached_tokens；问题集中在转换后的BillingUsage重新提取丢失缓存细项。退款不减used_quota主要造成统计失真，不等于一般任务额外获得可消费余额。
8. a20574136部分适用：main Go Ali adaptor也有Wan2.7-t2v协议问题。fa90b2312则属于上游新增校验的回归，main没有相同拒绝路径。
9. 610334dbd在main还需补高级自定义渠道的stream_options能力声明，否则复制上游条件分支不会生效。usage缺失可多收或少收，不能笼统说必然少收。
10. GORM实际仍1.25.2；8c8c4153d是升级至1.25.11+前必补，当前不能说统计已经恒零。旧PG约束问题同样依赖历史schema和驱动版本。
11. 不能只筛fix标题：0cd9dc85e、2d23cdf29、0ed497f06等标题为merge/feat，但包含对main有价值的局部修复。
12. 原“14项relay”应展开并扩充，详见协议专题R01–R23；其中与计费专题重叠的usage条目不重复计算问题数。

## 建议实施顺序与main约束

- 先分开修身份/会话安全、并发预扣与epay、Suno/MJ退款、tiered跨组计费、响应头超时和classic时间生成器。为每项保留能重现实际契约的回归用例。
- 再补敏感日志、充值容量、流式和桥接usage、GetBody、各实际使用渠道的协议错误。第三方登录和敏感操作验证需同时改classic交互，不能仅挂现有TOTP/Passkey中间件而锁死无此因子的普通账户。
- PG兼容改动按PrepareStmt、JSON Valuer/Scanner、GORM/驱动和双Scan防御一起验证；main的只读副本和ClickHouse日志入口不能遗漏。SQLite按部署实际DSN处理。
- 保留main的Pricing表、32位quota边界与饱和审计、自研订阅续期/限购、Ethereum晚到付款、读写分离、渠道归档/恢复、OA2及classic。a073f74b3的53位钱包和上游插件/前端大重构不是本次修复前置。
- 时间生成器修复不会自动重写已保存的恒真表达式；需要单独核查存量配置。CAS防重复也不等于资金与状态严格一次事务，崩溃漏退仍需明确兜底。

## 验证和覆盖范围

已完成：分支hash/共同祖先/249条提交核对；相关上游diff与main实际路径静态核验；Node直接加载classic时间表达式函数的确定性复现；go list -mod=readonly确认实际依赖版本；GORM官方版本源码及DOMPurify官方公告核对。

未完成也未声称完成：生产数据库/Redis并发实验、真实支付/供应商请求、浏览器安全PoC、全量Go测试或前端构建、每个大重构的逐行审核、Electron各依赖公告与桌面可利用性审核。部署是否启用相关provider/Redis/历史schema未知，已按条件描述。

以下专题给出详细证据，末尾249条索引明确“需要修”“部分适用”“已等价处理”“新功能/范围筛查”等判定。行号属于固定main快照，后续代码变化后需重新定位。

---




---

## 认证、权限、隐私和前端安全审计

审计基线：main = 37197dbca4192ff223fdada2426185efc39bdcd5；new-api = 69a50029819a26c53e6babd276d49cfe2f8880ad；分叉点 a63364d15。只读源码与提交差异，没有修改源码、切分支或访问生产数据。当前 new-api 已在 31d70fca3 删除 classic、将 default 平移到 web；main 仍只使用 web/classic。

本报告区分“已确认存在的代码缺陷”和“触发依赖部署、历史数据或已被劫持会话的安全加固”。没有实际运行攻击、数据库并发测试或浏览器 PoC。

### 一、需要修复，按主题归并

#### S1. 31d70fca3：控制台会话信任过时身份和权限（高优先级）

证据：
- middleware/auth.go:45-50 从 Cookie session 读 username/role/id/status；132-165 根据这份快照鉴权，没有回查用户当前状态。
- main.go:196-204 使用 cookie.NewStore，MaxAge=2592000（30 天）。这是 30 天有效的登录快照，不是“永不过期”。
- controller/user.go:149-157 签发快照；model/user.go:584-588、924 后密码修改/重置没有会话版本失效机制。
- middleware/auth.go:278-291 的 TokenOrUserAuth 也直接信任 session status。
- service/authz/resolver.go:13-16 的 superuser 分支使用传入旧 role；因此被降权的原管理员会话仍可通过相应管理权限，不能只描述为普通封禁延迟。

后果：封禁、角色降级、密码修改或注销之后，已签发 Cookie 仍可能被接受，直到过期。用户 API token 的 ValidateAccessToken 会查库，此缺陷主要针对 Cookie 控制台会话。

main 已有局部保护：middleware/auth.go:246-268 的 WebSocketUserAuth 实时查 GetUserCache 的 status，需保留。

最小方案：保留 Cookie/classic 架构，在所有 Cookie 鉴权入口使用当前主库/可靠缓存的 status、role、group，并验证用户仍存在；密码变更/删号/注销的立即撤销需要可服务端失效的 session id 或 auth_version。只回查 status/role 能修封禁和降权，不能覆盖改密后的旧 Cookie 失效。不必移植上游 token/session/UI 整体重构。

#### S2. 6f2333990：不同登录入口的 TOTP 策略不一致；Passkey 未统一要求 UV（高优先级）

证据：
- controller/user.go:82-110 只有密码登录查询 IsTwoFAEnabled；setupLogin:149-174 无统一因子策略。
- controller/oauth.go:134、controller/wechat.go:124、controller/telegram.go:111 直接调用 setupLogin。main 自有补填邀请码入口 controller/oauth.go:496,537 也必须纳入统一入口。
- service/passkey/service.go:50-57 根据配置使用 preferred（默认）或其他 UV 策略；controller/passkey.go:65、219 左右、420 左右未强制 VerificationRequired。上游 6f2333990 给注册、登录、敏感验证均强制 UV，并在 finish 校验 challenge 的要求。
- controller/twofa.go:408-425 pending_user_id 仅在 Cookie；没有该登录挑战的单独短 TTL/版本绑定。

触发：账户已启用本地 TOTP，同时有有效 OAuth/微信/Telegram 入口；掌握第三方认证凭据即可绕过本地 TOTP。设备存在不执行 UV 的可用认证器时，Passkey 也不等于实际进行了 PIN/生物验证。

最小方案：统一 primary authentication 后的登录策略，二次验证完成才签发登录会话；Passkey 本身完成 required UV 时允许作为已验证登录；挑战绑定用户、版本、有效期并原子消费。classic 的 OAuth/微信/Telegram/邀请码补登记流程同步支持 require_2fa 或统一 challenge 返回值。保留 main 自有邀请码逻辑。

#### S3. 6f2333990：注销账户缺少二次验证（高优先级的会话劫持防御）

证据：
- router/api-router.go:99 的 DELETE /api/user/self 只有 UserAuth。
- controller/user.go:936-955 仅取当前 id、排除 root，然后 DeleteUserById。
- frontend PersonalSetting.jsx:380-386 的输入用户名检查只在浏览器，服务端不验证。

触发/后果：已有有效用户会话的恶意同源脚本或会话持有者，可以直接执行删除。准确措辞是软删除账户：User 有 gorm.DeletedAt（model/user.go），Delete 调 DB.Delete（:726-735），并非物理永久删除。main.go:200 已启用 HttpOnly，因此不能说普通 XSS 直接读取 Cookie；它可以代发认证请求。

最小方案：删号要求账户专用、短时、一次性证明，事务内重新读取角色/有效身份，处理查询错误，完成后失效会话。仅挂现有 SecureVerificationRequired 不完整：现有验证只支持已配置的 TOTP/Passkey（controller/secure_verification.go:75-77），无此因子的普通账户需要原密码或原 OAuth 等重新认证路径。

#### S4. 45c3fbe8a + 0973dc2b8：安全因子和账号绑定可被劫持会话持久化；证明可跨动作/身份复用（高优先级）

证据：
- controller/passkey.go:531-540：没有启用 TOTP 就允许注册 Passkey，即使用户已绑定 Passkey 也不会验证现有 Passkey。
- controller/passkey.go:141 → model/passkey.go:180-196：UpsertPasskeyCredential 实际先删除旧凭证，再创建新凭证。因此不是仅“初次因子登记”，也能替换已有因子。
- controller/twofa.go:33-128 的 Setup2FA 无原身份再认证；现有 TOTP 尚未开启时，仅会话可取得新 secret/backup codes。
- controller/user.go:1240-1263 的 EmailBind 仅验证新邮箱验证码，不验证旧邮箱/原身份；controller/oauth.go:138-200 的绑定也仅依赖 Cookie 和此次新 OAuth 身份。拥有已劫持 Cookie 后可以绑定攻击者邮箱，再走 controller/misc.go:305-355 密码重置，或绑定攻击者第三方登录身份。
- controller/secure_verification.go:145-153 只保存 secure_verified_at/method；middleware/secure_verification.go:35-76 仅检查时间，不绑定用户/操作/渠道，也不消费。
- controller/user.go:149-157 的 setupLogin 覆盖 id/username/role/status/group，但未清理安全证明。保留原 Cookie 直接登录另一身份，会继承前一身份的五分钟 secure_verified_at/method。通过 /verify 获得的证明也可用于不限次数读取其他渠道密钥等操作。

纠正原清单：main 确实检查原密码（controller/user.go:881-900）和 OAuth state（controller/oauth.go:59-69），但这不等价于 0973dc2b8 的账号绑定/改密整体加固。不能将该 commit 整体标“已修复”。

最小方案：修改密码/邮箱/OAuth/Passkey/TOTP 等敏感操作先验证账户原有凭据；没有 TOTP/Passkey 时允许原密码/现有 OAuth 认证。proof 绑定用户、会话、操作 scope、目标上下文与有效期，服务端原子消费；切换登录身份先清除验证/pending 状态。保持 main 禁止解绑注册来源（controller/custom_oauth.go:554-557）和管理员 DisableUnbind 等自有策略。Argon2id 和传输加密是可分离的大范围改动，不是此次安全边界修复的必要前置。

#### S5. d7992672a + 0cd9dc85e：绑定/访问令牌轮换的旧 User 快照覆盖并发状态（高优先级并发安全）

证据：
- controller/oauth.go:175-193 完整加载 User 再 Update(false)。
- controller/wechat.go:223-232 同样完整加载 User 再 Update(false)。
- controller/user.go:435-455 GenerateAccessToken 加载所有列后 Update(false)。
- model/user.go:638-654 UpdateWithTx 的 Updates(newUser) 仅 Omit quota/used_quota/request_count，旧 role/status/group/password/access_token 等仍会写回。
- main 自有 EmailBind 同类：controller/user.go:1258 先 FillUserById，model/user.go:444-452 虽在事务里锁定新邮箱归属，最终仍 UpdateWithTx 整份快照。邮箱唯一性锁不能保护用户其他字段。

触发/后果：绑定或 token 轮换与封禁、降权、改密、token 轮换等并发，旧值可能恢复。尤其读取旧密码哈希再并发改密，会把旧密码哈希写回。0cd9dc85e 名为 Merge commit from fork，但含真实修复，不是应直接忽略的纯合并提交。

最小方案：绑定列白名单 + 单列更新（d799），access_token 专用单列写入并从普通 UpdateWithTx 排除（0cd9），EmailBind 保持邮箱唯一性事务但只更新 email；协调 main 的用户缓存失效，不能用旧快照回填 role/status。main aff 返利已经下线（model/user.go:43-47），0cd9 的 inviteUser/TransferAffQuota 子改动不适用。

#### S6. a8729b5c3 + 1da23d6b3：access token 无重新认证、无专用用户限流（中到高）

证据：
- router/api-router.go:100 目前只有 GET /token → GenerateAccessToken；没有上游后续新增的 status/POST/DELETE 管理 API。
- controller/user.go:433-464 仅认证身份即可生成长期管理 access token。
- classic PersonalSetting.jsx:209-218 点击后直接 GET，AccountManagement.jsx:681-688 重新生成按钮未确认。
- middleware/rate-limit.go 已有 userRateLimitFactory，但未绑定此路由。

后果：会话被劫持后容易生成长期凭证；高频轮换造成资源消耗、覆盖配置。该请求实际有全局 API limiter，准确说缺 critical/user 维度限制，不是“完全没有任何限流”。

最小方案：现有生成接口先要求 access_token.generate proof，并加入专用用户/IP critical limiter和 no-store；单列更新见 S5。若后续引入撤销 API，则同样验证。不要仅挂现有只支持 TOTP/Passkey 的 middleware 使普通账户失去该功能。1da 的 aff_transfer 不适用。

#### S7. 31d70fca3：Redis 限流检查与记账非原子（中）

证据：middleware/rate-limit.go:21-64 与 :155-195 先 LLEN/LINDEX 判定，后 LPUSH/LTRIM/EXPIRE，多并发都可能通过同一剩余额度判断；多个实例也一样。

上游修复：31d70fca3 同时引入带独立 key namespace 的单 Lua INCR/EXPIRE/TTL fixed-window 判定。此部分不依赖新 session 结构，可独立参考移植。注意它把原列表窗口改成固定窗口，窗口交界允许最多约 2 倍 burst；若 main 希望保留滑动窗口语义，可使用原子 Lua 滑动窗口，必须明确验证行为。

#### S8. 057f71c23 + 9f506dd7f：用户日志泄露管理员拒绝原因和内部渠道信息（中）

证据：
- service/text_quota.go:357,428-429 把 ContextKeyAdminRejectReason 写到公开顶层 other.reject_reason。
- model/log.go:127-145 注释了 reject_reason 删除，同时没有清除历史顶层 channel_* 字段。
- controller/relay.go:734-736 错误日志写公开 channel_id/channel_name/channel_type；formatUserLogs 只把 Log.ChannelName 清空，不能清除 Other.channel_name。
- 普通用户 self 日志和 token 日志走 formatUserLogs（model/log.go:165,720）。

最小方案：新日志把拒绝原因、渠道元数据移入 admin_info；用户读取同时过滤历史顶层 channel_id/channel_name/channel_type/reject_reason。可保留 main 现有 map 表示与审计结构，无需整包替换所有日志类型。若移入上游 LogOther 强类型系统，则 9f506 后续归并去重一起参考。main 自有隐藏模型映射逻辑必须保留。只恢复一行 delete 不覆盖同 commit 的渠道泄露。

#### S9. c3db41407 + 45c3fbe8a 日志子改动：SQL参数和OAuth敏感query入日志（中）

证据：
- model/main.go:151-193 的 SQLite/MySQL/PG/ClickHouse gorm.Config 没有 Logger，另只读副本初始化 :294-307 也要覆盖。
- 默认 GORM 慢/错 SQL 日志可能插入密码哈希、access token 等参数；驱动错误消息本身也可能内联字段值。
- middleware/logger.go:37 使用 Gin param.Path（包含 RawQuery），OAuth code/state/Telegram 登录签名参数可能被记录。

最小方案：参考 model/gorm_logger.go 的 ParameterizedQueries 和 sanitizer；同时覆盖 main 自有只读数据库入口与 ClickHouse。上游 !DebugEnabled 时脱敏，Debug 模式仍可能输出原值，部署风险需知道。OAuth 日志去 query 可独立小改，不影响 handler 原始 query，也无 auth 重构依赖。

#### S10. 043ff99a5：遗留 scoped Casbin policy 启动失败或限制被默认播种覆盖（中，条件性）

证据：
- service/authz/enforcer.go:19-30 定义 4 列 p policy。
- service/authz/adapter.go:21-31,108-120 将 V4/V5 也拼入；本地 Casbin v2.135.0 persist.LoadPolicyArray → model.HasPolicyEx 明确检查长度，非空额外 scope 会返回 invalid policy rule size。
- main.go:347-348 遇 Init 错误启动失败。所以不能写成“用户 scoped policy 被无声忽略并直接授权”。
- service/authz/seed.go:37-42 主节点先删除所有内置角色 p policy，再播种标准 allow。历史内置角色上的 own/other 限制可能直接被删除后放宽。
- 上游 adapter 明确将不支持 scope 的规则转 deny，seed 保留 scoped 行，两处必须一起处理。不能仅跳过用户 scoped 行，否则回落到 admin baseline。

前置条件：数据库保留旧 V4/V5 scope 行。未访问生产 DB，不知道实际是否触发。建议部署前只读检查历史策略，再移植 adapter+seed；正常新建 main 仅四列策略不受影响。

### 二、应从“已确认漏洞”降级或排除

#### f250f3b58：依赖确实落后，但当前 classic XSS 可达性未确认

package.json:19 / bun.lock:20,1249 顶层 DOMPurify=3.4.11；上游升 3.4.13。官方说明：
- https://github.com/cure53/DOMPurify/releases/tag/3.4.13 ：IN_PLACE hook removal、hook clone guard、IN_PLACE ownerDocument clobbering。
- https://github.com/cure53/DOMPurify/security/advisories/GHSA-55q2-fjhq-7xh7 ：<=3.4.12，fixed 3.4.13；维护者 severity low，要求 IN_PLACE + 移除包含元素的 hook。
- https://github.com/cure53/DOMPurify/security/advisories/GHSA-c2j3-45gr-mqc4 ：<=3.4.11，fixed 3.4.12；维护者 severity low，要求 CUSTOM_ELEMENT_HANDLING 和安全 hook/后续 custom-element HTML sink。

main helpers/sanitize.js:59-61,70-73,110-116 都只传 string，未配置 IN_PLACE/CUSTOM_ELEMENT_HANDLING/addHook。不能因版本号直接认定 classic 已有可利用 XSS。建议依赖更新，但不要用无条件“中危XSS已确认”写法。lock:3035 另有 @lobehub/ui/mermaid/dompurify@3.4.2，正式修复时需检查嵌套实例；本次未逐一审计 node_modules 中所有依赖调用方式。

#### 0973dc2b8、d8cb17744 的部分成立与不成立

- main 改密原密码校验、OAuth state 绑定 Cookie 已存在，但 0973 的持久化劫持防御未覆盖，见 S4；不是完整等价。
- main 有自研管理操作审计，d8cb 的新 AuditLog 表和新前端不必移植；不过上游新增 access token 状态/撤销/审计不是“main全等价已有”。main GenerateAccessToken 没有对应操作专用审计。可作为后续功能/可观测性增强，不列独立重大漏洞。

#### 其余领域内低优先级/不适用

- 33142f0ae：不是纯前端。controller/misc.go:117 的匿名 /api/status 仍公开 passkey_origins；上游删了该字段，测试覆盖 private.example.com 不应暴露。若 origins 包含内部域名则为低优先级信息泄露。可独立删除返回字段；classic 源码中未找到依赖 passkey_origins 的消费者。
- 385d2dfd1：新增安全多 RP ID 支持、配置快照和校验。main 无 legacy_rp_ids 及跨 RP 选择能力；没有确认现有签名验认证据会绕过。无需作为重大漏洞整包移植。7fd063819、b6566f33d 主要是其新前端配置/兼容修复。
- 9c97e78ac：classic 同样点击就轮换管理 token，建议为误操作加确认（PersonalSetting.jsx:209、AccountManagement.jsx:684），低优先级 UX；S6 的重新认证交互可一并解决。
- 116255f07：classic AccountManagement.jsx:155,161 已 Number(provider_id) 比较，:583 用 provider_user_id，等价覆盖这个显示类型错误。
- 692e8d6ee：上游新前端管理员解绑 key 名修复，main classic 未使用该错误组件；不直接适用。
- ffeb1b24e：classic LoginForm.jsx:289-326 未重置 Turnstile token，但 main middleware/turnstile-check.go:23-27 会话通过一次就跳过后续检查，上游 31d70fca3 已移除此缓存，才有新前端每次提交必须刷新 token 的问题。不把此 commit 直接判为 classic 登录重试回归。如未来移除长期 CAPTCHA 会话缓存，应一起适配 classic token 刷新。
- e78e1db1e：new web 的 window.opener 误判绑定模式；main classic OAuth2Callback 不使用那套 popup mode；不直接适用。
- 172114422：刷新 access-token 会话保活主要依赖上游 stateless auth；main 无该 refresh 机制，不适用。其 middleware Retry-After 改善可独立参考，不属于严重漏洞。
- 2d8e50bf3：新前端 usage-log autofill；不适用该组件。
- b27b2b1d6：新会话 UI 设备 iPad 判断，不是漏洞。
- d49160f0e：后台文字长度按 UTF-16 与前端统一；不是注入或高危验证缺失。
- 137d1171f：流式文字效果、playground 编辑器未保存退出提示；不属于安全加固。
- 84834eee8：向日志所有者公开 stream_status 是功能变化，main 仍过滤该字段；不需要为“安全修复”移植。
- 3e84ec0ab：Telegram OAuth/PKCE/ID-token 是协议迁移。main 旧 Telegram 验证器有 HMAC/date/duplicate-param 验证；无证据其签名验证本身失效。不同登录入口未统一 TOTP 的缺陷归 S2。
- 219c9e063/c79b74b68：公开数据缓存/新前端减少 status/refresh 查询，主要性能优化，不属于重大安全修复。
- 3f8a50cf8：审计记录 token/配额操作的增强；token 部分主要新增日志埋点，计费字段由另一领域审计。不据此宣称main漏高危认证漏洞。
- d3874db61：内存 limiter 按使用分配和 LRU 清理优化。main common/rate-limit.go:65 预分配 maxRequestNum，每 key 内存较大；本次未确认实际部署下可触发严重资源耗尽，归性能/容量优化。

### 三、提交分类索引

深入读取上游关键 diff，并对照 main 相关调用链：
- 已确认需修：6f2333990、45c3fbe8a、0973dc2b8、31d70fca3、d7992672a、0cd9dc85e、a8729b5c3、1da23d6b3、057f71c23、c3db41407。
- 后续关联：9f506dd7f（057 日志类型重构优化，独立安全收益不重复计数）。
- 条件性需修：043ff99a5（历史 scoped policy）、33142f0ae（泄露内部 Passkey origins，低）。
- 依赖升级建议/未确认可利用：f250f3b58。
- 有部分等价实现但不能整体排除：0973dc2b8、d8cb17744。
- main 对该小问题已等价覆盖：116255f07。
- 可选功能/低级别/不适用：385d2dfd1、7fd063819、b6566f33d、3e84ec0ab、692e8d6ee、9c97e78ac、b27b2b1d6、d49160f0e、e78e1db1e、172114422、2d8e50bf3、ffeb1b24e、137d1171f、84834eee8、219c9e063、c79b74b68、3f8a50cf8、d3874db61。

仅从全部 249 条 git log 筛查了领域内相关提交；本报告不表示逐行审完全部 249 diff。31d70fca3 和其他大提交只深入认证/限流/相关服务端边界，没有通读其数千个前端移动/删除。

### 四、边界与建议顺序

先修 S1、S2、S4/S3、S5；再修 S6/S7 与 S8/S9。S10 由实际历史数据决定优先级。所有移植都要在 classic 内适配，不带入上游删除 classic 或新增 web/src 的操作。

未访问生产账户、数据库、Cookie、token 或日志，不知道当前部署是否启用 Telegram/OAuth/Passkey/TOTP/Redis 或存在旧 Casbin scope。没有运行测试；结论来自 main 的实际代码和上游 diff，而不是动态复现。S1 的 auth_version、S4 的一次性 proof 会产生跨模块设计/迁移工作，应单独设计并验证，不能描述成“挂两个现有中间件即可完成”。



---

## new-api 与 main：计费、配额、充值、退款审计

基线：main=37197dbca；new-api=69a500298；分叉 a63364d15。已先阅读 `pkg/billingexpr/expr.md`，浏览分叉后完整 249 条提交列表，对以下计费候选逐项读取 diff 与 main 实际调用链。仅静态审计，无源码改动、无数据库/生产请求、无随机压力测试。下文行号均为本次 main 工作区。

### 一、优先修复：可造成真实额度损失

#### 1. ccd535ef8：初始预扣非原子、缓存陈旧快照覆盖 — 确认未修

- 钱包调用链：`service/billing_session.go:375` 读余额、`:385` 比较 → `service/funding_source.go:40` → `model/user.go:1116` 无条件 `quota - ?`。并发请求可同时通过同一旧余额检查，超出可用额度仍发往上游。关闭 Redis 仍存在 DB 检查与更新分离；开启 Redis、批量记账或读写分离会增加窗口。应区分初始预扣限制与设计上允许的最终结算欠费。
- 令牌：`service/quota.go:393-400` 同样先读/检查再扣；`model/token_cache.go:11-14,30-40` 快照 HSET 与异步 HINCRBY 竞争，`model/token.go:263-282` 读 DB 后异步写快照，能覆盖正在变化的余额。裸 HINCRBY 还可能建立只有 quota 字段的不完整哈希。
- 用户缓存也需一起适配：`model/user.go:971-979` 把读得的旧余额异步写缓存；`model/user_cache.go:66-75,104-112,156-160` 是全快照水合和裸 HINCRBY。仅添加 TryReserve 函数而保留旧快照写回不够。
- 同提交的渠道状态问题：`model/channel.go:354-358` 的 `SaveWithoutKey` 除 key 外全行保存，由 `:813-843` 状态流调用，会把陈旧 used_quota/配置覆盖回去；多 key 的 polling lock 未包住整个读改写。

适合 main：引入 DB 条件 UPDATE 的原子初始预扣与 Redis Lua 完整哈希检查、只初始化冷缓存、token mutation fence、数据库失败补偿；状态流仅更新 status/other_info/必要 channel_info，并保留 main 归档渠道保护。移植前须适配 main 自有 ReadDB/缓存与订阅逻辑：上游脚本依赖 `CacheSchema`，main 没有此字段，不能直接粘贴 `model/quota_reserve.go`。`50e5377ea` 提供该文件早期基础；取其中机制，不必引入 `31d70fca3` 整套认证重构。

同提交的订阅限购部分：main `model/subscription.go:959-981` 已有套餐行锁和限购检查，`:1207-1215` 先续期后限购，是自研业务。已有相应并发控制，不把上游“没有任何限购锁”的缺陷直接算作 main 未修；应保留该策略，不能机械新增上游用户锁替换整个函数。

#### 2. 50e5377ea：epay 状态与入账分离 — 确认未修

`controller/epay_notify.go:61-96` 只用进程内 `LockOrder`；`:76-78` 先保存 success，`:83-84` 后转换额度并入账。

- 多实例同时收到同一笔已签名成功回调，可都读到 pending 并重复加余额；人工补单和回调并发也不共享这把进程锁。
- 单实例也受影响：保存 success 后入账失败/进程崩溃，后续重试在 `:72` 直接返回成功，永久漏充。
- `:83` 的 `int(decimal.IntPart())` 没有 int32 范围检查；不能认为其他计费的饱和转换已覆盖此充值路径。

适合 main：将 epay 结算放到 model：主库订单行锁、provider 校验、状态检查、严格额度转换、订单成功与用户额度增量同一事务，事务成功后同步缓存和记录 main 的 LogSource。重复成功回调无额外余额变化。无需改 Stripe/Creem/Ethereum 的架构。

已核：Stripe `model/topup.go:252-279`、Creem `:535-580`、Ethereum `:612-677` 都已有事务和订单行锁，状态与余额同事务，用户关于三者原子化的说法成立。但它们的余额上界仍见下一项。

#### 3. df43f8015 + cfaba1dd6：tiered 自动重试仍用旧分组倍率 — 确认未修

`relay/helper/price.go:439-453` 冻结最初 group ratio；`controller/relay.go:195-201` 仅初始非免费时建计费会话；`:225-238` 重试选渠道后无 billing prepare，`:449`/`:460` 只更新 PriceData.GroupRatioInfo；`service/tiered_settle.go:98-108` 始终使用初始 snapshot。

触发：使用 tiered_expr，并允许跨组重试，最终选中不同倍率组。低价→高价少收，高价→低价也会多收。免费倍率 0→付费时 snapshot 仍为 0，且没有计费会话，基础表达式费用为零；并非只漏预扣而最终能自动补上。

适合 main：每次渠道选择成功后调用 PrepareTieredBillingForSelectedGroup：刷新 snapshot 的分组倍率与估价；已有 session 调 Reserve；免费→付费新建 session 并清 FreeModel。结合 cfaba1 将失败选路的倍率更新后移、记录 use_channel 后再预扣。保留 main 静态路由/同渠道重试/订阅追加分组。上游 cfaba1 对钱包补差额允许欠费，订阅硬限额，这与“所有 Reserve 必须拒绝欠费”不是同一个策略。main 还支持分组独立表达式，此补丁只刷新 group ratio，不能声称顺带解决不同组表达式/计费模式切换。

#### 4. e0d515611：Suno 先退款后持久化 — 确认未修

`service/task_polling.go:280-295` 先 `RefundTaskQuota`（:288），再无条件 `task.Update`（:295）；`service/task_billing.go:165-196` 退款后未清 task.Quota。已退款但 task 更新失败后再轮询，或旧快照/重叠执行，会再次退款。主路径 scheduler 已有数据库任务租约（`service/system_task.go:241,305-327`），因此不能把“部署多个实例就必然重叠轮询”当作事实；租约切换/持久化失败仍需任务级 CAS 保证。

适合 main：先将状态强制设为 FAILURE 并 `UpdateWithStatus(prevStatus)`，仅状态转移赢家退款；退款成功清 quota，失败保留标记供明确重试/人工对账。main Video `service/task_polling.go:563-588` 已按终态 CAS 防重复，用户判断正确。注意后续 `bc14c18f6` 已删除 e0 的自动退款 reconciliation 扫描，且纠正 legacy cutoff 年份；不要整包复制 e0 重建已被上游撤回的扫描。CAS+资金调整仍非一笔完整账务事务，崩溃后漏退窗口需账务兜底，不能宣传成 exactly-once。

### 二、需要修复但应限定影响条件

#### 5. 2a0ce3475 + 47ba9d2c6：付款前校验及钱包容量上界 — 确认大部分未修

main Epay 创建订单 `controller/topup.go:202-213` 只校验最小额，Stripe `controller/topup_stripe.go:73` 的数量 10000 上限并不等于 int32 quota 上限（默认每单位 500000）；Creem `controller/topup_creem.go:110` 直接保存所配产品 quota。下单前需按结算时同一转换检查 0<credit<MaxQuota，避免已付款才发现无法入账。

`47ba9d2c6` 另解决“现有余额+本次充值”的容量：下单读主库预检查，事务内 `WHERE quota <= max-current-credit` 与加余额同一 UPDATE，检查行存在。main `model/topup.go:274,504,561,673` 都没有最终余额条件。MySQL/PostgreSQL int32 列可能拒绝导致已付款无法入账，SQLite 可存更大整数形成跨库不兼容；不宜统一描述成所有数据库必然整数回绕。

Ethereum 已部分等价：`controller/topup_ethereum.go:190-194` 下单校验单笔；`model/topup.go:662-664` 结算拒绝 clamp。但 `:673` 仍缺现有余额容量上界；需把此防线扩到 main 自有链上支付，保留过期报价/晚到付款和审计逻辑。

上游后续 `a073f74b3` 把钱包扩为 53 位安全整数并引入 schema 检查。main `model/user.go:39-40` 仍 `gorm:type:int`，不建议为本次修复引入额度类型/数据库迁移；取 2a0/47ba 当时的 int32 约束。main `common/quota_math.go` 已有 FloatStrict，但还需补 DecimalStrict 或等效检查。

#### 6. 58d4e9bd3：任务退款的已用额/令牌记账不完整 — 确认未修

`service/task_billing.go:172-195` 退资金和令牌，但没有减 user/channel used_quota；差额退款 `:223-247` 同样漏减，正差额 `:242` 还把同一任务 request_count 再加一次。Midjourney `controller/midjourney.go:216` 仅退钱包，没有退令牌和 user/channel used_quota，模型未保存退款需用的 token id。

后果：quota+used_quota 的“总额度”统计虚增，已用额度和渠道成本失真；一般任务本身并未凭此多获得可消费余额，不能将统计虚增等同于多退真钱。Midjourney 不退 token 则使令牌可用额度持续少于钱包。

同补丁还有实际计费标记修复：main `relay/mjproxy_handler.go:500-501` inpaint/custom-zoom 跳过扣费，但 `:593` 仍无条件 task.Quota=priceData.Quota；如果这种未收费任务后来返回失败，退款器仍会加钱包。因此应用 58d 时不能只补两行 used_quota，还应保证 task.Quota 仅标记已发生的扣费，并记录实际 token/渠道，失败不退款未扣金额。

适合 main：新增只调整 used_quota、不改请求数的方法；完整与差额退款同步 user/channel 用量，MJ 持久化实际扣费标记和 token id。新字段可能需迁移，属于后续实现审核点，当前不执行迁移。

#### 7. f11641428：Responses 桥接结算丢 cache — 确认未修，原生 Responses 已有处理

main 原生 Responses `relay/channel/openai/relay_responses.go:52-54,105-107` 已复制 cached_tokens。因此不应笼统说“Responses 缓存全部按全价”。

缺陷发生在 Responses→Chat 等转换：`service/relayconvert/internal/oai_responses/to_oai_chat_resp.go:138-140` 创建保存原始 input_tokens_details 的 BillingUsage；`service/text_quota.go:349` 优先从该 sidecar 取计费用量，而 `service/billing_usage.go:103-123` 只规范化总量，没有将 InputTokensDetails 的 cache/image/audio 等填入 PromptTokensDetails。转换器已经放到顶层的缓存数据因此失效。命中缓存且缓存单价不同才有多扣/错扣。

适合 main：将 f116 的字段补齐逻辑移入现有 usageFromOpenAIBillingUsage（包括 PromptCacheHitTokens 回退），保留已存在的 canonical 顶层字段。无需为 23 行修复整体提取 relaykit。

#### 8. bbd97446c：Gemini/Claude 细分用量 — 确认适用的三点

- `service/billing_usage.go:175,197` 直接按大写 modality 匹配，小写/空格变体被忽略，独立图片/音频价格错误。应统一 TrimSpace+ToUpper；同一数组重复 modality 当前 += 已会累计，不要把上游新 merge helper 的全部问题算给 main（main 根本没有 dto/usage_merge.go）。
- `service/billing_usage.go:128-134` 以数值 0 回退 Claude flat cache 字段，忽略“子对象存在且明确为 0”的语义。`service/relayconvert/internal/claude_messages/to_oai_chat_resp.go:376-380` message_delta 也只接受 >0，无法清前帧 1h 值；`:386` 再生成计费快照。后帧把 1h 修为 0 时仍可能多收。应当以 CacheCreation 非 nil 的存在性为准，整体同步 split，包括显式零。
- `service/billing_usage.go:187-188` 在 total<prompt 且无 completion 时产生负 completion。上游加下界 0；main tiered 层有下界但普通倍率路径直接使用此值，会折抵输入费用/记录不正确统计。main SettleBilling 已拒绝负 charge，所以不能称其直接允许负数充值。

该提交 InitChannelMeta 重置 SendResponseCount 属 relay 重试清理，由 relay 审计报告处理。

#### 9. f064bffa2：图片最终数量与预扣一致性 — 已部分修，仍有关键缺口

纠正原清单：main `dto/openai_image.go:158-170` 已把顶层 n 放 BillingRatios，`relay/helper/price.go:254-263` 按次预扣已乘 n；普通按次 OpenAI 图片不是全都漏 n。

仍缺：Ali `relay/channel/ali/image.go:18-65` 在 adaptor 才解析 Extra.parameters.n/prompt_extend 并设置计费倍率，时点晚于初始预扣；按 token 图片 `relay/helper/price.go:207-212` 未应用 Ali 数量；`relay/image_handler.go:49-87` pass-through/参数覆盖后的实际出站 n 未重新验证/补足预扣。因此配置对应渠道/旁路时，可以仅预扣较小数量而提交较大数量。`relay/channel/ali/image.go:352-355` 对上游 ImageCount 或结果数组长度无 128 上界，虽负数被 AddOtherRatio 挡住且最终算费有饱和，异常正数仍会导致过量扣费。

main 顶层 `n` 的 JSON/表单 MaxImageN 上界以及 Ali adaptor 的 `<0 || >128` 已存在（`relay/helper/valid_request.go:177,227`、`relay/channel/ali/image.go:61`），不应称完全无验证。修复应补入口 provider quantity、出站旁路重验、发送前原子补预扣、受限响应数量回写。保留现有 128 常量与饱和审计。`f362c7c51` image_count/image-cache 表达式与 `064ed943e` fixed() 为新特性，非此次修复必需。

#### 10. 0ed497f06：流式 usage 保存 — 确认未修的三个局部

- Gemini `relay/channel/gemini/relay-gemini.go:165-167` 每帧完整覆盖 usage，后帧部分数据可擦除前帧缓存/模态；应按 provider 累计快照语义合并，不能简单把累计总量每帧相加。
- Gemini 缺输出用量时 `:45-61` 估算 completion 后重新创建精简 BillingUsage，抹掉已知 cache/modality；`dto/billing_usage.go:110-123` 只留下总量。上游改 clone 原快照并只补缺失 completion。
- OpenAI Chat `relay/channel/openai/relay-openai.go:124-179` 只有 audio 模型保存倒数第二帧；非 audio 兼容上游完整 usage 在倒数第二帧、末帧无 usage，会在 `:200-202` 回退本地估算，丢缓存/隐藏输出计数。上游让所有模型保留该帧，仅末帧无有效 usage 才回退，并 `:205` 用实际 usage 所在帧做 post-process。

适合 main：移植这三个小机制及必要 dto clone/merge，不搬整个 149 文件 hosted-tool 重构。

#### 11. 2d23cdf29 的工具计费局部：不是纯新特性

main `relay/channel/openai/relay_responses.go:61-67` 按 response.Tools 声明给工具 CallCount++，但实际调用在 response.Output；声明却没调用会多收，多次调用只收一次会少收。流式 `:123-127` 只计 web_search_preview，文件搜索和 web_search 新名称有漏记。上游该 feat 提交换成实际 output invocation 计数；可移植此正确性修复至 main 现有工具价格模型，Sub2API/alpha search/新工具价格 UI 不必引入。

### 三、不应误判为必须移植的内容

- `a073f74b3` 钱包从 int32 扩容是类型/schema 调整；其有用的安全边界应适配 main 当前 int32，不能直接复制 HEAD 上限。
- `3cea2bf79` 新增 cached_tokens_details 内部分模态字段、`f362c7c51` 新表达式变量、`064ed943e` fixed()、`f256e40bc` GPT image 默认式主要属于新特性。main 没有这些字段/表达式时，不把指针 clone/新增定价缺失认作旧功能漏洞。
- `27235a277`、`e926e5cac` 的 diff 均落上游 web/src 新前端（模型编辑器、兑换码编辑器）；不应照搬到 classic。本代理未重新完整审计 classic 兑换码精度，故不独立声称已等价修复；交给主审计核验。
- `8c8c4153d` main `model/log.go:774,778` 双 Scan 模式存在，但当前实际 GORM=1.25.2；主代理已核官方 1.25.11+ 会清零未选字段，应作为升级前必补防御修复，不说当前统计已恒零。
- `7c044d7c5` @model modifier/canonical identity 为上游新功能，未证明 main 旧语义有对应新回归，不建议整包移植。
- `bc14c18f6` 为 e0 后续撤掉自动退款对账的最终语义；Suno 修复应参考其调整。

额外旁注（不计作上游已修候选）：main `model/topup.go:484-490` 手动补单只特判 Stripe，Creem Amount 已是 quota（`:556-557`），手动补 Creem 仍乘 QuotaPerUnit。new-api HEAD 也仍如此；不得写成“50e 已修 Creem 单位”。这属于双方额外问题，后续若做充值专项可单独处理。

### 四、检查记录与边界

已读取 diff 并对照 main 关键链：`ccd535ef8`、`50e5377ea`、`2a0ce3475`、`47ba9d2c6`、`df43f8015`、`cfaba1dd6`、`e0d515611`、`bc14c18f6`、`58d4e9bd3`、`f11641428`、`bbd97446c`、`f064bffa2`、`0ed497f06`（计费局部）、`a073f74b3`（钱包局部）、`2d23cdf29`（工具计费局部）、`3cea2bf79`、`8c8c4153d`。

按变更文件/功能界面筛除或供依赖分析，未对整提交每一行做行为审计：`27235a277`、`e926e5cac`、`064ed943e`、`f362c7c51`、`f256e40bc`、`7c044d7c5`、`3f8a50cf8`。纯样式、插件、文档提交仅提交列表初筛。

未覆盖：生产真实支付/Redis/三数据库并发复现，部署是否使用 Redis/BatchUpdate/读写分离/tiered/跨组/对应provider；风险均按代码具备路径给出条件。HTTP/认证/relay其他协议/DB迁移/classic时间规则由其他审计分工负责。本报告不宣称249提交全部逐行验证。


---

## 传输、数据库和 classic 补充复核

基线为 main=37197dbca、new-api=69a500298。以下是读取相关提交差异并对照 main 的结果；没有连接生产数据库或供应商接口。

### I1. b518d0033：等待上游响应头无时间上界（高优先级）

main service/http_client.go:74、165、210 分别构造直连、HTTP/HTTPS 代理、SOCKS 代理 Transport，都没有 ResponseHeaderTimeout。common/init.go:109 的 RELAY_TIMEOUT 默认 0。TCP 建连和 TLS 握手超时不能限制“请求已发送，上游一直不返回响应头”。

触发需要请求 context 未取消、未设总超时，而上游连接不结束、不返回头；此时处理请求的 goroutine 和其可达请求对象可长期驻留。main 用 NewRequestWithContext 传递客户端 context，所以客户端已断开不应一概声称仍永久挂起。上游提交说明中的“三份请求体、172.9 小时 OOM”是上游实例观察，不是此次在 main 的实测，也不是固定的 OOM 时间。

适合 main：新增有合理默认值的响应头超时并覆盖上述三处构造，转换秒数前防 time.Duration 溢出；保留 main 的 RELAY_CONNECT_TIMEOUT、SOCKS DialContext、TLS 与 SSRF 取数策略。上游默认 1800 秒可作为兼容起点，非流式长任务应按使用场景配置。收到响应头后的 SSE 流不受该字段限制。无需引入 e99a9bd86 的逐渠道传输控制新功能。

### I2. d6b5ce99d + ea4f02101：请求体无法正确重放（中优先级）

main relay/common/outbound_body.go:25-30 通过 common.ReaderOnly 隐藏 BodyStorage 类型。relay/channel/api_request.go:359-363、382-386 只还原 ContentLength，没有提供 GetBody。HTTP/2 发生可安全重试的 REFUSED_STREAM/GOAWAY 时，已读 body 无法重新提供，导致请求失败。

main 同文件:638-640 还覆盖了任务请求的 GetBody，返回同一个 requestBody reader。该 reader 被第一次读取后已经位于末尾，重放会读到 EOF；这也覆盖了 net/http 原本能为 bytes.Reader 生成的正确 GetBody。是否在真实请求中重放取决于传输错误/重定向条件，此次未运行 HTTP/2 故障注入。

适合 main：BodyStorage 暴露每次从起点开始、游标独立的 NewReader；内存共享只读底层字节，磁盘使用独立文件句柄；出站 body 携带 Size/NewReader 并统一绑定到请求。保留存储清理所有权，GetBody 关闭不能关闭整个存储。参考 ea4 的最终 ReplayableBody 结构可避免把上次请求 body 元数据留在 RelayInfo。移除错误的任务 GetBody 覆盖。

d6 同时停止 relay 自动跟随上游 3xx。main service/http_client.go:25 与 relay/channel/api_request.go 的 doRequest 仍可跟随通过 SSRF 检查的重定向；引入可重放 POST 时应一并审查此行为，避免意外重发 body。应复制共享 Client 后只修改 relay 的 CheckRedirect，不能改动全局缓存客户端或破坏独立的受保护 URL 取数服务。

### I3. 1751f43ee：SQLite 默认 DSN 不适配当前驱动（中优先级）

main common/database.go:44 是 one-api.db?_busy_timeout=30000；go.mod 使用 github.com/glebarez/sqlite v1.9.0 / modernc SQLite，而不是 mattn 驱动。上游修复改用：

    one-api.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate

_pragma 设置才能传入该驱动，WAL 改善读写并发；BEGIN IMMEDIATE 避免事务先读后写升级锁时的 SQLITE_BUSY_SNAPSHOT，这不是简单加大 busy timeout 能解决的。

main common/init.go:67-68 会用 SQLITE_PATH 完整覆盖默认值。因此只改默认 DSN 无法自动修复部署中已有自定义 DSN，需在实际配置中保留路径、补正确参数，不能覆盖用户路径。启用 WAL 对网络文件系统的适用性、数据库文件所在存储条件尚未检查；本次没有改配置或打开数据库。

### I4. 66031a09d + 6eb6f35ed + 74158715c：PG 事务池、JSON 参数与驱动联动（条件性中优先级）

已执行 go list -mod=readonly -m，main 实际选中：

    gorm.io/gorm v1.25.2
    gorm.io/driver/postgres v1.5.2
    gorm.io/driver/mysql v1.4.3
    gorm.io/driver/clickhouse v0.6.0

main model/main.go:159-163 同时设置 PreferSimpleProtocol:true 和 GORM PrepareStmt:true。对于不支持命名 prepared statement 的事务池配置，关闭 pgx 隐式 prepare 不等于关闭 GORM 显式 prepare，仍可能出现协议/重名语句错误。不是所有新版 PgBouncer/所有直连 PG 都必然失败。

main 自有只读副本入口 model/main.go:294-297 也设置 PrepareStmt:true，上游原代码没有这条入口；移植时必须覆盖。主库、日志库经 chooseDB 的 PG 分支也应一致。

只关 PrepareStmt 又会暴露 JSON Valuer 的问题：model/channel.go:170、model/task.go:92/156、model/prefill_group.go:23 返回 []byte，pgx simple protocol 可能将其编码成 bytea 而非 JSON 文本。对应 Valuer 应返回 string，Scanner 兼容 []byte/string/nil。需保持 main JSON 包装函数和 NULL 语义。

上游组合目标为 GORM 1.25.12、PG driver 1.5.9、MySQL driver 1.5.7。应将这些兼容改动作为一个验证单元，保留 main 的 ClickHouse 日志/读写分离扩展；不推断“任意更高 GORM 版本都安全”，也不把“禁止全部1.26+”当作已验证结论。本次未改 go.mod/go.sum，未完成三库集成测试。

### I5. 8c8c4153d：GORM 升级前必须修双 Scan（当前并未恒零）

main model/log.go:774-781 先把 quota 聚合 Scan 到 stat，再把 rpm/tpm Scan 到同一个 stat。已读取官方 GORM v1.25.2、v1.25.11、v1.25.12 的 scan.go 和 ScanRows：

- 1.25.2 的 struct 分支不会先清零整个对象，符合当前行为。
- 1.25.11/1.25.12 在 ScanInitialized 下 reflect.Zero 整个 struct；第二次扫描会将 quota 清零。

适合 main：第二次扫描使用单独的 Rpm/Tpm 结构，再将两项赋回 stat。该防御应在依赖升级前或同次完成，不说 main 当前统计已经恒零。

核验来源：
- https://github.com/go-gorm/gorm/blob/v1.25.2/scan.go
- https://github.com/go-gorm/gorm/blob/v1.25.11/scan.go
- https://github.com/go-gorm/gorm/blob/v1.25.12/scan.go

### I6. b7017c251：MySQL no-op 写误判任务锁丢失（中优先级）

main model/system_task.go:310-332 对带租约条件的 UPDATE 只要 RowsAffected==0 就报 ErrSystemTaskLockLost。MySQL 默认计的是实际 changed rows；同一秒写入相同 state 和 updated_at 时，即使匹配仍持锁也会返回 0。

main service/system_task.go:365、400、417 可在日志清理流程中短时间重复保存同样完成状态，错误后直接退出，任务随后等待租约过期。使用 clientFoundRows 的部署可能不受此相同条件影响。

适合 main：零更新后，使用同一个 now 和同样租约/任务条件回查是否仍持锁；有则成功，无则返回丢锁。不能无条件把所有 RowsAffected==0 当成功，真实租约丢失仍必须拦截。

### I7. PG 历史约束与重复 DDL：条件性/升级准备，不作为已证实当前故障

相关提交：69a41eead → 2bf0820f4 撤回 → 2b6f1dfef 重做，后续 27ff6a876、007d69942、043ff99a5；9a8674425 负责 DDL 比较规范化。

main model/main.go:334-342 与并行 migrateDBFast:405 后直接 AutoMigrate，没有专门处理旧 prefill_groups.name/tokens.key 唯一约束。model/prefill_group.go:77 的当前部分唯一索引与旧全局唯一约束可能冲突。

应区分两层事实：main 没有迁移防线；但是否出现 42704 取决于实际历史 schema 和所用 GORM/PG driver。main 当前 1.25.2/PG1.5.2 尚无后续 MigrateColumnUnique 的同一执行链，所以不能只凭上游 1.25.12 的报错认定当前 main 必然启动失败。升级前应检查真实 catalog，并按语义匹配约束，避免误删复合/部分/表达式索引。

实现时应同时考虑 main 的普通/并行迁移入口、自有表和三数据库 fallback。旧回滚提交不可照搬。9a 的 DECIMAL 默认值/PG CHAR-bpchar 比较以及 SQLite driver 1.11.0 更新偏维护项，不能当作全库无条件结构重建的授权。

### I8. e13d4033e：代理地址规范化与缓存生命周期（中/低）

main service/http_client.go:202 将未补端口的 parsedURL.Host 直接交给 SOCKS5；socks5://host 缺默认1080时可能拨号失败。controller/channel.go:865 的删渠道路径没有清理对应代理客户端，代理凭据与连接池可继续留在进程内；多处状态更新却调用全局 ResetProxyClientCache，造成无谓连接重建。

适合 main：校验/规范化地址、SOCKS补1080、按代理失效旧客户端，并在删除/批删时清理；支持共享代理和并发首次创建。main 已有 relayDialer().DialContext/SOCKS ContextDialer，不能在移植中倒退其取消和建连超时。凭据留在内部缓存不等于已证实泄露给外部。

### I9. ac381acf4：classic 时间范围生成恒真式（高优先级计费错误）

main web/classic/src/pages/Setting/Ratio/components/requestRuleExpr.js:214-218 对所有范围都生成 >=start || <end。已用 Node 直接加载当前文件的 buildRequestRuleExpr，检查实际输出：

    range 9-17, multiplier 2:
    (hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 17 ? 2 : 1)
    hour 0/9/12/17/23 -> 2/2/2/2/2
    range 21-6 -> 2/1/1/1/2（跨夜逻辑正确）
    range 9-9 -> 全天2（边界同样错误）

适合 main：start<end 用 &&，start>end 用 ||，相等边界明确为空区间；生成和反解析都验证 hour/minute/weekday/month/day 合法整数域，并支持普通同日范围回读。classic 的匹配标签/说明经 i18n 更新；保留 main 多条件组合结构。

仅修改生成器不会自动修改数据库里已经存储的错误表达式。后续实施应列出可疑现有配置供管理者检查，不能直接批量改写可能由管理员有意编写的原始表达式。

### I10. 低优先级 main 同类/已等价问题

- b5b94bc68：classic SubscriptionPlansCard.jsx:394-405 将无有效订阅的 subscription_only 错显为 wallet_first，:617 还说自动用钱包；service/billing_session.go:427-428 实际只试订阅。应修显示/提示，保留真实计费偏好；不是后端擅自扣钱包。
- 4aa08f917：controller/user.go:622-641 对 group=auto 直接查字面分组，未展开实际可用组，影响 playground 模型列表；非越权证明。
- 62f8db775：controller/channel-billing.go 的 DeepSeek 余额只找 CNY 且原值展示，未统一成USD；影响管理余额展示，不直接改变用户计费。
- 876903a8e：默认火山模型发现仍用 /v1/models；应为 /api/v3/models，见 relay 详情。
- 506e41c11：分页排序属于管理列表准确性，不列重大漏洞。
- b941253ae：main controller/channel-test.go:755 仍把 Anthropic/Gemini 测试构造成 GeneralOpenAIRequest；:400 附近按该类型调用 ConvertOpenAIRequest。测试路径因此不能真实覆盖原生端点，可能测错路由或误报渠道状态。应分别构造 ClaudeRequest/GeminiChatRequest、调对应转换接口，并给 Gemini 流式合成 :streamGenerateContent URL；保留 main 自研测试/恢复逻辑。此项未等价修复。
- e926e5cac：classic EditRedemptionModal.jsx:94/130 使用六位小数编辑和 displayAmountToQuota；helpers/quota.js:46-54 用 Math.round。未使用上游导致该问题的 TS 货币编辑工具，不能直接移植新前端组件；这也不等于证明任意自定义汇率都绝对无舍入。
- 5c7cca015：main pkg/perf_metrics/types.go:67 与 metrics.go:217 已有带时间戳 RecentSeries，不必替换成上游新字段/API。
- 98d50d538：classic SetupCheck.js 根据当前 status context 检查，不使用上游的永久 localStorage setup_status_checked 标记。
- 4fc9d1f1f：main Option.Key 已 primaryKey，定价使用独立 Pricing 结构，不走上游新 mutateModelPricingOptions 插入默认后全表覆盖的路径；没有生产 schema，不能凭此证明历史库绝无缺主键问题。

### 验证范围

本补充报告完成静态调用链核查、classic 实际纯函数复现、依赖解析和 GORM 官方源码比对。没有运行服务、付款、压力测试、完整 Go 测试套件或前端构建；当前环境无 Bun 可执行文件，但 Node 可运行上述无依赖纯函数。没有升级依赖、改环境变量或迁移数据库。

桌面 Electron 依赖提交 626058075、53a8739ee、cf38105a9、bbf67df04 仅列维护候选，main 虽只有 classic 网站前端，仍有 electron/ 目录。本轮未逐个核验桌面依赖漏洞公告/运行配置，不能视为已经排除桌面风险。e5efc73cd 的 tree 与父提交相同，实际无文件变更，不能以标题宣称 tar 已升级。



---

## new-api → main：Relay / Provider 只读复核

基线：main=37197dbca，new-api=69a500298，分叉 a63364d15。本文仅为静态审计；已读取上游相关差异、main DTO、请求路由、适配器与响应处理调用链。没有修改仓库源码、切分支、合并或 cherry-pick，也没有请求真实上游或运行生产验证。用户原清单不能直接当成已确认结论；以下区分确认存在、条件适用、已等价修复及上游新增回归。

### 结论摘要

原先列出的 Relay 候选大多数仍适用于 main，但 fa90b2312 的大小写校验错误属于上游新增校验引入的回归，不能列为 main 同等缺陷。a20574136 不能因文件在 plugins 中就整项排除：main 的 Go Ali task adapter 存在同类 Wan2.7 协议问题。610334dbd 在 main 还缺 AdvancedCustom 的 stream options 能力声明，直接复制上游转换函数的条件判断不会生效。

额外发现需要补充到候选表：16bfae175、49ec46966、6e10f9bc9，以及 0ed497f06 中可单独移植的 Gemini 工具 ID 关联与非音频 SSE 倒数第二帧用量保留。大重构不宜整包移植；main 自有协议转换、取消逻辑、OA2、路由与计费需要保留。

### main 确认仍存在的 Relay 问题

#### R01：Responses 转 Chat 的流式工具调用重复

- 上游：1086038f5。
- main：service/relayconvert/internal/oai_responses/to_oai_chat_stream_resp.go:259、271、163；生产入口 relay/channel/openai/chat_via_responses.go:188 与 relay/channel/advancedcustom/adaptor.go:261。
- 触发：先收到带 output_index 的 output_item.added / arguments.delta，后收到 response.completed，终态 output 内带同一 item/call ID 但没有 output_index。
- 影响：key 从 output:N 变为 item:ID，ensureToolForEvent 直接创建第二个工具 index，完整参数再次输出；客户端可能执行工具两次。若调用有写操作，影响可高于普通协议错误。
- main 修法：创建工具前按既有 itemIDToKey、callIDToKey 找回同一对象，并把新 key 作为别名。保留原流式状态机，移植上游去重回归用例即可。

#### R02：Qwen thinking_budget 被 DTO 与跨协议转换丢弃

- 上游：66ee6b8f9。
- main：dto/openai_request.go:89、883；service/relayconvert/internal/oai_chat/to_oai_responses_req.go:375；service/relayconvert/internal/oai_responses/to_oai_chat_req.go:58；relay/channel/ali/adaptor.go:179。
- 触发：客户端发送 thinking_budget，包括明确 0；请求经 DTO 解码/重编码或 Chat ↔ Responses 转换。main DTO 没有 ThinkingBudget 字段，转换自然无法保留。
- 影响：推理预算静默失效，输出/成本与客户端设置不同。全体积透传路径另论。
- main 修法：两类请求 DTO 增加能保留显式 0 的字段，双向转换复制，Ali 按映射后的上游模型决定是否保留。不要盲目照抄上游按原始别名过滤字段的 MarshalJSON；main 自有克隆/别名处理需验证别名→Qwen 时预算不会在映射前消失。

#### R03：Ali 未传 top_p 时被强制改为 0.001

- 上游：2399de97d。
- main：relay/channel/ali/text.go:12-19。
- 触发：普通阿里文本请求省略 top_p。lo.FromPtrOr(nil,0) 随后进入 <=0 分支。
- 影响：模型默认采样被悄悄换成近贪心；明确边界 0/1 被改成三位小数，部分模型还拒绝该精度。
- main 修法：仅在非 nil 时钳制边界，按上游改为 0.01 / 0.99；省略值继续省略。

#### R04：Responses 兼容协议丢失 frequency/presence penalty

- 上游：253a74dd1。
- main：dto/openai_request.go:840；service/relayconvert/internal/oai_chat/to_oai_responses_req.go:375；service/relayconvert/internal/oai_responses/to_oai_chat_req.go:58。
- 触发：使用支持这些扩展字段的兼容上游，例如 vLLM；Chat → Responses 或 Responses → Chat。
- 影响：生成参数静默丢失。应表述为兼容上游问题，不能声称它们是官方 OpenAI Responses 标准参数。
- main 修法：DTO 保留两字段与显式 0，双向转换并校验合法浮点值。Codex 后端不支持这些字段；一并在 relay/channel/codex/adaptor.go:105 附近清理，避免添加 DTO 后把兼容字段误传 Codex。

#### R05：Chat → Responses 丢 prompt_cache_key

- 上游：7d09c6954。
- main：dto/openai_request.go:76、862 已有字段；service/relayconvert/internal/oai_chat/to_oai_responses_req.go:375 未复制。反向 service/relayconvert/internal/oai_responses/to_oai_chat_req.go:90 已复制。
- 影响：仅正向转换时用户缓存路由键丢失，可能降低缓存命中、增加成本；不能断言每次必然多扣。
- main 修法：用 common.Marshal 序列化非空 key 到 Responses RawMessage 字段，覆盖引号、换行与 Unicode 的用例。

#### R06：无工具的 Chat 请求向 Claude 注入 tools:[]

- 上游：4442bb302。
- main：service/relayconvert/internal/oai_chat/to_claude_messages_req.go:31、103；dto/claude.go:222 的 Tools 是 any。
- 触发：转换的 claudeTools 是空切片，但赋入非 nil interface，omitempty 不省略。
- 影响：严格兼容上游可能拒绝请求，或把它识别为工具请求；是协议兼容问题，不是权限漏洞。
- main 修法：只在 len(claudeTools)>0 时设置 Tools。

#### R07：Claude 转换丢无参数工具，异常 schema 可触发 panic

- 上游：3dda1d50c。
- main：service/relayconvert/internal/oai_chat/to_claude_messages_req.go:34、41；Claude 渠道 relay/channel/claude/adaptor.go:95 会调用这个转换器。
- 触发：function 工具省略 parameters 时不满足 map 断言而被整项丢弃；参数 schema type 是非字符串时，params["type"].(string) panic。
- 影响：工具不可用或单请求 500。main.go:180 配有恢复中间件，不应夸大成直接杀死进程。
- main 修法：function 工具提供默认 object / 空 properties schema，拷贝并规范化 map，避免不安全类型断言。现有 Responses → Claude 的 service/relayconvert/internal/oai_responses/to_claude_messages_req.go:135 已有类似默认 schema；该方向主要是抽复用，无需声称也丢无参工具。

#### R08：Ollama 请求转换丢推理上下文 / 工具关联 / JSON schema

- 上游：8ad159a3b。
- main：relay/channel/ollama/dto.go:7、27、39、51；relay/channel/ollama/relay-ollama.go:22、29、91、116；relay/channel/ollama/stream.go:45。
- 触发：使用 reasoning_effort、assistant reasoning_content、含 tool_call_id 但不含 name 的工具结果，或 response_format=json_object/json_schema。
- 影响：推理开关不生效、历史 thinking 丢失、工具结果无法准确匹配；json_object 未识别，json_schema 把外层 {name,schema,...} 当实际 schema。返回工具 ID 也被一律改为 call_N。
- main 修法：保留请求/响应 call ID，按历史 call ID 回填 tool_name，映射 reasoning 和 thinking，json_schema 只传实际 schema。显式 stream:false 也应保留；main 的非流处理能聚合 NDJSON，当前省略 false 不必描述为必然请求失败，但确实使上游默认开启流式。

#### R09：Ollama done:true 末帧工具调用丢失

- 上游：8e5e09166。
- main：relay/channel/ollama/stream.go:131、176。
- 触发：工具调用、内容或 thinking 只存在 done:true 最终帧。
- 影响：只读取 usage 和结束原因，实际 payload 被丢；客户端看不到应执行的工具。
- main 修法：对 done 和非 done 帧共用 payload 转换，done 帧先输出真实 payload，再输出结束与用量；与 R08 工具 ID 改动一并处理。

#### R10：Ali 图片模型映射后仍按别名判断协议

- 上游：93d2df85f。
- main：relay/channel/ali/adaptor.go:113、119、146、153、186、195、198；relay/image_handler.go:36 先运行 ModelMappedHelper，:56 后调用 ConvertImageRequest。
- 触发：任意自定义模型别名映射到 qwen-image / wan 等具有同步、异步或特定编辑协议的模型。
- 影响：URL、X-DashScope-Async、请求结构、响应轮询方式选错，图片失败。模型映射已经发生，OriginModelName 仍故意保存原别名，不能用于协议决策。
- main 修法：所有协议判断统一用 UpstreamModelName，原始模型名保留用于展示和计费身份。

#### R11：Ali 忽略 response_format=b64_json

- 上游：0bee5d441。
- main：relay/channel/ali/image.go:303、351。
- 触发：ImageRequest.ResponseFormat 已解析，但处理器从无人设置的 c.GetString("response_format") 取值；仅其他渠道有这个上下文设置。
- 影响：客户端请求 Base64，实际仍得到 URL。
- main 修法：从 info.Request.(*dto.ImageRequest) 读取 ResponseFormat，类型断言安全失败时用默认空值。

#### R12：请求验证错误回 HTTP 500

- 上游：0f2a2075a。
- main：controller/relay.go:117-124；types/error.go:255、268。
- 触发：GetAndValidateRequest 返回错误，且不是请求体过大。
- 影响：缺字段、错误参数等客户端错误被标为服务器故障，客户端可能做无意义重试。此分支发生在 relay 重试循环之前，不能声称当前代码会在内部自动反复调用上游。
- main 修法：显式 400 + skip retry；413 分支保持原有行为。

#### R13：Gemini :countTokens 被当生成请求执行并可能计费

- 上游：81336fc69。
- main：router/relay-router.go:199、208；relay/channel/gemini/adaptor.go:166；relay/helper/valid_request.go:348。
- 触发：POST /v1beta/models/{model}:countTokens 携带正常 contents，通配路由接受并按 Gemini relay 处理；适配器只据流式状态生成 generateContent URL。
- 影响：本应计数的预检变成真实生成，返回体错误并可能收取生成费用。
- main 修法：在 Gemini 路由组的认证/渠道选择之前拒绝 :countTokens，复用 RelayNotFound；无需实现上游新计数功能。

#### R14：跨协议转 Chat 缺少 include_usage，main 还有能力位依赖

- 上游：610334dbd。
- main：relay/channel/openai/adaptor.go:45；relay/channel/advancedcustom/adaptor.go:70、94、118；relay/common/relay_info.go:219、290、379。
- 触发：Gemini 流式转 OpenAI Chat；或高级自定义渠道 Claude/Gemini/Responses 流式转 Chat。源协议通常不携带 stream_options。
- 影响：上游不返回权威 usage，结算回落估算，缓存、推理等分项可能丢失；可能少收或多收，不能统一说必然少收。
- main 修法：目标支持 stream_options 且请求流式时添加 IncludeUsage:true。关键：main 的 streamSupportedChannels 没有 ChannelTypeAdvancedCustom，默认 SupportStreamOptions=false；只复制上游 if 条件会使高级自定义三条修复失效，必须一并设置其目标能力。普通 OpenAI ConvertClaudeRequest:85 已做，不应重复列为缺陷。

#### R15：Realtime GA 模型仍被注入旧 beta 标记

- 上游：16bfae175。
- main：relay/channel/openai/adaptor.go:204-217。
- 触发：连接 gpt-realtime 等 GA 模型，含有或不含 Sec-WebSocket-Protocol 两条分支均注入 beta 标记。
- 影响：上游可返回 beta_api_shape_disabled，实时会话无法建立。
- main 修法：只有映射后的旧 -realtime-preview 模型保留 beta header / subprotocol，其余移除；新增模型列表属于便利性补充，不是必要修复主体。

#### R16：OpenAI 模型能力硬编码过窄/过宽

- 上游：49ec46966。
- main：dto/openai_request.go:222；relay/channel/openai/adaptor.go:323-338。
- 触发：gpt-6-astra 使用 max_tokens/不支持的采样参数，main 不识别该模型；GPT-5.1/5.2/5.4 的无推理模式允许采样，但 main 一律删除 temperature/top_p/logprobs。top_logprobs 还可能残留。
- 影响：前者被上游 400，后者客户端参数悄悄失效。
- main 修法：使用按明确模型/快照与有效 reasoning effort 判定的独立能力表；未知未来 GPT 名称保持透传。注意该提交最终实现并非提交说明第一段所述“所有 GPT>=5 一概限制”，不能仅凭 commit message 编码。

#### R17：Kimi K3 动态加载工具消息被丢弃

- 上游：6e10f9bc9。
- main：dto/openai_request.go:287；relay/compatible_handler.go:108 附近的 DTO 重编码；relay/chat_completions_via_responses.go:22 的系统提示注入。
- 触发：system 消息带 tools 数组用于 Kimi K3 动态工具加载。main Message 只保留 tool_calls，没有 tools。
- 影响：工具声明丢失，上游可报 required tool_choice requires tools；若只补字段但继续输出 content:null 或注入系统提示，也仍会破坏 Kimi 协议。
- main 修法：保留 Message.Tools；仅对这种无内容的工具加载消息省略 content；系统提示扫描/注入跳过工具加载消息；估算计入这些工具。仅用于对应 OpenAI 兼容透传，别将新 Kimi 字段无差别映射到其他协议。

#### R18：Gemini → Chat 工具历史 ID 不匹配，思考与零值也丢失

- 上游：0ed497f06 中可拆分的请求转换修复。
- main：service/relayconvert/internal/gemini_chat/to_oai_chat_req.go:14、63、74、99；dto/gemini.go:263。
- 触发：model content 内 functionCall，下一条 user content 内 functionResponse。每条 content 都新建 toolCalls，调用得 call_1，结果得 call_0；多轮/并行会继续冲突。
- 影响：正常工具对话转 OpenAI 后 tool_call_id 无匹配，上游 400 或上下文关联错误。FunctionCall 原始 ID 也因 DTO 缺字段丢失。转换函数同时未映射 ThinkingConfig，并把 thought 文本当普通内容；显式 topP/topK/maxOutputTokens=0 被 >0 条件丢弃。
- main 修法：优先小范围移植跨 content 的 call history：保留上游 ID，缺 ID 时按名称及顺序关联并避免生成 ID 冲突；复用 main 当前推理策略补字段转换。无需引入整套 relaykit/hosted-tool 新架构。

#### R19：普通 Chat 流式倒数第二帧权威 usage 被丢弃

- 上游：0ed497f06 的 relay/channel/openai/relay-openai.go 小块。
- main：relay/channel/openai/relay-openai.go:129、135、164、200。
- 触发：非 audio 模型的兼容上游在倒数第二 SSE 帧报完整累计 usage，最后一帧不含 usage。
- 影响：main 仅为 audio 模型保留/读取倒数第二帧，其他模型回落本地估算，丢缓存/推理等真实数据。
- main 修法：所有模型保留倒数第二帧；先读最后有效 usage，只有最后没有有效 usage 时取第二末帧，并将 applyUsagePostProcessing 的数据源同步为采用的帧。已与计费子审计对齐，合并报告勿重复计数。

#### R20：流式重试未重置发送计数及转换状态

- 上游：bbd97446c 的 InitChannelMeta 重置部分。
- main：relay/common/relay_info.go:198；relay/channel/openai/helper.go:24、37；service/relayconvert/internal/oai_chat/to_claude_messages_resp.go:84、127；controller/relay.go:663。
- 触发：首次尝试在 HandleStreamFormat 增加 SendResponseCount 后解析/转换失败并允许重试；第二尝试复用 RelayInfo。即使首尝试还没实际写出响应，也可留下计数。
- 影响：Claude 新尝试跳过 message_start，或受上轮 Done/工具 index 状态影响。main 并无上游新 ChatToGeminiStreamState 字段，不应逐字移植无用状态字段。
- main 修法：按“每次尝试”重置 SendResponseCount 和实际存在的转换状态；保留请求级审计/计费累计信息。单独验证已经向下游输出后是否还允许重试，避免重置状态后在同一 SSE 中启动第二条响应。

#### R21：a20574136 的 Wan2 协议修复在 main 部分适用

- 上游：a20574136；上游实现文件虽为 JS 插件，main 有同业务的 Go TaskAdaptor，不是整体不适用。
- main：relay/channel/task/ali/constants.go:5 已列 wan2.7-t2v；relay/channel/task/ali/adaptor.go:55-62、137、350、370、385；relay/relay_adaptor.go:147 根据渠道选该适配器。
- 明确默认路径：Wan2.7-t2v 应使用 resolution+ratio。main :370 拒绝 size=720P，:391 默认塞 parameters.size=1280*720；上游修复删除 size 并按模型构造 resolution/ratio。
- 条件路径：管理员额外添加 wan2.2-kf2v-flash / wan2.2-s2v 时，main :137 总是 video-generation/video-synthesis；上游这两类改用 image2video/video-synthesis。s2v 要 image_url、按音频决定时长；main Input 只有 img_url 且默认 duration=5。它们未列在 main 默认 ModelList，但倍率表 :226、235 包含，适配器也没有按列表拒绝。
- 零值问题：PromptExtend/Watermark 是 bool,omitempty、Seed 是 int,omitempty，metadata 提供 false/0 会在出站时丢掉。上游 a205 保留显式 false/0。
- main 修法：在现有 Go adapter 中按映射后模型分支组装 Wan2.7-t2v 与既有 Wan2 图生/音驱协议，指针保留可选零值；不要引入 JS 插件或 Wan3 功能。Wan3 输入+输出时长计费新增路径在 main 没有明确支持，不能要求照搬。

#### R22：Chat → Claude 首帧即结束时提前丢终态 usage

- 上游：a043eef55 的 Chat-to-Claude terminal 子修复。
- main：service/relayconvert/internal/oai_chat/to_claude_messages_resp.go:127、241；relay/channel/openai/helper.go:37、162。
- 触发：首个 Chat chunk 已含 finish_reason，但 usage 由后续独立帧发送。
- 影响：首帧立即 message_stop 并置 Done=true，后续 usage 被 state.Done guard 丢掉，下游 Claude 看不到真实终态用量。main 自身账单可能仍由 OaiStreamHandler 读取末帧，不应一概说本实例结算错误。
- main 修法：首帧 finish 但无 usage 时延迟 message_stop；EOF 统一 finalize，确保只发送一次终态。上游同提交的 Gemini 新 registry 状态机并非全部缺失：main GeminiChatStreamHandler:194 已自有稳定 ID、STOP 与 tool 索引处理，不能整项说“main 完全没有终态处理”。

#### R23：部分渠道忽略“保留模型推理后缀”配置

- 上游：6b659fd61 的渠道守卫子修复。
- main：relay/channel/deepseek/adaptor.go:94、123；relay/channel/xai/adaptor.go:81；relay/channel/volcengine/adaptor.go:310；设置定义 setting/model_setting/global.go:71。
- 触发：用户配置保留模型 suffix，或只把映射后的上游模型加入保留列表。
- 影响：DeepSeek/XAI 仍截掉 suffix 并注入 thinking，Volc 只检查原始名，可能请求错模型/错误推理参数。
- main 修法：上述渠道同时检查原始和上游模型保留设置。6b 的统一 reasoning intent / max→xhigh 回归发生于上游新重构，main 无该层，不应整包移植。

### 低优先级、可按使用情况处理

- 3d5dc36f1：Gemini GET /v1/models 列表。main router/relay-router.go:32 调 RetrieveModel 而非 ListModels；middleware/auth.go:396 只支持 /v1/models/ 前缀，不支持恰好 /v1/models 的 query key。影响 Gemini SDK 列模型，修这两处即可。
- f51dd4d80：main 已允许高级自定义配置 /v1/responses/compact（dto/channel_settings.go:258），但 relay/responses_handler.go:27 硬白名单只有 OpenAI/Codex。使用高级自定义 compact 返回 400；按 main 白名单补 APITypeAdvancedCustom，不必引入上游其它新渠道。
- 76f7dafd2：common/audio.go:24 直接 switch 大小写敏感扩展名，service/token_counter.go:206 原样 filepath.Ext。上传 .MP3/.WAV 不能计算时长并被拒绝；解析前 ToLower 即可。不是溢出漏洞，main 时长转 quota 已独立钳制。
- 876903a8e：controller/channel_upstream_update.go:433-438，默认火山模型发现走 /v1/models 而非 /api/v3/models；专用 plan 分支已有单独 URL。影响管理员拉列表，修默认分支，不影响现有推理 URL。
- 84a79b680：service/error.go:126 附近 ToMessage 为空时未补原始 body 日志，不利诊断，不是 Relay 功能/安全修复。
- eab18a835：main relay/common/override.go:178 未把参数覆盖后的 reasoning effort 同步到日志；但 main relay/claude_handler.go:111 与 relay/gemini_handler.go:91 已用自有 ThinkingSummary 记推理配置。因此是部分日志准确性补充，不能说 Claude/Gemini 全未记录。

### 无须按上游原问题移植 / 仅新功能

- fa90b2312：上游 0ed 引入 ValidateGeminiThinkingConfig，错误比较 canonical level 与原始大写字符串；fa90 修该新增回归。main 没有此校验；relay/channel/gemini/adaptor.go:26 原生请求直接返回，dto/gemini.go:193 解码保留大小写，故不存在该“合法 HIGH 被网关拒绝”的同等问题。main ThinkingSummary:177 返回原始大小写，仅日志可选规范化；Gemini→Chat 丢 ThinkingConfig 另记 R18。
- bd585d78e：main relay/channel/aws/relay-aws.go:120 从 c.Request.Context 派生；流式 :343 使用该 context，:360 加 downstream abort cancel；controller/relay.go:227、618、668 断连后禁止重试。已经等价修复核心 Bedrock 取消链，无须覆盖 main 自有取消/用量收尾。上游每次事件 select ctx.Done 与 main SDK event channel 取消实现不同，不构成直接证明未修。
- 9bf328d97：main relay/channel/task/sora/adaptor.go:326 直接在 task.Data 上用 sjson 改公开 ID 返回，保留任意 provider 扩展字段，不存在上游新插件 renderer DTO 再编码丢字段路径。不能以无插件简单判断；本次已比对实际 Go 返回路径。
- 8461e5339：新增 NewAPI 渠道 adaptor 的 multipart 图片 bug。main 无 ChannelTypeNewAPI 路径；main advancedcustom 的图片编辑仍调用 OpenAI 图片转换且 DoRequest:231 使用 DoFormRequest，不需要移植新渠道。
- 3a9f41ee8：上游临时关闭 0ed 新增 /messages/count_tokens；main router 不注册该 endpoint，不存在新增控制器问题。
- 9724ef1b2 / cae3676ec：新增 DeepSeek / GLM Responses 端点。main 对应 adaptor 明确 not implemented；是功能缺口，不是既有请求回归。
- ba2e9287b / 5caafd3d8：Ollama 新增原生 Claude/Responses 转发及可选 Chat 兼容端点，不应为了修 R08/R09 整包引入开关和新协议。
- 8f6961c67：vLLM thinking_token_budget 新扩展字段，和已存在 Qwen thinking_budget 丢失问题分开；按用户实际使用可选支持。
- 85feb7a34：参数覆盖增加 user/group 上下文，新功能。
- 69a500298：高级自定义每路由透传开关，新功能；不是 main 已有转换路径的通用漏洞修复。
- 9fe0457ee：Responses WebSocket 大功能引入及随带安全/计费，main 无对应 WS 模块。本次只读了 stat 和与现有 JSON/Responses转换/校验边界相关的 diff，不能据此声称 52 个改动文件已逐块审计。其请求上限函数导出只是复用，不替换 main 现有 max-token 保护。

### 覆盖与未覆盖边界

已读相关生产 diff 并核 main 实际路径的提交（可用于“已深入核对”标记）：

1086038f5, 66ee6b8f9, 2399de97d, 8ad159a3b, 8e5e09166, 253a74dd1, 93d2df85f, 7d09c6954, 4442bb302, 3dda1d50c, 0f2a2075a, 0bee5d441, 81336fc69, 610334dbd, fa90b2312, 16bfae175, 84a79b680, f51dd4d80, a043eef55, bd585d78e, eab18a835, 3d5dc36f1, 0ed497f06, bbd97446c, 3a9f41ee8, 6b659fd61, 49ec46966, 6e10f9bc9, 9bf328d97, a20574136, 76f7dafd2, 876903a8e, 8461e5339。

已读生产 diff 作新增功能筛选（不标 main 必修）：9724ef1b2, cae3676ec, 8f6961c67, ba2e9287b, 5caafd3d8, 85feb7a34, 69a500298。

只作主题/文件范围筛选：0ab020206；9fe0457ee 只读部分共用边界；31d70fca3 仅观察前端迁移 stat（安全审计由其他子任务负责）。其余提交本子任务只扫过 249 条完整 log 标题，没有逐块读 diff，不能因此标为所有 249 个提交都已完成深审。

大提交 0ed497f06 与 bbd97446c：本报告覆盖上列请求转换、工具关联、terminal 和状态重置；账单标准化/缓存/多模态主体由计费子审计覆盖。上游新 hosted-tool、citation、loss-policy 等完整功能未作等价性完审，不以“main 缺新架构”判严重漏洞。

仓库版本未执行代码测试；结论来自静态确定路径，实际供应商当前服务器行为仅以当前上游修复代码/注释与合同测试为参照，未调用真实上游。若后续实施，上述每个修复应抽取针对协议输入/输出的确定性用例，Go 单次测试设 60 秒上限。


---

## 附录：249个提交逐条筛查索引

以下按时间先后列出全部提交。状态是对main既有代码的适用性判断；带“筛查”的条目主要检查提交主题和变更范围，不代表逐块安全验证。高风险和需修条目的证据见前述专题。原始提交标题保持原文，以便git检索。

1. **506e41c11 · 低优先级** — fix: add server-side sorting to user list, prevent client-side sort on paged data (#6194)
   main列表分页排序功能不同；非严重bug/漏洞。

2. **dc9aeab98 · 上游界面改动·筛查** — fix(data-table): update table body immediately when toggling columns (#6253)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

3. **923a17ca8 · 上游界面改动·筛查** — fix: update high-risk status code retry confirmation texts
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

4. **1086038f5 · 需修** — fix: prevent duplicate tool calls in Responses-to-Chat streaming (#6225)
   Responses->Chat流式工具调用去重。

5. **57746fc97 · 新功能** — feat(channel): support Codex upstream model discovery (#6184)
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

6. **a6cf42c0f · 新功能** — feat: support upstream model fetch for advanced custom channels (#5971)
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

7. **5a6c53d49 · 上游界面改动·筛查** — feat: standardize OpenAI Models label usage across components
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

8. **31d70fca3 · 需修·高** — refactor(auth): replace dashboard sessions with stateless tokens and session control (#6329)
   Cookie会话撤权；并可抽取Redis原子限流；不搬新认证架构。

9. **d0e23e1e0 · 文档/构建·筛查** — chore: include frontend in language stats
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

10. **16bfae175 · 需修** — fix: drop realtime beta header for ga models and register new ones (#6032)
   Realtime GA不注入beta头。

11. **2ef4cfff9 · 上游界面改动·筛查** — fix(playground): prevent model group option stretching (#6120)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

12. **270accc32 · 上游界面改动·筛查** — perf(data-table): optimize action column width allocation (#6135)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

13. **08677566f · 上游界面改动·筛查** — fix(web): show used quota for unlimited API keys (#6224)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

14. **e13d4033e · 需修** — fix(channel): improve proxy client compatibility and cache lifecycle (#6157)
   SOCKS默认端口/代理缓存失效；main取消机制已改进。

15. **4aa08f917 · 低优先级** — fix(playground): resolve auto group model listing (#6163)
   自动分组模型列表需展开实际组，影响playground列表。

16. **e0d515611 · 需修·高** — fix: prevent duplicate suno task refunds via cas status update (#6074)
   Suno状态CAS先于退款；保留后续bc14语义。

17. **172114422 · 非同等路径** — fix(auth): keep login state on rate-limited or failing token refresh
   上游新前端/新登录session流程专属，不直接移植。

18. **cb96ab020 · 文档/构建·筛查** — chore(github): migrate issue templates to required forms (#6452)
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

19. **cbd9b30aa · 文档/构建·筛查** — fix(github): restore issue form visibility (#6454)
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

20. **84a79b680 · 低优先级** — fix: log response body when parsed upstream error message is empty
   上游空错误补日志；避免将诊断日志当安全修复。

21. **27235a277 · 无该组件路径** — fix: prevent model create from wiping existing pricing for same name (#6365)
   上游新模型编辑器清空定价；classic未走该定价写入路径。

22. **bf8cfcc51 · 上游界面改动·筛查** — fix(model-mutate-drawer): prevent form reset on modelSettings refetch
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

23. **a0d0e5049 · 上游界面改动·筛查** — fix(web/channel): stabilize inline priority updates (#6415)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

24. **18b0b7631 · 上游界面改动·筛查** — refactor: rename channel priority update to channel field update
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

25. **eb4a1bd19 · 上游界面改动·筛查** — perf(json-editor): unify admin JSON editing experience (#6421)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

26. **257223be2 · 文档/构建·筛查** — chore(README): update AtomGit badge and remove Product Hunt badge
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

27. **5ede832d8 · 文档/构建·筛查** — chore(README): update AtomGit badge and remove Product Hunt badge
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

28. **ae17f2749 · 上游界面改动·筛查** — fix(json-code-editor): remove opaque line-number layer background hiding editor content
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

29. **8b41defbe · 新功能** — feat: add gemini-3-pro-image and gemini-3.1-flash-image GA models (#6371)
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

30. **08f88d25e · 新功能** — feat: support Tencent TokenHub API key via OpenAI-compatible protocol (#6232)
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

31. **ab65d2582 · 上游界面改动·筛查** — fix: allow topup amount input to be fully cleared (#6473)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

32. **3e1e72827 · 上游界面改动·筛查** — perf(web): debounce users page search input (#6474)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

33. **2d23cdf29 · 含需修局部** — feat: configurable tool pricing, Sub2API channel, and alpha search billing
   按实际工具调用计费；其他渠道与定价功能不搬。

34. **bc14c18f6 · 后续语义** — refactor: update task refund logic and remove legacy handling
   撤掉Suno退款自动对账扫描，修e0时一起参考。

35. **398cdafec · 新功能** — feat: add New API channel support
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

36. **f51dd4d80 · 低优先级** — fix: allow Advanced Custom Responses Compact
   AdvancedCustom已有compact路由但白名单不允许。

37. **86ac0f774 · 架构重构·筛查** — refactor: extract protocol conversion layer into standalone relaykit module (#6369)
   抽取relaykit/导入路径/代理包迁移；不整体带入main。

38. **60a1acb70 · 架构重构·筛查** — refactor: update import paths to use relaykit module
   抽取relaykit/导入路径/代理包迁移；不整体带入main。

39. **b8bb3f40a · 架构重构·筛查** — refactor: update import paths to use new types package
   抽取relaykit/导入路径/代理包迁移；不整体带入main。

40. **8aa5e754a · 架构重构·筛查** — refactor: rename trusted_proxies package to middleware and update function calls
   抽取relaykit/导入路径/代理包迁移；不整体带入main。

41. **8a7a49072 · 文档/构建·筛查** — feat: add workflow to sync GitHub releases to GitCode
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

42. **2ec6171fa · 文档/构建·筛查** — fix: replace ASCII apostrophes in release notes to prevent script breakage
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

43. **6d57d250f · 文档/构建·筛查** — feat: enhance GitCode release workflow with bootstrap asset and asset matrix
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

44. **f3ab2cff3 · 文档/构建·筛查** — feat: enhance GitCode release workflow to handle bootstrap and matrix assets
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

45. **a043eef55 · 含需修局部** — feat: implement Gemini to OpenAI chat stream conversion with state management and terminal handling
   Chat->Claude延迟终态以保留usage；不搬Gemini新状态机。

46. **b27b2b1d6 · 非同等路径** — fix(web): detect iPad login sessions correctly
   上游新前端/新登录session流程专属，不直接移植。

47. **e99a9bd86 · 新功能** — feat: add per-channel HTTP transport controls
   逐渠道HTTP传输策略；超时可独立修，无需该新设置。

48. **2cf3c8d71 · 文档/构建·筛查** — docs: update AGENTS.md
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

49. **f01c13b08 · 文档/构建·筛查** — feat: add CI workflow for backend and frontend build, typecheck, and test
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

50. **c3db41407 · 需修** — fix: 慢查询/错误 SQL 日志参数化 (#6493)
   SQL参数及错误消息脱敏，覆盖main只读副本。

51. **8e2bfe278 · 重构·低** — refactor: remove mutex from CustomEvent and update TCP connection handling in tests
   CustomEvent无实际使用的锁及测试TCP清理。

52. **1db6ae195 · 文档/构建·筛查** — feat: enhance CI workflow to include vetting for Go modules
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

53. **afe16c64c · 文档·筛查** — feat: add README.md for RelayKit module with capabilities and usage examples
   新relaykit模块说明文档，不属于main运行时修复。

54. **c27d1ef65 · 文档/构建·筛查** — Update .gitattributes
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

55. **cb4c8c02f · 新功能** — feat(oidc): 支持自定义 OIDC 登录显示名称 (#6012)
   OIDC显示名称/zstd请求支持/聊天预设。

56. **66ee6b8f9 · 需修** — fix: preserve Qwen thinking_budget passthrough (#5836)
   Qwen thinking_budget保留（含显式0）。

57. **0f9f668c6 · 新功能** — feat: support zstd request decompression (#6545)
   OIDC显示名称/zstd请求支持/聊天预设。

58. **84834eee8 · 新功能** — feat(logs): expose stream status to log owners (#6558)
   向用户展示流状态，与敏感日志隔离不同。

59. **8461e5339 · 无该路径** — fix(relay): preserve multipart image edits for New API channels (#6559)
   main无NewAPI渠道；非AdvancedCustom通用修复。

60. **e78e1db1e · 非同等路径** — fix(oauth): stop treating a foreign window.opener as a bind flow (#6425)
   上游新前端/新登录session流程专属，不直接移植。

61. **aa7d0d39a · 上游界面改动·筛查** — style: use text-sm for public header nav links to match other nav components (#6557)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

62. **9724ef1b2 · 新功能** — feat: deepseek responses api (#6562)
   新协议/字段/路由能力，未列为既有功能严重修复。

63. **df43f8015 · 需修·高** — fix(billing): settle tiered retries with final group (#6518)
   tiered跨组重试刷新计费与预扣；见计费3。

64. **cfaba1dd6 · 需修·高** — fix(billing): harden tiered retry group-switch billing (#6570)
   tiered跨组重试刷新计费与预扣；见计费3。

65. **bd585d78e · 已等价处理** — fix(aws): cancel Bedrock requests on client disconnect (#6589)
   Bedrock客户端断开取消；保留main自有收尾。

66. **0ab020206 · 新功能/语义变化·筛查** — Feat/auto group (#6590)
   自动分组选择扩展；保留main自研分组。

67. **d6b5ce99d · 需修** — fix(relay): set Request.GetBody so the HTTP/2 transport can transparently retry after an upstream stream reset (#6249)
   请求体独立重放与relay禁止自动跟随重定向。

68. **ea4f02101 · 关联改动** — refactor(relay): move replay metadata onto request bodies
   d6后续ReplayableBody重构；可参考最终设计。

69. **0cd9dc85e · 需修·高** — Merge commit from fork
   绑定和access_token轮换只写目标列；附main EmailBind同类。

70. **c9bc03864 · 上游界面改动·筛查** — feat(channels): refine fetched model categorization (#6632)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

71. **b941253ae · 需修** — fix: test Claude/Gemini endpoints with native request format (#6698)
   main渠道测试仍用OpenAI测试体；需分别构造原生请求并匹配路由，保留main恢复策略。

72. **1da23d6b3 · 需修** — feat(rate-limit): add user critical rate limit middleware for access token and aff transfer routes
   验证与按用户限流需适配无TOTP/Passkey账户。

73. **e926e5cac · 已等价处理** — fix: 修复兑换码额度精度损失 (#6685)
   classic兑换码已有displayAmountToQuota舍入转换。

74. **5c3abffe8 · 文档/构建·筛查** — CI: enhance release synchronization workflow with optional file syncing
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

75. **2399de97d · 需修** — fix(ali): stop injecting top_p into requests that omit it (#6674)
   Ali省略top_p不注入0.001。

76. **823e26304 · 上游界面改动·筛查** — fix(channels): classify Qwen TTS models correctly (#6711)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

77. **5d3423bec · 新功能/策略变化·筛查** — feat(channels): add auto-disable-only channel test mode (#6728)
   渠道测试功能；保留main渠道恢复与自动禁用策略。

78. **7dd1000a1 · 上游界面改动·筛查** — perf(web): debounce server and large-list searches (#6727)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

79. **eab18a835 · 低优先级** — fix: record reasoning effort consistently in usage logs (#6641)
   参数覆盖后的reasoning日志同步；部分main已有。

80. **85feb7a34 · 新功能** — feat(relay): expose user and group context to parameter overrides (#6534)
   新协议/字段/路由能力，未列为既有功能严重修复。

81. **8ad159a3b · 需修** — fix(ollama): preserve reasoning and tool-call context (#6605)
   Ollama推理/工具/JSON schema及末帧payload。

82. **d49160f0e · 低优先级** — fix: backend length validation (#5548)
   字符长度计数一致性，非注入修复。

83. **4cf9107f0 · 展示/诊断增强·筛查** — feat(billing): highlight matched conditional multipliers in logs (#6561)
   倍率匹配日志/时间定价预览；旧时间表达式bug另列。

84. **9c97e78ac · 低优先级** — fix(web): require confirmation before rotating access token (#6749)
   访问令牌轮换确认，与重新认证交互合并。

85. **253a74dd1 · 需修** — fix(relay): preserve presence/frequency penalty in Responses conversion (#6654)
   兼容Responses的penalty字段；Codex需清理不支持字段。

86. **bb234ff41 · 语义重构·筛查** — refactor(responses): remove compact model suffix handling (#6770)
   移除compact后缀；不当作main漏洞要求删除既有语义。

87. **4eaeefbdf · 上游界面改动·筛查** — fix: mobile sidebar (#6760)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

88. **ffeb1b24e · 非同等回归** — fix(web): refresh Turnstile token after login attempt (#6764)
   main Turnstile验证缓存在会话；改策略时再配前端刷新。

89. **3d5dc36f1 · 低优先级** — fix: 修复 Gemini 风格 /v1/models 列表请求 (#6199)
   Gemini SDK的/v1/models列表路由/query key。

90. **d7992672a · 需修·高** — fix(oauth): avoid overwriting user state when binding
   绑定和access_token轮换只写目标列；附main EmailBind同类。

91. **50e5377ea · 需修·高** — fix(topup): settle recharge orders atomically
   epay订单状态与入账需同一事务；见计费2。

92. **ccd535ef8 · 需修·高** — fix: harden concurrent quota and status updates
   并发预扣/缓存/渠道状态写回；见计费1。

93. **58d4e9bd3 · 需修** — fix(billing): 异步任务退款时同步减少 used_quota (#6795)
   异步退款账务；含MJ未扣费任务退款标记。

94. **15cfdedde · 上游界面改动·筛查** — fix(web): keep fetched model selection in sync with form (#6841)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

95. **93d2df85f · 需修** — fix(ali): 修复阿里图片模型映射后仍使用原始模型名判断协议的问题 (#6772)
   Ali图片协议使用映射后的模型。

96. **626058075 · 桌面依赖维护·筛查** — chore(deps): bump builder-util-runtime and electron-builder in /electron (#6467)
   main有Electron目录；仅桌面构建/运行时依赖。未核每个公告与桌面可利用性，不称已无风险。

97. **f250f3b58 · 安全维护** — chore(deps): bump dompurify from 3.4.11 to 3.4.13 in /web (#6735)
   DOMPurify版本落后；未证实classic满足公告利用条件。

98. **53a8739ee · 桌面依赖维护·筛查** — chore(deps-dev): bump fast-uri from 3.1.4 to 3.1.5 in /electron (#6846)
   main有Electron目录；仅桌面构建/运行时依赖。未核每个公告与桌面可利用性，不称已无风险。

99. **e5efc73cd · 无文件变化** — chore(deps-dev): bump tar from 7.5.16 to 7.5.22 in /electron (#6468)
   提交树相对父提交无变更；包升级标题不能作为实际升级证据。

100. **2a0ce3475 · 需修** — fix(topup): reject uncreditable orders before payment (#6845)
   付款前可入账校验+事务内余额容量上界，含main Ethereum。

101. **cf38105a9 · 桌面依赖维护·筛查** — chore(deps-dev): bump js-yaml from 4.3.0 to 4.3.1 in /electron (#6704)
   main有Electron目录；仅桌面构建/运行时依赖。未核每个公告与桌面可利用性，不称已无风险。

102. **bbf67df04 · 桌面依赖维护·筛查** — chore(deps-dev): bump electron from 39.8.5 to 39.8.10 in /electron (#6705)
   main有Electron目录；仅桌面构建/运行时依赖。未核每个公告与桌面可利用性，不称已无风险。

103. **47ba9d2c6 · 需修** — fix(topup): guard wallet quota during recharge
   付款前可入账校验+事务内余额容量上界，含main Ethereum。

104. **7d09c6954 · 需修** — fix: prompt_cache_key openai chat -> openai responses (#6861)
   Chat->Responses复制prompt_cache_key。

105. **e90a7c48e · 上游界面改动·筛查** — feat: add field passthrough controls for gateway channels (#6847)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

106. **4442bb302 · 需修** — fix(relay): stop injecting empty tools into Claude requests
   不向Claude注入空tools。

107. **116255f07 · 已等价处理** — fix(oauth): align custom binding response fields in frontend (#6818)
   classic绑定字段按provider_id比较。

108. **e2c7aa7b1 · 测试体系·筛查** — test(web): standardize frontend tests on Vitest (#6569)
   上游新前端统一Vitest，不替换classic工具链。

109. **3dda1d50c · 需修** — fix(relaykit): preserve parameterless tools in Claude conversion (#6862)
   Claude无参工具/异常schema安全处理。

110. **2b0efd848 · 新功能/编辑器重构·筛查** — refactor: advanced custom channel route editor (#6865)
   高级自定义路由编辑重构；不整体替换classic。

111. **4add708eb · 新功能/策略变化·筛查** — feat: channel test (#6917)
   渠道测试功能；保留main渠道恢复与自动禁用策略。

112. **137d1171f · 前端体验·筛查** — feat(web): fade in streamed response words and harden playground editor (#6895)
   流式淡入/编辑器交互，未列重大问题。

113. **f11641428 · 需修** — fix: settle Responses cached token usage (#6892)
   Responses转换后BillingUsage缓存字段补齐；原生已处理。

114. **2d8e50bf3 · 非同等路径** — refactor(web): prevent credential autofill in usage log filters (#6966)
   上游新前端/新登录session流程专属，不直接移植。

115. **a073f74b3 · 兼容注意** — refactor: deprecate int32 (#7025)
   53位钱包扩容属类型/schema变化，main保留int32。

116. **8c25eee71 · 文档/构建·筛查** — chore(build): upgrade Bun to 1.4.0
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

117. **8f6961c67 · 新功能** — feat: vllm thinking_token_budget (#7027)
   新协议/字段/路由能力，未列为既有功能严重修复。

118. **cae3676ec · 新功能** — feat: glm chanel /v1/responses (#7050)
   新协议/字段/路由能力，未列为既有功能严重修复。

119. **ba2e9287b · 新功能** — feat(ollama): passthrough Claude Messages and OpenAI Responses (#7051)
   新协议/字段/路由能力，未列为既有功能严重修复。

120. **e468b7391 · 文档/构建·筛查** — docs: update PR template and remove PR Check workflow (#7053)
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

121. **692e8d6ee · 非同等路径** — fix(web): restore admin unbinding for built-in providers (#6987)
   上游新前端/新登录session流程专属，不直接移植。

122. **ac381acf4 · 需修·高** — fix(billing): 修复时间规则恒真表达式导致倍率全天生效 (#6934)
   classic时间范围生成恒真式；已本地复现。

123. **7037ac15b · 文档/构建·筛查** — fix(docker): add relaykit go.mod to dev build context (#7072)
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

124. **eb48396d5 · 架构/新功能·筛查** — feat(task): replace built-in task adaptors with a sandboxed JS plugin system (#7076)
   JS任务插件替换内置Go适配器；相关旧bug按实际Go路径另列。

125. **0f2a2075a · 需修** — fix(relay): 请求参数校验错误返回 HTTP 400 (#6774)
   请求验证错误明确400/413。

126. **98d50d538 · 已等价处理** — fix(web): recheck setup status after page reload (#6968)
   classic不使用localStorage永久setup完成标记。

127. **b80d633cf · 新功能** — feat(auth): encrypt password login transport
   密码传输加密及开关，不是现存重新认证缺口的前置。

128. **8454082f9 · 新功能** — feat(chat): add AQBot preset (#7079)
   OIDC显示名称/zstd请求支持/聊天预设。

129. **918427d8a · 新功能** — feat(auth): make password encryption opt-in #6743
   密码传输加密及开关，不是现存重新认证缺口的前置。

130. **6c22550ea · 上游新增路径·筛查** — feat(task): resolve channel-mapped aliases and case variants for plugin models
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

131. **66031a09d · 条件性** — fix(model): disable PostgreSQL prepared statements for pooler compatibility
   PG事务池兼容：关PrepareStmt+Valuer/Scanner+驱动适配。

132. **0bee5d441 · 需修** — fix(ali): honor image response format (#5513) (#7048)
   Ali实际尊重b64_json。

133. **dc4732cfe · 上游界面改动·筛查** — feat(web): factory task plugins update only with the system
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

134. **b5b94bc68 · 低优先级** — fix(subscription): 无有效订阅时前端如实显示「仅用订阅」偏好 (#6222) (#7086)
   classic同样错显订阅only为钱包；只修显示/提示。

135. **1751f43ee · 需修** — fix(sqlite): enable WAL + working busy timeout + _txlock=immediate to stop concurrent write lockouts (#7030)
   SQLite默认DSN的busy/WAL/immediate；自定义DSN另查。

136. **6eb6f35ed · 条件性** — fix(model): return string from JSON column Valuers for pg simple protocol
   PG事务池兼容：关PrepareStmt+Valuer/Scanner+驱动适配。

137. **b518d0033 · 需修·高** — fix(relay): bound the wait for upstream response headers (fixes unbounded heap growth → OOM) (#6949)
   响应头等待无上限；直连/HTTP/SOCKS三处均需补。

138. **74158715c · 条件性** — fix initialize database
   PG事务池兼容：关PrepareStmt+Valuer/Scanner+驱动适配。

139. **69a41eead · 已被回滚** — fix(model): drop leftover prefill_groups unique constraints before AutoMigrate (#7100)
   该prefill迁移被2bf0820f4撤回；应参考最终版本。

140. **2bf0820f4 · 回滚提交** — Revert "fix(model): drop leftover prefill_groups unique constraints before Au…" (#7101)
   撤回69a41eead；不是独立修复候选。

141. **2b6f1dfef · 条件性** — fix(model): drop leftover prefill_groups unique constraints before AutoMigrate
   旧PG唯一约束迁移；取最终实现，不照抄回滚前版本。

142. **8c8c4153d · 升级前需修** — fix(log): preserve quota in usage statistics (#7108)
   GORM>=1.25.11双Scan清空quota；当前1.25.2未触发。

143. **27ff6a876 · 条件性** — fix(model): migrate legacy token key constraints
   旧PG唯一约束迁移；取最终实现，不照抄回滚前版本。

144. **67a0585d0 · 文档/构建·筛查** — fix(docs): correct Video API links across localized READMEs (#7116)
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

145. **b7017c251 · 需修** — fix(model): do not treat no-op system task state writes as lock loss (#7135)
   MySQL同秒no-op误判任务租约丢失。

146. **0ed497f06 · 含需修局部** — feat(relay): hosted-tool conversion fidelity, reasoning normalization, and billing usage integrity (#7137)
   工具历史ID、流式权威usage；其余新转换架构不搬。

147. **bbd97446c · 需修** — fix(relay): follow-up billing integrity and conversion completions (#7170)
   usage合并/零值/模态及每次重试状态清理。

148. **aece11d2f · 上游新增路径·筛查** — feat(plugin): add MiniMax-H3 /v2 video generation to the hailuo task … (#7168)
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

149. **d8ca0ed0b · 文档/构建·筛查** — chore: let owners use human PR templates
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

150. **73afad588 · 新增插件路径·筛查** — fix(plugin): account for MiniMax-H3 input media usage (#7171)
   仅JS插件功能/后续修复；main无对应JS运行时；旧Go同业务缺陷另列。

151. **057f71c23 · 需修** — fix(logs): isolate privileged metadata
   用户日志历史及新增字段脱敏。

152. **219c9e063 · 新功能/优化** — 优化匿名冷启动与公开内容接口的重复回源请求 (#7166)
   公开内容缓存/前端请求去重，非严重漏洞。

153. **9f506dd7f · 关联改动** — refactor(logs): simplify LogOther projection and dedupe sensitive keys
   057后续日志投影/字段归并；按main结构适配。

154. **9df450fe5 · 上游新增路径·筛查** — feat(task): give polling hooks a real query context, host HTTP classification, and bounded poll failures
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

155. **36dbbf0f7 · 无该新增路径** — fix: keep ETag valid across different JSON packages
   ETag公开内容缓存新路径修复；main无此revalidated_response。

156. **8f5ab8e40 · 文档/构建·筛查** — fix(ci): resolve release version from trigger tag
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

157. **32c261923 · 上游新增路径·筛查** — fix(task): explain 503 when a plugin-claimed model has no channel
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

158. **3a9f41ee8 · 无该路径** — fix: temp disable /messages/count_tokens
   上游新增/messages/count_tokens临时下线；main未注册。

159. **7c044d7c5 · 新功能/重构** — feat(relay): explicit @ model modifiers and canonical billing identity
   @模型后缀与计费身份；保留main自定义语义。

160. **6b659fd61 · 含需修局部** — fix(relay): preserve reasoning effort without implicit remapping
   推理后缀保留策略检查原名及映射名；不搬新intent体系。

161. **d5803532b · 文档/构建·筛查** — docs: require expression pricing and consolidated tests
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

162. **eb99ab1b4 · 新功能** — feat(billing): add built-in expression pricing for gpt-6-astra
   模型默认表达式/定价转换能力，非必修漏洞。

163. **2cf177ac4 · 性能优化·筛查** — perf(common): 批量复制 RawMessage，优化请求深拷贝 (#7221)
   RawMessage拷贝批量化；不作为内存泄漏修复。

164. **49ec46966 · 条件性需修** — fix(relay): apply model-specific OpenAI chat capabilities (#7211)
   按具体GPT模型能力处理采样和max_tokens。

165. **d8cb17744 · 部分功能已有** — feat(security): add access token management and audit logs
   main有审计但不等于拥有上游完整token管理；安全缺口另列。

166. **9a8674425 · 低优先级** — fix(db): avoid redundant schema migrations on restart
   DDL比较规范化；含SQLite驱动升级需三库验证。

167. **45c3fbe8a · 需修·高** — fix(security): bind verification proofs to sessions and actions
   绑定/因子登记重新认证、一次性proof；不能只取日志改动。

168. **3e84ec0ab · 新功能** — feat(auth): migrate Telegram to unified OAuth
   多RP Passkey/Telegram协议迁移，与现存漏洞分开。

169. **a8729b5c3 · 需修** — feat(security): require verification for access token management
   验证与按用户限流需适配无TOTP/Passkey账户。

170. **0973dc2b8 · 需修·高** — feat(security): harden account binding and password changes
   绑定/因子登记重新认证、一次性proof；不能只取日志改动。

171. **3f8a50cf8 · 新功能/关联** — feat(audit): complete token and quota operation records
   审计埋点增强；不整包覆盖main自研审计。

172. **521cebf58 · 上游界面改动·筛查** — fix(dashboard): simplify completed setup guide
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

173. **6f2333990 · 需修·高** — feat(auth): unify login verification and secure account deletion
   统一登录TOTP/Passkey策略与删号验证。

174. **0c76e4dae · 架构/功能重构·筛查** — feat(models): rework model/vendor management and pricing
   模型/厂商/可见性/定价；不整体替换main Pricing结构。

175. **3b4652269 · 新增插件路径·筛查** — feat(ali): support wan3.0 all-in-one video models
   仅JS插件功能/后续修复；main无对应JS运行时；旧Go同业务缺陷另列。

176. **6e10f9bc9 · 条件性需修** — fix(relay): preserve Kimi K3 dynamic tool loading messages
   Kimi K3动态工具加载消息完整保留。

177. **7bbe85bcb · 架构重构·筛查** — refactor(json): route JSON helpers through a host-injectable codec
   可注入JSON codec；无需为局部DTO修复引入。

178. **387a40914 · 上游界面改动·筛查** — fix(web): keep drawer popups interactive and shim storage in tests
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

179. **5c7cca015 · 已等价处理** — fix(perf): return hourly success-rate series for model health bar
   main指标已有带时间戳RecentSeries。

180. **6298b0f32 · 上游新增路径·筛查** — fix(plugin): suppress factory layer when disabling an overridden task plugin
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

181. **92bc7ff73 · 新增插件路径·筛查** — fix(plugins): make sunoapi alias-safe and lock alias echo across built-ins
   仅JS插件功能/后续修复；main无对应JS运行时；旧Go同业务缺陷另列。

182. **210734bb7 · 上游新增路径·筛查** — refactor(task): remove the custom-plugin layer switch
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

183. **bee45b58a · 上游界面改动·筛查** — fix(web): switch the pricing card grid to three columns at xl
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

184. **75e533209 · 上游界面改动·筛查** — feat(pricing): support site currency in pricing editors
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

185. **99974a814 · 上游新增路径·筛查** — feat(plugins): extend plugin metadata and icon support
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

186. **984330920 · 上游界面改动·筛查** — feat(web): improve plugin management and marketplace
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

187. **eb76b136b · 上游界面改动·筛查** — feat(channels): improve plugin channel setup and icons
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

188. **0e0ba152b · 上游界面改动·筛查** — feat(pricing): improve pricing editors and log display
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

189. **71c1fd7ca · 架构/功能重构·筛查** — feat(models): improve model listing, pricing and visibility filters
   模型/厂商/可见性/定价；不整体替换main Pricing结构。

190. **a5e41a893 · 上游界面改动·筛查** — feat(usage-logs): refine mobile layout and keep quick actions visible
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

191. **2bec37062 · 上游界面改动·筛查** — feat(web): refine API key and user quota displays
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

192. **8f72ecbbf · 上游界面改动·筛查** — feat(usage-logs): add searchable group filter
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

193. **551bb63ed · 上游界面改动·筛查** — fix(keys): show desktop quota amounts side by side
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

194. **bd22e45a7 · 上游界面改动·筛查** — style(keys): widen the desktop quota column
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

195. **950644c9d · 上游界面改动·筛查** — fix(keys): preserve spacing after desktop quota content
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

196. **524455fac · 新功能·筛查** — feat(redemptions): add batch deletion and optional file exports
   兑换码批删/文件导出，保留main现有兑换码功能。

197. **fff0635bb · 文档/构建·筛查** — docs: update project architecture and Go conventions
   文档、仓库治理或构建发布变更；非本次已确认main严重业务缺陷。

198. **ebe4c368f · 机械重构·筛查** — refactor: modernize Go code conventions
   Go习惯写法大范围改动；未逐行验证所有214文件。

199. **ea7cb0ba4 · 上游界面改动·筛查** — refactor(web): unify table cells and quota details
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

200. **9bf328d97 · 已等价处理** — fix: preserve provider fields in Sora video queries
   main Sora直接返回task.Data保留上游扩展字段。

201. **4fc9d1f1f · 无该路径** — fix(options): rebuild options table primary key and stop pricing writes resetting rows
   上游模型定价options全表重置路径在main不存在。

202. **c79b74b68 · 新功能/优化** — fix(frontend): deduplicate /api/status requests (#7189)
   公开内容缓存/前端请求去重，非严重漏洞。

203. **876903a8e · 低优先级** — fix: 修正火山方舟渠道获取模型列表的端点路径 (#7203)
   火山模型发现默认端点/api/v3/models。

204. **d52bdc0b4 · 展示/诊断增强·筛查** — feat(billing): add time-based pricing editor and expression previews
   倍率匹配日志/时间定价预览；旧时间表达式bug另列。

205. **12be9975c · 上游界面改动·筛查** — fix(web): unify server error notifications
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

206. **a20574136 · 部分适用** — fix(alibaba): correct Wan model protocols and usage accounting
   Go Ali adaptor也有Wan2.7协议缺陷；Wan3新路径排除。

207. **7cf9b473f · 上游界面改动·筛查** — fix(web): keep model pricing content in one scroll area
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

208. **064ed943e · 新功能** — feat(billing): support fixed per-request expression pricing
   缓存分模态/表达式变量/fixed()，main没有同等新功能。

209. **bdef11750 · 上游界面改动·筛查** — fix: restore add split button in advanced custom routes (#7289)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

210. **b6566f33d · 前端后续** — feat: clarify passkey website configuration
   上游多RP配置新界面后续；main无该配置。

211. **3e1b8b153 · 上游界面改动·筛查** — feat(plugins): display localized changelogs with English fallback
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

212. **3cea2bf79 · 新功能** — feat(relaykit): preserve cached input token breakdowns
   缓存分模态/表达式变量/fixed()，main没有同等新功能。

213. **f362c7c51 · 新功能** — feat(billingexpr): support image cache and quantity variables
   缓存分模态/表达式变量/fixed()，main没有同等新功能。

214. **f064bffa2 · 需修** — fix(billing): validate image quantities before reserving quota
   图片最终数量与预扣一致；main顶层n已有边界与按次倍率。

215. **f256e40bc · 新功能** — feat(billing): add expression defaults for GPT image models
   缓存分模态/表达式变量/fixed()，main没有同等新功能。

216. **25ec832fa · 新功能** — feat(pricing): convert legacy prices into expression drafts
   模型默认表达式/定价转换能力，非必修漏洞。

217. **39294418a · 上游界面改动·筛查** — feat(web): extend expression pricing editors and usage details
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

218. **b3e279464 · 上游界面改动·筛查** — feat(web): review legacy pricing conversion before applying drafts
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

219. **505805a4c · 界面功能·筛查** — feat(web): unify channel setup and refine model and settings editors
   渠道/模型编辑展示与默认BaseURL占位，非严重漏洞。

220. **d4c26bfb8 · 上游界面改动·筛查** — fix(channels): allow editing multi-key selection strategy
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

221. **ab489ab88 · 上游新增路径·筛查** — feat(channels): add plugin extensions to provider and model selection
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

222. **251b76d86 · 上游界面改动·筛查** — feat(web): add administrator update reminders
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

223. **74629e29f · 上游新增路径·筛查** — feat(plugins): enhance task streaming and model pricing
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

224. **385d2dfd1 · 新功能** — feat(auth): add safe multi-RP ID passkey support
   多RP Passkey/Telegram协议迁移，与现存漏洞分开。

225. **129f21b69 · 上游新增路径·筛查** — fix(plugins): normalize invalid UTF-8 in JSON state
   插件模型/轮询/工厂层/流式扩展；main无JS插件，未作完整语义深审。

226. **c9a110190 · 界面功能·筛查** — fix(channels): show built-in base URLs as placeholders
   渠道/模型编辑展示与默认BaseURL占位，非严重漏洞。

227. **be36cbb8f · 上游界面改动·筛查** — test(security): provide query client for passkey verification tests
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

228. **007d69942 · 条件性** — fix(model): migrate renamed prefill group unique indexes
   旧PG唯一约束迁移；取最终实现，不照抄回滚前版本。

229. **2ba615761 · 上游界面改动·筛查** — feat(web): share collapsible mobile table filters
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

230. **043ff99a5 · 条件性** — fix: handle legacy database constraints and scoped policies
   历史Casbin scope与PG约束；需成套检查。

231. **33142f0ae · 条件性·低** — fix: refine sign-in page behavior
   匿名status公开Passkey origins；含内部域名时有影响。

232. **7fd063819 · 前端后续** — fix(auth): restore saved compatible passkey domains
   上游多RP配置新界面后续；main无该配置。

233. **815217ba6 · 上游界面改动·筛查** — test(web): stabilize flaky frontend suite timeouts and motion visibility races (#7367)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

234. **d92612038 · 上游界面改动·筛查** — fix: unify model provider detection and add Wan icon (#7373)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

235. **d1c79d728 · 上游界面改动·筛查** — fix(dashboard): align weekly default range (#7355)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

236. **2509e25fa · 上游界面改动·筛查** — fix(web): prevent combobox dropdowns opening on dialog autofocus (#7365)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

237. **76f7dafd2 · 低优先级** — fix(audio): normalize file extension case in GetAudioDuration (#7321)
   音频扩展名大小写归一化。

238. **04c64734c · 上游界面改动·筛查** — fix(pricing): keep model status bar spacing uniform (#7284)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

239. **9fe0457ee · 新功能/部分筛查** — Add Responses WebSocket relay support (#5062)
   新Responses WebSocket；只核共用边界，非整提交安全审计。

240. **8529f209c · 新功能** — feat: vllm channel && sglang channel (#7332)
   模型发现/新增默认模型/渠道支持；非既有严重漏洞修复。

241. **2bfb89c1b · 上游界面改动·筛查** — fix(pricing): prevent sort menu layout shift (#7145)
   仅上游default后继界面/样式/测试；无main同组件路径，未判严重后端漏洞。

242. **8e5e09166 · 需修** — fix(ollama): preserve tool calls from final stream frame | 保留流式末尾帧的 tool calls (#7376)
   Ollama推理/工具/JSON schema及末帧payload。

243. **62f8db775 · 低优先级** — feat(channel): 修复获取DeepSeek余额错误 (#6814)
   DeepSeek余额币种/单位；不影响用户结算。

244. **d3874db61 · 低优先级** — feat(rate-limit): optimize in-memory limiter allocation and cleanup (#6807)
   内存limiter按需分配/LRU；未确认当前严重资源耗尽。

245. **5caafd3d8 · 新功能** — feat(ollama): add per-channel OpenAI-compatible chat switch (#7382)
   新协议/字段/路由能力，未列为既有功能严重修复。

246. **fa90b2312 · 上游新增回归** — fix(gemini): accept case-insensitive thinkingLevel and log canonical effort (#7387)
   main未引入该thinkingLevel校验；不属相同问题。

247. **81336fc69 · 需修** — fix(gemini): reject :countTokens as an unknown route instead of relaying it as generateContent (#7388)
   Gemini countTokens拒绝误转生成。

248. **610334dbd · 需修** — fix(relay): request stream usage on every cross-protocol conversion to OpenAI chat (#7389)
   跨协议流式Chat索取usage；AdvancedCustom能力位也需补。

249. **69a500298 · 新功能** — feat(channel): per-route pass-through for advanced custom channels (#7386)
   新协议/字段/路由能力，未列为既有功能严重修复。
