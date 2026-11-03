// wendao-upgrade 只在显式指定的可丢弃数据库中演练问道游戏表的升级与事务。
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/QuantumNous/new-api/scripts/wendao-upgrade/internal/fixtures"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type options struct {
	Driver     string
	SQLiteDir  string
	DSNEnv     string
	Disposable bool
	ReportPath string
	dsn        string
	database   string
	schema     string
}

type sampleSize struct {
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Bytes       int            `json:"bytes"`
	StoredBytes int            `json:"stored_bytes"`
	TopLevel    map[string]int `json:"top_level_value_bytes,omitempty"`
	ValidJSON   bool           `json:"valid_json"`
	RoundTrip   bool           `json:"round_trip,omitempty"`
	Refused     bool           `json:"refused,omitempty"`
	MySQL48KiB  bool           `json:"within_mysql_48kib_target"`
}

type columnReport struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	Definition string `json:"definition"`
}

type scenarioReport struct {
	Name     string         `json:"name"`
	Location string         `json:"location"`
	Checks   []string       `json:"passed_checks"`
	Columns  []columnReport `json:"columns"`
	Samples  []sampleSize   `json:"samples"`
}

type report struct {
	Driver        string           `json:"driver"`
	EngineVersion string           `json:"engine_version"`
	Database      string           `json:"database"`
	SaveSchema    int              `json:"save_schema"`
	Scope         string           `json:"scope"`
	Scenarios     []scenarioReport `json:"scenarios"`
}

var disposableName = regexp.MustCompile(`^wendao_check_[A-Za-z0-9_]+$`)
var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseOptions(args []string, getenv func(string) string) (options, error) {
	var o options
	fs := flag.NewFlagSet("wendao-upgrade", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.Driver, "driver", "", "sqlite, mysql or postgres")
	fs.StringVar(&o.SQLiteDir, "sqlite-dir", "", "parent directory for a new disposable SQLite directory")
	fs.StringVar(&o.DSNEnv, "dsn-env", "", "environment variable containing an explicit disposable database DSN")
	fs.BoolVar(&o.Disposable, "disposable", false, "acknowledge writes to an empty disposable database")
	fs.StringVar(&o.ReportPath, "report", "", "create a new JSON report file; existing files are refused")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return o, errors.New("invalid arguments; use -driver sqlite -sqlite-dir <directory>, or -driver mysql|postgres -dsn-env <name> -disposable")
	}
	switch o.Driver {
	case "sqlite":
		if strings.TrimSpace(o.SQLiteDir) == "" || o.DSNEnv != "" {
			return o, errors.New("sqlite requires an explicit -sqlite-dir and does not accept -dsn-env")
		}
	case "mysql", "postgres":
		if !o.Disposable || o.SQLiteDir != "" || !environmentName.MatchString(o.DSNEnv) {
			return o, errors.New("external databases require -dsn-env <name> and -disposable; -sqlite-dir is not accepted")
		}
		if o.DSNEnv == "SQL_DSN" || o.DSNEnv == "LOG_SQL_DSN" || o.DSNEnv == "SQL_DSN_READONLY" {
			return o, errors.New("use a dedicated disposable database environment variable, not a site database variable")
		}
		o.dsn = getenv(o.DSNEnv)
		if o.Driver == "mysql" {
			cfg, err := mysqldriver.ParseDSN(o.dsn)
			if err != nil || cfg == nil || !disposableName.MatchString(cfg.DBName) {
				return o, errors.New("invalid MySQL DSN or database name; require wendao_check_<suffix>")
			}
			o.database = cfg.DBName
		} else {
			u, err := url.Parse(o.dsn)
			if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.Fragment != "" {
				return o, errors.New("PostgreSQL DSN must be a postgres:// URL with an explicit database")
			}
			o.database = strings.TrimPrefix(u.Path, "/")
			if !disposableName.MatchString(o.database) {
				return o, errors.New("unsafe PostgreSQL database name; require wendao_check_<suffix>")
			}
			for key := range u.Query() {
				switch strings.ToLower(key) {
				case "dbname", "database", "service", "servicefile", "options", "search_path":
					return o, errors.New("PostgreSQL URL cannot override the database or schema through query parameters")
				}
			}
		}
	default:
		return o, errors.New("-driver sqlite|mysql|postgres must be explicitly specified")
	}
	return o, nil
}

