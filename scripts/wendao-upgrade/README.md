# 问道存档升级与容量演练

此工具复用当前 `wendao.ParseSave/Encode`、`model.GameSave`、`GameSession` 和模型事务，在可丢弃数据库中检查游戏表的新建、旧组件表加列、JSON 往返、版本检查、回滚与重新连接后的二次迁移。它不启动完整站点，不读取玩家数据，不发游戏币，也不执行历史二进制升级或并发压力测试。

依赖使用仓库 `go.mod`，当前 Go 为 **1.27.3**，不需要另外安装 Go 模块或数据库驱动。SQLite 随 Go 驱动提供；MySQL/PostgreSQL 服务由执行者在隔离环境中准备。

## 执行

从仓库根目录运行。无参数时拒绝连接，不读取 `SQL_DSN`、`LOG_SQL_DSN` 等站点配置。

```powershell
go test ./scripts/wendao-upgrade/... -count=1
go run ./scripts/wendao-upgrade -driver sqlite -sqlite-dir ./tmp/wendao-check -report ./sqlite-report.json
```

SQLite 必须显式指定父目录。每次在其下创建一个唯一的 `wendao_check_*` 子目录，分别保存 `new.db` 和 `legacy.db`；工具保留全部文件，不清理旧结果。

外部数据库必须预先创建一个**没有任何用户表的全新可丢弃库**，库名为 `wendao_check_<suffix>`，并使用独立环境变量传入。下面是本机隔离实例的配置示例，端口和用户名需替换为自己的测试环境：

```powershell
$env:WENDAO_CHECK_MYSQL_DSN='test_user:temporary_password@tcp(127.0.0.1:13306)/wendao_check_example?charset=utf8mb4&parseTime=true'
go run ./scripts/wendao-upgrade -driver mysql -dsn-env WENDAO_CHECK_MYSQL_DSN -disposable -report ./mysql-report.json

$env:WENDAO_CHECK_POSTGRES_DSN='postgres://test_user:temporary_password@127.0.0.1:15432/wendao_check_example?sslmode=disable'
go run ./scripts/wendao-upgrade -driver postgres -dsn-env WENDAO_CHECK_POSTGRES_DSN -disposable -report ./postgres-report.json
```

工具先核对实际连接库名与空表条件，再开始写入。MySQL 使用 `new_`、`legacy_` 两组表；PostgreSQL 创建 `wendao_check_new`、`wendao_check_legacy` 两个 schema，避免现行模型固定索引名冲突。PostgreSQL 只接受带明确库名的 URL，禁止通过查询参数覆盖库名或 `search_path`。工具不会创建/删除数据库、清表或删除文件；第二次运行必须选择新的空库。

`-report` 只创建新文件，现有文件一律拒绝覆盖。成功时文件是可直接解析的 JSON；失败返回非零退出码，可能留下空报告和部分演练数据，保留用于诊断。未指定 `-report` 时 JSON 写到标准输出末行；项目依赖在 `main` 前会输出一行初始化日志，采集报告时建议始终使用 `-report`。

## 检查范围与报告

两组演练均验证：

- 当前 `GameSave`、`GameSession` 的真实 GORM `AutoMigrate`；旧组件表缺少版本/计时列，新增列后原始 JSON 不变。
- 空存档、无 schema 宗门、旧卡牌战斗/秘境成长，以及 schema 13 的历练、游历、打坐、契约、满席下院、灵兽孵化等固定样本。
- 当前样本包括 12 名弟子、师承、四支派遣或八名驻守、四处下院、30 条宗门日志、七块灵田、12 条来访、80 张功法、30 张旧牌组、三个旧预设、全节点记录、洞府、祭炼、三炉丹、三只灵兽、周天试炼、32 张请求收据与持久回流报告。互斥活动分开保存。
- 秘境、大比会话使用真实 `{"run":...}` 和 `{"steps":0,"tournament":...}` 外层形状；保存状态字节、版本递增与旧版本拒写。
- 未来 schema、损坏 JSON 拒写后内容和行版本保持原样；旧战斗的未用丹符只退一次，进度和秘境成长保留。
- 存档回调失败、会话 SQL 更新后存档失败、双存档回调失败均保持事务原子性。
- 关闭连接、重新连接、再次迁移后，全部行载荷、版本和计时列保留。

JSON 报告包含引擎实际版本、表位置、`data/state` 列类型、原始/入库 UTF-8 字节数，以及原始 JSON 各顶层值的字节贡献（不含 key、逗号和括号开销）。`within_mysql_48kib_target` 只说明该样本是否在长期方案的 48 KiB 工程目标内。

这些是确定的合成容量样本，**不是最大存档证明，也不证明所有组合都能通过真实玩家流程到达**。累计次数、时间数字和历史成长会继续增加字节数；新增内容后应保留样本并更新报告。当前会话样本是开局状态，不声称覆盖所有深度的最大状态。工具只用底层模型验证这些会话载荷的保存，不替代真实接口恢复与整场结算。

## 2026-11-04 实测

使用全新本机临时目录和独立实例，未读取或写入生产库。两组共 13 个固定样本，结果见 [本轮验证记录](../../docs/wendao-upgrade-2026-11-04.md)。

本轮组件演练不能替代发版前完整站点旧版本升级/再次启动、真实 API 进行中对局恢复、MySQL 5.7 最低版本验证，以及浏览器桌面/手机和触屏验收。工具不改业务 schema、奖励配置或生产数据。
