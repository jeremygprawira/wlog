// This file runs the shared database/sql checks against three real drivers: modernc
// SQLite in a temporary file, pgx stdlib, and go-sql-driver/mysql. The two server drivers
// skip when the machine holds no server, so the tests carry no tag.
package drivertest_test

import (
	"database/sql"
	"os"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"modernc.org/sqlite"

	wlogsql "github.com/jeremygprawira/wlog/store/sql"
	"github.com/jeremygprawira/wlog/store/sql/sqltest"
)

// TestSQLite runs the shared checks against SQLite.
func TestSQLite(t *testing.T) { sqltest.Run(t, sqliteFactory{}) }

// TestPGX runs the shared checks against pgx stdlib, and it skips when no server answers.
func TestPGX(t *testing.T) { sqltest.Run(t, pgxFactory{}) }

// TestMySQL runs the shared checks against go-sql-driver/mysql, and it skips when no
// server answers.
func TestMySQL(t *testing.T) { sqltest.Run(t, mysqlFactory{}) }

// sqliteRegister registers the wrapped SQLite driver once, because sql.Register refuses a
// name twice.
var sqliteRegister sync.Once

// sqliteFactory opens SQLite, which needs no server.
type sqliteFactory struct{}

// System returns the system of a SQLite record.
func (sqliteFactory) System() string { return "sqlite" }

// Open returns a database over the wrapped SQLite driver.
func (sqliteFactory) Open(t *testing.T) *sql.DB {
	t.Helper()
	sqliteRegister.Do(func() {
		sql.Register("wlogsql-sqlite", wlogsql.WrapDriver(&sqlite.Driver{}, wlogsql.WithSystem("sqlite")))
	})
	db, err := sql.Open("wlogsql-sqlite", "file:"+t.TempDir()+"/wlog.db")
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// pgxFactory opens pgx stdlib against the server in WLOG_TEST_POSTGRES_DSN.
type pgxFactory struct{}

// System returns the system of a Postgres record.
func (pgxFactory) System() string { return "postgresql" }

// Open returns a database over the wrapped pgx connector, and it skips with no DSN.
func (pgxFactory) Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WLOG_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set WLOG_TEST_POSTGRES_DSN to run the Postgres checks")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse the Postgres DSN: %v", err)
	}
	db := sql.OpenDB(wlogsql.Wrap(stdlib.GetConnector(*cfg), wlogsql.WithSystem("postgresql")))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mysqlFactory opens go-sql-driver/mysql against the server in WLOG_TEST_MYSQL_DSN.
type mysqlFactory struct{}

// System returns the system of a MySQL record.
func (mysqlFactory) System() string { return "mysql" }

// Open returns a database over the wrapped MySQL connector, and it skips with no DSN.
func (mysqlFactory) Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("WLOG_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set WLOG_TEST_MYSQL_DSN to run the MySQL checks")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse the MySQL DSN: %v", err)
	}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatalf("build the MySQL connector: %v", err)
	}
	db := sql.OpenDB(wlogsql.Wrap(connector, wlogsql.WithSystem("mysql")))
	t.Cleanup(func() { _ = db.Close() })
	return db
}