func openDatabase(o options, prefix string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch o.Driver {
	case "sqlite":
		dialector = sqlite.Open(o.dsn)
	case "mysql":
		dialector = mysql.Open(o.dsn)
	case "postgres":
		dsn := o.dsn
		if o.schema != "" {
			u, err := url.Parse(dsn)
			if err != nil {
				return nil, errors.New("invalid internal PostgreSQL connection configuration")
			}
			query := u.Query()
			query.Set("search_path", o.schema)
			u.RawQuery = query.Encode()
			dsn = u.String()
		}
		dialector = postgres.Open(dsn)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}})
	if err != nil {
		return nil, errors.New("database connection failed; check the selected disposable database configuration (DSN omitted)")
	}
	connection, err := db.DB()
	if err != nil {
		return nil, errors.New("cannot access database connection")
	}
	connection.SetMaxOpenConns(1)
	connection.SetMaxIdleConns(1)
	return db, nil
}

func closeDatabase(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	return connection.Close()
}

// requireEmptyDatabase 检查整个指定库，PostgreSQL 也覆盖 search_path 之外的用户表。
func requireEmptyDatabase(db *gorm.DB, driver string) error {
	var count int64
	if driver == "postgres" {
		if err := db.Raw("SELECT COUNT(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r', 'p', 'f')").Scan(&count).Error; err != nil {
			return errors.New("cannot inspect disposable database tables")
		}
	} else {
		tables, err := db.Migrator().GetTables()
		if err != nil {
			return errors.New("cannot inspect disposable database tables")
		}
		count = int64(len(tables))
	}
	if count != 0 {
		return errors.New("refusing a nonempty database; choose a new empty disposable database")
	}
	return nil
}

// legacyGameSave/Session 是明确的旧组件表样本：缺少现行版本与计时列，不冒充某次整站历史发布。
type legacyGameSave struct {
	UserId int    `gorm:"primaryKey;autoIncrement:false"`
	Game   string `gorm:"type:varchar(32);primaryKey"`
	Data   string `gorm:"type:text"`
}

func (legacyGameSave) TableName() string { return "legacy_game_saves" }

type legacyGameSession struct {
	Id        int
	UserId    int
	Game      string `gorm:"type:varchar(32)"`
	Seed      int64  `gorm:"type:bigint"`
	Status    string `gorm:"type:varchar(16)"`
	State     string `gorm:"type:text"`
	CreatedAt int64  `gorm:"bigint"`
}

func (legacyGameSession) TableName() string { return "legacy_game_sessions" }

func run(o options, samples []fixtures.Sample) (report, error) {
	result := report{Driver: o.Driver, Database: o.database, SaveSchema: wendao.SaveSchema,
		Scope: "game-table component rehearsal only; not a full site startup, production migration, concurrency benchmark, or minimum supported server-version certification"}
	if o.Driver == "sqlite" {
		parent, err := filepath.Abs(o.SQLiteDir)
		if err != nil {
			return result, errors.New("cannot resolve explicit SQLite directory")
		}
		if err := os.MkdirAll(parent, 0700); err != nil {
			return result, errors.New("cannot create explicit SQLite parent directory")
		}
		directory, err := os.MkdirTemp(parent, "wendao_check_")
		if err != nil {
			return result, errors.New("cannot create unique disposable SQLite directory")
		}
		result.Database = filepath.Join(directory, "game-check.db")
		o.dsn = result.Database
	}
	db, err := openDatabase(o, "")
	if err != nil {
		return result, err
	}
	if err := requireEmptyDatabase(db, o.Driver); err != nil {
		_ = closeDatabase(db)
		return result, err
	}
	versionQuery := "SELECT version()"
	if o.Driver == "sqlite" {
		versionQuery = "SELECT sqlite_version()"
	}
	if err := db.Raw(versionQuery).Scan(&result.EngineVersion).Error; err != nil {
		_ = closeDatabase(db)
		return result, errors.New("cannot read database engine version")
	}
	if o.Driver != "sqlite" {
		var actual string
		query := "SELECT DATABASE()"
		if o.Driver == "postgres" {
			query = "SELECT current_database()"
		}
		if err := db.Raw(query).Scan(&actual).Error; err != nil || actual != o.database {
			_ = closeDatabase(db)
			return result, errors.New("connected database does not match the validated disposable database")
		}
	}
	if err := closeDatabase(db); err != nil {
		return result, errors.New("cannot close initial database connection")
	}
	previousDB, previousType := model.DB, common.MainDatabaseType()
	defer func() { model.DB = previousDB; common.SetMainDatabaseType(previousType) }()
	common.SetMainDatabaseType(common.DatabaseType(o.Driver))
	for _, name := range []string{"new", "legacy"} {
		scenarioOptions := o
		if o.Driver == "sqlite" {
			// SQLite 的固定索引名在整个文件内唯一，两组分别放在本次唯一目录下的新文件。
			scenarioOptions.dsn = filepath.Join(filepath.Dir(result.Database), name+".db")
		}
		if o.Driver == "postgres" {
			// PostgreSQL 的索引名在 schema 内唯一；现行模型索引名固定，不能只靠表前缀隔离两组。
			db, err := openDatabase(o, "")
			if err != nil {
				return result, err
			}
			scenarioOptions.schema = "wendao_check_" + name
			if err := db.Exec("CREATE SCHEMA " + scenarioOptions.schema).Error; err != nil {
				_ = closeDatabase(db)
				return result, errors.New("cannot create isolated PostgreSQL rehearsal schema")
			}
			if err := closeDatabase(db); err != nil {
				return result, errors.New("cannot close PostgreSQL schema setup connection")
			}
		}
		scenario, err := rehearse(scenarioOptions, name, samples)
		if err != nil {
			return result, fmt.Errorf("%s scenario: %w", name, err)
		}
		result.Scenarios = append(result.Scenarios, scenario)
	}
	return result, nil
}

