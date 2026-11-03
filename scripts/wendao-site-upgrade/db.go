// db 是整站升级演练的最小数据库助手，只接受显式可丢弃库。
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	driver := flag.String("driver", "", "sqlite|mysql|postgres")
	env := flag.String("dsn-env", "", "dedicated disposable DSN variable")
	path := flag.String("sqlite", "", "explicit disposable SQLite file")
	mode := flag.String("mode", "", "empty|save|write|version|counts")
	flag.Parse()
	var dialector gorm.Dialector
	var database string
	valid := regexp.MustCompile(`^wendao_check_[A-Za-z0-9_]+$`)
	if *driver == "sqlite" {
		if *path == "" || !strings.Contains(*path, "wendao-site-check-") {
			return errors.New("explicit wendao-site-check- SQLite path required")
		}
		dialector = sqlite.Open(*path)
	} else {
		if *env == "" || *env == "SQL_DSN" || *env == "LOG_SQL_DSN" {
			return errors.New("dedicated explicit DSN required")
		}
		dsn := os.Getenv(*env)
		switch *driver {
		case "mysql":
			cfg, err := mysqldriver.ParseDSN(dsn)
			if err != nil || !valid.MatchString(cfg.DBName) {
				return errors.New("unsafe disposable MySQL DSN")
			}
			database = cfg.DBName
			dialector = mysql.Open(dsn)
		case "postgres":
			u, err := url.Parse(dsn)
			if err != nil || !valid.MatchString(strings.TrimPrefix(u.Path, "/")) || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
				return errors.New("unsafe disposable PostgreSQL DSN")
			}
			for key := range u.Query() {
				if key != "sslmode" {
					return errors.New("PostgreSQL query overrides refused")
				}
			}
			database = strings.TrimPrefix(u.Path, "/")
			dialector = postgres.Open(dsn)
		default:
			return errors.New("explicit driver required")
		}
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("disposable database connection failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if *driver != "sqlite" {
		query := "SELECT DATABASE()"
		if *driver == "postgres" {
			query = "SELECT current_database()"
		}
		var actual string
		if err := db.Raw(query).Scan(&actual).Error; err != nil || actual != database {
			return errors.New("actual disposable database mismatch")
		}
	}
	switch *mode {
	case "empty":
		if *driver == "postgres" {
			var count int64
			if err := db.Raw("SELECT COUNT(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','f')").Scan(&count).Error; err != nil || count != 0 {
				return errors.New("PostgreSQL database is not empty or cannot inspect all schemas")
			}
			fmt.Println("empty")
			return nil
		}
		tables, err := db.Migrator().GetTables()
		if err != nil {
			return errors.New("table inspection failed")
		}
		if len(tables) != 0 {
			return errors.New("database is not empty")
		}
		fmt.Println("empty")
	case "save":
		var row struct{ Data string }
		if err := db.Table("game_saves").Where("user_id = ? AND game = ?", 1, "wendao").Take(&row).Error; err != nil {
			return errors.New("cannot read fixture save")
		}
		fmt.Print(row.Data)
	case "write":
		var payload map[string]any
		if err := common.DecodeJson(os.Stdin, &payload); err != nil {
			return errors.New("invalid fixture JSON")
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		result := db.Table("game_saves").Where("user_id = ? AND game = ?", 1, "wendao").Updates(map[string]any{"data": string(data), "version": gorm.Expr("version + 1")})
		if result.Error != nil || result.RowsAffected != 1 {
			return errors.New("fixture write failed")
		}
	case "version":
		query := "SELECT version()"
		if *driver == "sqlite" {
			query = "SELECT sqlite_version()"
		}
		var value string
		if err := db.Raw(query).Scan(&value).Error; err != nil {
			return errors.New("version query failed")
		}
		fmt.Println(value)
	case "counts":
		tables, err := db.Migrator().GetTables()
		if err != nil {
			return errors.New("table inspection failed")
		}
		fmt.Println(len(tables))
	default:
		return errors.New("explicit mode required")
	}
	return nil
}
