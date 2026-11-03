# 问道整站旧版升级演练（Windows）

这是实际运行完整站点二进制与 HTTP 路由的演练，补充 `scripts/wendao-upgrade` 的游戏表组件检查。只操作显式指定的全新可丢弃库，不读生产配置，不改站点业务源码。

依赖：仓库要求的 Go 1.27.3、Python 3.13（只用标准库）、PowerShell，以及执行者准备的 SQLite/MySQL/PostgreSQL 隔离环境。外库名必须以 `wendao_check_` 开头，使用独立 DSN 环境变量；禁止使用 `SQL_DSN`、`LOG_SQL_DSN`。工具开始前检查实际库名和全库空表，PostgreSQL 覆盖所有非系统 schema。

## 准备二进制

先以 `git worktree add --detach <全新目录>/old-src v2.16.28` 创建旧源码副本。旧版本的 `web/classic/dist` 未入库，API 演练可在副本建立最小 `index.html`；要验收旧前端则必须在副本构建真实产物。当前源码构建使用真实 `web/classic/dist`。

完整站点默认监听 `:port`，Windows 隐藏进程也不便投递控制台信号；`build.ps1` 用 Go build overlay 将测试二进制限制在 `127.0.0.1`，并添加仅测试环境使用的停止文件轮询：文件出现后向**原有**退出信号通道送 `SIGTERM`，继续执行原来的 HTTP graceful shutdown 和数据库关闭。磁盘上的 `main.go`、路由、规则、模型与迁移代码不变。报告必须保留这两处测试辅助差异。

```powershell
$runRoot=Join-Path $env:TEMP ('wendao-site-check-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $runRoot | Out-Null
# 在此准备 old-src 与它的 web/classic/dist/index.html。
./scripts/wendao-site-upgrade/build.ps1 -Source (Join-Path $runRoot 'old-src') -OutputDirectory $runRoot -Name old
./scripts/wendao-site-upgrade/build.ps1 -Source . -OutputDirectory $runRoot -Name new
go build -o (Join-Path $runRoot 'db.exe') ./scripts/wendao-site-upgrade
```

## 执行

```powershell
python -I scripts/wendao-site-upgrade/check.py --driver sqlite --work-root $runRoot --old (Join-Path $runRoot 'old.exe') --new (Join-Path $runRoot 'new.exe') --db-helper (Join-Path $runRoot 'db.exe') --port 13689 --disposable

$env:WENDAO_SITE_MYSQL='test:temporary_password@tcp(127.0.0.1:13306)/wendao_check_example?charset=utf8mb4&parseTime=true'
python -I scripts/wendao-site-upgrade/check.py --driver mysql --dsn-env WENDAO_SITE_MYSQL --work-root $runRoot --old (Join-Path $runRoot 'old.exe') --new (Join-Path $runRoot 'new.exe') --db-helper (Join-Path $runRoot 'db.exe') --port 13691 --disposable

$env:WENDAO_SITE_POSTGRES='postgres://test:temporary_password@127.0.0.1:15432/wendao_check_example?sslmode=disable'
python -I scripts/wendao-site-upgrade/check.py --driver postgres --dsn-env WENDAO_SITE_POSTGRES --work-root $runRoot --old (Join-Path $runRoot 'old.exe') --new (Join-Path $runRoot 'new.exe') --db-helper (Join-Path $runRoot 'db.exe') --port 13693 --disposable
```

每次创建唯一工作子目录，留存日志、PID、停止文件与 `report.json`，不删除或复用旧结果。外库须每次换全新空库。进程使用 `Start-Process -WindowStyle Hidden`，子进程显式覆盖 DB/Redis/session 等配置，不加载仓库 `.env`；网络请求禁代理，只请求本机 HTTP。

每次启动前拒绝被占用端口；就绪检查还核对本轮进程存活与随机版本标识，避免把其他本机站点当作新进程执行写入。关闭超时保留 PID 跟踪和停止文件，报告失败并交由执行者检查，不强杀。隔离保护测试可用 `python -B -I scripts/wendao-site-upgrade/check_test.py` 执行。

升级样本在旧站通过真实 `/api/setup`、登录、管理配置、建角、剧情、开宗、灵田、洞府、战斗/大比/秘境接口建立。为了进入开宗条件，明确通过数据库助手将测试角色提升金丹并给材料；这是**合成升级 fixture**，不是正常养成耗时或完整战斗通关证明。

检查内容：schema7 旧档与旧卡牌战斗、秘境和大比进行中；升级后旧战斗按规则结束且进度保留，秘境/大比通过真实接口恢复并退出；新斗法开局/退走；完整二次启动后存档原样；未来 schema 999 拒读拒写；旧二进制拒绝 schema13 且不覆盖。`--keep-final` 专供本机 UI 验收，会保留最后站点并在本地报告记录随机临时 root 口令，使用后应创建报告中的 `stop_file` 关闭，且不要把带口令的报告提交仓库。

本轮实测见 [整站验证记录](../../docs/wendao-site-upgrade-2026-11-04.md)。工具不负责安装数据库、管理系统服务、生产升级或删库。数据库版本应记录实际版本，MySQL 5.7.44 只能证明该 5.7 版本，不能写成精确 5.7.8 实测。