func rehearse(o options, name string, samples []fixtures.Sample) (scenarioReport, error) {
	result := scenarioReport{Name: name, Location: o.database}
	if o.Driver == "sqlite" {
		result.Location = o.dsn
	} else if o.Driver == "postgres" {
		result.Location += "/" + o.schema
	}
	db, err := openDatabase(o, name+"_")
	if err != nil {
		return result, err
	}
	defer func() { _ = closeDatabase(db) }()
	model.DB = db
	if name == "legacy" {
		if err := db.AutoMigrate(&legacyGameSave{}, &legacyGameSession{}); err != nil {
			return result, errors.New("cannot create legacy component tables")
		}
		if db.Migrator().HasColumn(&legacyGameSave{}, "version") || db.Migrator().HasColumn(&legacyGameSession{}, "play_seconds") {
			return result, errors.New("legacy fixture unexpectedly contains new columns")
		}
		for i, sample := range samples {
			if sample.Kind == "save" {
				err = db.Create(&legacyGameSave{UserId: i + 1, Game: wendao.SaveKey, Data: sample.Data}).Error
			} else if sample.Kind == "session" {
				err = db.Create(&legacyGameSession{UserId: i + 1, Game: "wendao", Seed: 7, Status: model.GameSessionStatusPlaying, State: sample.Data, CreatedAt: 1793664000}).Error
			} else {
				return result, errors.New("unknown fixture kind")
			}
			if err != nil {
				return result, errors.New("cannot seed legacy component tables")
			}
		}
	}
	if err := db.AutoMigrate(&model.GameSave{}, &model.GameSession{}); err != nil {
		return result, errors.New("current game-table AutoMigrate failed")
	}
	if name == "legacy" {
		for i, sample := range samples {
			if sample.Kind == "save" {
				var row model.GameSave
				if err := db.Where("user_id = ? AND game = ?", i+1, wendao.SaveKey).First(&row).Error; err != nil || row.Data != sample.Data || row.Version != 0 {
					return result, errors.New("legacy save bytes or added-column default changed during migration")
				}
			} else {
				var row model.GameSession
				if err := db.Where("user_id = ?", i+1).First(&row).Error; err != nil || row.State != sample.Data || row.Version != 0 || row.PlaySeconds != 0 {
					return result, errors.New("legacy session bytes or added-column defaults changed during migration")
				}
			}
		}
		result.Checks = append(result.Checks, "legacy_rows_preserved_and_missing_columns_added")
	}
	result.Checks = append(result.Checks, "current_game_table_migration")
	for i, sample := range samples {
		size := sampleSize{Name: sample.Name, Kind: sample.Kind, Bytes: len([]byte(sample.Data)), StoredBytes: len([]byte(sample.Data)), MySQL48KiB: len([]byte(sample.Data)) <= 48*1024}
		var fields map[string]json.RawMessage
		if common.UnmarshalJsonStr(sample.Data, &fields) == nil {
			size.ValidJSON, size.TopLevel = true, map[string]int{}
			for key, value := range fields {
				size.TopLevel[key] = len(value)
			}
		}
		userId := 100 + i
		if sample.Kind == "save" {
			row := model.GameSave{UserId: userId, Game: wendao.SaveKey, Data: sample.Data, Version: 3}
			if err := db.Create(&row).Error; err != nil {
				return result, errors.New("cannot insert save sample")
			}
			data, updateErr := model.UpdateGameSave(userId, wendao.SaveKey, func(data string) (string, error) {
				return wendao.UpdateSave(data, func(*wendao.Save) error { return nil })
			})
			var saved model.GameSave
			if err := db.Where("user_id = ? AND game = ?", userId, wendao.SaveKey).First(&saved).Error; err != nil {
				return result, errors.New("cannot reread save sample")
			}
			size.StoredBytes = len([]byte(saved.Data))
			size.MySQL48KiB = max(size.Bytes, size.StoredBytes) <= 48*1024
			if sample.Name == "future" || sample.Name == "corrupt" {
				if updateErr == nil || saved.Data != sample.Data || saved.Version != 3 || (sample.Name == "future" && !errors.Is(updateErr, wendao.ErrNewerSave)) {
					return result, errors.New("invalid or future save was not refused without writing")
				}
				size.Refused = true
			} else {
				if updateErr != nil || saved.Data != data || saved.Version != 4 {
					return result, errors.New("save upgrade or version increment failed")
				}
				parsed, err := wendao.ParseSave(data)
				if err != nil {
					return result, errors.New("upgraded save cannot be parsed")
				}
				encoded, err := parsed.Encode()
				if err != nil || encoded != data {
					return result, errors.New("upgraded save is not stable across a second parse and encode")
				}
				size.RoundTrip = true
			}
		} else if sample.Kind == "session" {
			session, err := model.CreateGameSession(userId, "wendao", 7, sample.Data)
			if err != nil {
				return result, errors.New("cannot create session sample")
			}
			stale := *session
			if err := model.UpdateGameSessionState(session, sample.Data, "component-check"); err != nil {
				return result, errors.New("session state version update failed")
			}
			if err := model.UpdateGameSessionState(&stale, "stale", ""); !errors.Is(err, model.ErrGameRoundChanged) {
				return result, errors.New("stale session update was not refused")
			}
			current, err := model.GetUserGameSession(userId, "wendao", session.Id)
			if err != nil || current.State != sample.Data || current.Version != 1 {
				return result, errors.New("session state bytes or version were not preserved")
			}
			size.RoundTrip = true
		} else {
			return result, errors.New("unknown fixture kind")
		}
		result.Samples = append(result.Samples, size)
	}
	result.Checks = append(result.Checks, "save_round_trip_and_version", "future_and_corrupt_save_refused_unchanged", "session_bytes_and_stale_version_guard")
	if err := rehearseRollback(db); err != nil {
		return result, err
	}
	result.Checks = append(result.Checks, "callback_failure_rollback", "session_sql_update_then_save_failure_rolls_back_both", "two_save_callback_failure_rollback")
	var beforeSaves []model.GameSave
	var beforeSessions []model.GameSession
	if err := db.Order("user_id, game").Find(&beforeSaves).Error; err != nil {
		return result, errors.New("cannot snapshot saves before reopening")
	}
	if err := db.Order("id").Find(&beforeSessions).Error; err != nil {
		return result, errors.New("cannot snapshot sessions before reopening")
	}
	before, err := common.Marshal([]any{beforeSaves, beforeSessions})
	if err != nil {
		return result, errors.New("cannot serialize row snapshot")
	}
	if err := closeDatabase(db); err != nil {
		return result, errors.New("cannot close database before restart rehearsal")
	}
	db, err = openDatabase(o, name+"_")
	if err != nil {
		return result, err
	}
	model.DB = db
	if err := db.AutoMigrate(&model.GameSave{}, &model.GameSession{}); err != nil {
		return result, errors.New("second migration after reopening failed")
	}
	var afterSaves []model.GameSave
	var afterSessions []model.GameSession
	if err := db.Order("user_id, game").Find(&afterSaves).Error; err != nil {
		return result, errors.New("cannot reread saves after reopening")
	}
	if err := db.Order("id").Find(&afterSessions).Error; err != nil {
		return result, errors.New("cannot reread sessions after reopening")
	}
	// 模型 JSON 隐藏 Data/State，因此把原始载荷另行对照，行快照保护其余版本与计时列。
	after, err := common.Marshal([]any{afterSaves, afterSessions})
	if err != nil || string(before) != string(after) || len(beforeSaves) != len(afterSaves) || len(beforeSessions) != len(afterSessions) {
		return result, errors.New("row values changed during second migration")
	}
	for i := range beforeSaves {
		if beforeSaves[i].Data != afterSaves[i].Data {
			return result, errors.New("save bytes changed during second migration")
		}
	}
	for i := range beforeSessions {
		if beforeSessions[i].State != afterSessions[i].State || beforeSessions[i].Version != afterSessions[i].Version || beforeSessions[i].PlaySeconds != afterSessions[i].PlaySeconds || beforeSessions[i].PlayedAt != afterSessions[i].PlayedAt {
			return result, errors.New("session bytes or version changed during second migration")
		}
	}
	for i, sample := range samples {
		if sample.Kind == "save" {
			data, err := model.GetGameSave(100+i, wendao.SaveKey)
			if err != nil || data == "" {
				return result, errors.New("save missing after reopening")
			}
			if sample.Name == "future" || sample.Name == "corrupt" {
				if data != sample.Data {
					return result, errors.New("refused save changed after reopening")
				}
			} else if _, err := wendao.ParseSave(data); err != nil {
				return result, errors.New("save unreadable after reopening")
			}
		} else {
			session, err := model.GetPlayingGameSession(100+i, "wendao")
			if err != nil || session == nil || session.State != sample.Data || session.Version != 1 {
				return result, errors.New("session changed after reopening")
			}
		}
	}
	result.Checks = append(result.Checks, "reopen_and_second_migration_preserve_samples")
	for _, target := range []struct{ table, column string }{{name + "_game_saves", "data"}, {name + "_game_sessions", "state"}} {
		columns, err := db.Migrator().ColumnTypes(target.table)
		if err != nil {
			return result, errors.New("cannot inspect migrated column types")
		}
		found := false
		for _, column := range columns {
			if column.Name() == target.column {
				definition, ok := column.ColumnType()
				if !ok {
					definition = column.DatabaseTypeName()
				}
				result.Columns = append(result.Columns, columnReport{Table: target.table, Column: target.column, Definition: definition})
				found = true
			}
		}
		if !found {
			return result, errors.New("expected text column missing")
		}
	}
	return result, nil
}

