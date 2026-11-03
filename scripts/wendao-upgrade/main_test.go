package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scripts/wendao-upgrade/internal/fixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitDisposableDatabaseRequired(t *testing.T) {
	for i, args := range [][]string{
		nil,
		{"-driver", "sqlite"},
		{"-driver", "mysql", "-dsn-env", "TEST_DSN"},
		{"-driver", "postgres", "-disposable"},
		{"-driver", "mysql", "-dsn-env", "SQL_DSN", "-disposable"},
		{"-driver", "unknown"},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			readEnvironment := false
			_, err := parseOptions(args, func(string) string { readEnvironment = true; return "" })
			require.Error(t, err)
			assert.False(t, readEnvironment, "invalid options must be refused before looking up any DSN")
		})
	}
}

func TestUnsafeExternalDatabaseNamesAreRefusedWithoutLeakingDSN(t *testing.T) {
	for _, tc := range []struct{ driver, dsn string }{
		{"mysql", "root:private_password@tcp(localhost:3306)/production"},
		{"mysql", "root:private_password@tcp(localhost:3306)/wendao_check_"},
		{"mysql", "root:private_password@tcp(localhost:3306)/wendao_check_a%2Fb"},
		{"postgres", "host=localhost password=private_password dbname=production"},
		{"postgres", "postgres://root:private_password@localhost/production"},
		{"postgres", "postgres://root:private_password@localhost/wendao_check_a?dbname=production"},
		{"postgres", "postgres://root:private_password@localhost/wendao_check_a?options=-csearch_path=production"},
		{"postgres", "postgres://root:private_password@localhost/wendao_check_a%2Fb"},
	} {
		t.Run(tc.driver+"_"+tc.dsn[len(tc.dsn)-6:], func(t *testing.T) {
			_, err := parseOptions([]string{"-driver", tc.driver, "-dsn-env", "WENDAO_TEST_DSN", "-disposable"}, func(name string) string {
				assert.Equal(t, "WENDAO_TEST_DSN", name)
				return tc.dsn
			})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "private_password")
			assert.NotContains(t, err.Error(), tc.dsn)
		})
	}
	for _, tc := range []struct{ driver, dsn string }{
		{"mysql", "root:private_password@tcp(localhost:3306)/wendao_check_local"},
		{"postgres", "postgres://root:private_password@localhost/wendao_check_local?sslmode=disable"},
	} {
		o, err := parseOptions([]string{"-driver", tc.driver, "-dsn-env", "WENDAO_TEST_DSN", "-disposable"}, func(string) string { return tc.dsn })
		require.NoError(t, err)
		assert.Equal(t, "wendao_check_local", o.database)
	}
}

func TestRehearsalRefusesExistingDatabaseTables(t *testing.T) {
	db, err := openDatabase(options{Driver: "sqlite", dsn: filepath.Join(t.TempDir(), "existing.db")}, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closeDatabase(db)) })
	require.NoError(t, db.AutoMigrate(&model.GameSave{}))
	assert.ErrorContains(t, requireEmptyDatabase(db, "sqlite"), "nonempty database")
	assert.True(t, db.Migrator().HasTable(&model.GameSave{}), "refusal preserves preexisting data")
}

func TestSQLiteRehearsesNewAndLegacyTables(t *testing.T) {
	samples, err := fixtures.Build()
	require.NoError(t, err)
	result, err := run(options{Driver: "sqlite", SQLiteDir: t.TempDir()}, samples)
	require.NoError(t, err)
	assert.NotEmpty(t, result.EngineVersion)
	assert.FileExists(t, result.Database)
	require.Len(t, result.Scenarios, 2)
	for _, scenario := range result.Scenarios {
		assert.Contains(t, scenario.Checks, "reopen_and_second_migration_preserve_samples")
		assert.Contains(t, scenario.Checks, "session_sql_update_then_save_failure_rolls_back_both")
		require.Len(t, scenario.Columns, 2)
		assert.Len(t, scenario.Samples, len(samples))
		for _, sample := range scenario.Samples {
			if sample.Name == "empty" {
				assert.Zero(t, sample.Bytes)
				assert.Positive(t, sample.StoredBytes, "an empty input becomes an encoded new save")
			} else {
				assert.Positive(t, sample.Bytes)
			}
			if sample.Name == "future" || sample.Name == "corrupt" {
				assert.True(t, sample.Refused)
			} else {
				assert.True(t, sample.RoundTrip)
			}
		}
	}
	assert.Contains(t, result.Scenarios[1].Checks, "legacy_rows_preserved_and_missing_columns_added")
}

func TestCommandWritesJSONReportAndDoesNotReadDefaultDSN(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, execute([]string{"-driver", "sqlite", "-sqlite-dir", t.TempDir()}, func(string) string {
		t.Fatal("SQLite must not read any DSN environment variable")
		return ""
	}, &output))
	var result report
	require.NoError(t, common.Unmarshal(output.Bytes(), &result))
	assert.Equal(t, "sqlite", result.Driver)
	assert.Contains(t, result.Scope, "component rehearsal only")
}

func TestReportFileDoesNotOverwriteExistingResults(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "report.json")
	args := []string{"-driver", "sqlite", "-sqlite-dir", directory, "-report", path}
	var output bytes.Buffer
	require.NoError(t, execute(args, func(string) string { return "" }, &output))
	assert.Empty(t, output.String())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	var result report
	require.NoError(t, common.Unmarshal(before, &result))
	assert.Len(t, result.Scenarios, 2)
	require.ErrorContains(t, execute(args, func(string) string { return "" }, &output), "cannot create a new report file")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
