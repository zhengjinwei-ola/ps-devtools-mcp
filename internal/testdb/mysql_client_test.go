package testdb

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLClientQueryUsesReadOnlyTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	configDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer configDB.Close()

	statement := "SELECT id, name FROM xs_user_profile LIMIT 1"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(statement)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(int64(1), []byte("test")))
	mock.ExpectRollback()

	client := newMySQLClient(db, configDB, time.Second, 500, 2048)
	output, err := client.Query(context.Background(), QueryInput{Statement: statement, Engine: EngineXianshiSQL})
	if err != nil {
		t.Fatal(err)
	}
	if output.Engine != EngineXianshiSQL || output.RowCount != 1 || !strings.Contains(output.ResultJSON, `[1,"test"]`) {
		t.Fatalf("output = %+v", output)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLClientRejectsResultAboveLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	configDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer configDB.Close()

	statement := "SHOW TABLES"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(statement)).
		WillReturnRows(sqlmock.NewRows([]string{"table"}).AddRow("one").AddRow("two"))
	mock.ExpectRollback()

	client := newMySQLClient(db, configDB, time.Second, 1, 2048)
	_, err = client.Query(context.Background(), QueryInput{Statement: statement, Engine: EngineXianshiSQL})
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 rows") {
		t.Fatalf("error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMySQLConfig(t *testing.T) {
	valid := MySQLConfig{
		Host: "127.0.0.1", Port: 3306, User: "readonly", Password: "secret",
		XianshiDatabase: "xianshi", ConfigDatabase: "config",
	}
	if err := validateMySQLConfig(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ConfigDatabase = "config;DROP"
	if err := validateMySQLConfig(invalid); err == nil {
		t.Fatal("expected invalid database name error")
	}
}

func TestValidateDirectDatabaseScope(t *testing.T) {
	for _, statement := range []string{
		"SELECT authentication_string FROM mysql.user LIMIT 1",
		"SELECT authentication_string FROM `mysql`.`user` LIMIT 1",
		"SELECT profile.uid FROM xs_user_profile AS profile LIMIT 1",
		`SELECT authentication_string FROM "mysql"."user" LIMIT 1`,
		"SHOW TABLES FROM mysql",
		"SHOW GRANTS",
	} {
		if err := validateDirectDatabaseScope(statement); err == nil {
			t.Fatalf("expected scope error for %q", statement)
		}
	}
	for _, statement := range []string{
		"SELECT uid FROM xs_user_profile LIMIT 1",
		"SELECT JSON_EXTRACT(data, '$.nested.value') FROM xs_user_profile LIMIT 1",
		"SELECT 1.5 AS value LIMIT 1",
		"SHOW TABLES",
		"SHOW COLUMNS FROM xs_user_profile",
		"SHOW CREATE TABLE `xs_user_profile`",
	} {
		if err := validateDirectDatabaseScope(statement); err != nil {
			t.Fatalf("unexpected scope error for %q: %v", statement, err)
		}
	}
}