func rehearseRollback(db *gorm.DB) error {
	refused := errors.New("fixture callback refused")
	const first, second = 9001, 9002
	for _, userId := range []int{first, second} {
		if _, err := model.UpdateGameSave(userId, wendao.SaveKey, func(string) (string, error) { return "before", nil }); err != nil {
			return errors.New("cannot prepare rollback save")
		}
	}
	if _, err := model.UpdateGameSave(first, wendao.SaveKey, func(string) (string, error) { return "after", refused }); !errors.Is(err, refused) {
		return errors.New("save callback failure was not returned")
	}
	session, err := model.CreateGameSession(first, "wendao", 7, "before")
	if err != nil {
		return errors.New("cannot prepare rollback session")
	}
	if err := model.UpdateGameSessionAndSave(session, "after", "", wendao.SaveKey, func(string) (string, error) { return "after", refused }); !errors.Is(err, refused) {
		return errors.New("combined session and save callback failure was not returned")
	}
	current, err := model.GetUserGameSession(first, "wendao", session.Id)
	if err != nil || current.State != "before" || current.Version != 0 {
		return errors.New("session SQL update escaped a rolled-back transaction")
	}
	if err := model.UpdateTwoGameSaves(first, second, wendao.SaveKey, func(string, string) (string, string, error) { return "after", "after", refused }); !errors.Is(err, refused) {
		return errors.New("two-save callback failure was not returned")
	}
	for _, userId := range []int{first, second} {
		var row model.GameSave
		if err := db.Where("user_id = ? AND game = ?", userId, wendao.SaveKey).First(&row).Error; err != nil || row.Data != "before" || row.Version != 1 {
			return errors.New("failed transaction changed save bytes or version")
		}
	}
	return nil
}

func execute(args []string, getenv func(string) string, output io.Writer) error {
	o, err := parseOptions(args, getenv)
	if err != nil {
		return err
	}
	if o.ReportPath != "" {
		file, err := os.OpenFile(o.ReportPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("cannot create a new report file; select a writable path that does not exist")
		}
		defer file.Close()
		output = file
	}
	samples, err := fixtures.Build()
	if err != nil {
		return errors.New("cannot build deterministic game fixtures")
	}
	result, err := run(o, samples)
	if err != nil {
		return err
	}
	encoded, err := common.Marshal(result)
	if err != nil {
		return errors.New("cannot serialize rehearsal report")
	}
	_, err = fmt.Fprintln(output, string(encoded))
	return err
}

func main() {
	if err := execute(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
