# 自定义 OAuth 提供方：PKCE

在管理后台新建或编辑自定义 OAuth 提供方，在「高级选项」打开「启用 PKCE（S256）」。默认关闭，不影响已有提供方。开启后，服务端为每次授权生成独立的 verifier，与 OAuth state 一起保存 5 分钟；回调核销 state 时清除 verifier，并用它换取 token。请使用系统设置里的「服务器地址」访问站点，并向提供方登记相同的回调地址。

## X 配置示例

在 X 开发者后台启用 OAuth 2.0，使用可通过 Client Secret 认证的应用，并登记 `https://你的站点域名/oauth/x`。

| 管理后台字段 | 值 |
| --- | --- |
| 名称 / Slug | `X` / `x` |
| Client ID / Client Secret | X 开发者后台对应的 OAuth 2.0 凭证 |
| 授权端点 | `https://twitter.com/i/oauth2/authorize` |
| Token 端点 | `https://api.twitter.com/2/oauth2/token` |
| 用户信息端点 | `https://api.twitter.com/2/users/me` |
| Scopes | `users.read` |
| 认证方式 | `Basic Auth 头` |
| 用户 ID / 用户名 / 显示名称字段 | `data.id` / `data.username` / `data.name` |
| 邮箱字段 | 留空（X 的该接口不返回默认的 `email` 字段） |
| 启用 PKCE（S256） | 开启 |
| 启用提供方 | 开启 |

[X 官方授权文档](https://docs.x.com/fundamentals/authentication/guides/v2-authentication-mapping)还为 `GET /2/users/me` 列出了 `tweet.read`。生产配置建议将 Scopes 填成 `users.read tweet.read`，以免授权成功后获取用户信息时权限不足。
