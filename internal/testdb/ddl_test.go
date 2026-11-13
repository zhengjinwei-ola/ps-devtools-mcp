package testdb

import (
	"strings"
	"testing"
)

func TestValidateDDLSQL_CreateTable(t *testing.T) {
	sql := "CREATE TABLE IF NOT EXISTS `xs_test` (`id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, PRIMARY KEY (`id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='t'"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("create table should pass: %v", err)
	}
}

func TestValidateDDLSQL_CreateTableNoIfNotExists(t *testing.T) {
	sql := "CREATE TABLE `xs_test` (`id` BIGINT NOT NULL, PRIMARY KEY (`id`))"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("create table without IF NOT EXISTS should pass: %v", err)
	}
}

func TestValidateDDLSQL_AlterAddColumn(t *testing.T) {
	sql := "ALTER TABLE `xs_bigarea` ADD COLUMN `boom_rocket_diamond_switch` tinyint(3) unsigned NOT NULL DEFAULT 0 COMMENT '开关'"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("alter add column should pass: %v", err)
	}
}

func TestValidateDDLSQL_AlterAddColumnShort(t *testing.T) {
	sql := "ALTER TABLE xs_bigarea ADD boom_rocket_x tinyint NOT NULL DEFAULT 0"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("alter add (no COLUMN keyword) should pass: %v", err)
	}
}

func TestValidateDDLSQL_RejectDrop(t *testing.T) {
	if err := ValidateDDLSQL("DROP TABLE xs_test"); err == nil {
		t.Fatal("DROP should be rejected")
	}
}

func TestValidateDDLSQL_RejectCreateIndex(t *testing.T) {
	if err := ValidateDDLSQL("CREATE INDEX idx_x ON xs_test (id)"); err == nil {
		t.Fatal("CREATE INDEX should be rejected")
	}
}

func TestValidateDDLSQL_RejectAlterDropColumn(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test DROP COLUMN id"); err == nil {
		t.Fatal("ALTER ... DROP should be rejected")
	}
}

func TestValidateDDLSQL_RejectAlterModify(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test MODIFY id BIGINT"); err == nil {
		t.Fatal("ALTER ... MODIFY should be rejected")
	}
}

func TestValidateDDLSQL_RejectCommentSmuggling(t *testing.T) {
	// 注释内夹带 DROP：注释被拒绝而不是被忽略
	if err := ValidateDDLSQL("CREATE TABLE xs_test (id INT) -- DROP TABLE xs_secret"); err == nil {
		t.Fatal("comment should be rejected")
	}
}

func TestValidateDDLSQL_RejectLiteralSmuggling(t *testing.T) {
	// 字面量内夹带不可执行语句本体也应保持白名单前缀校验（前缀仍需合法）
	if err := ValidateDDLSQL("SELECT 1"); err == nil {
		t.Fatal("SELECT prefix should be rejected by whitelist")
	}
}

func TestValidateDDLSQL_RejectMultipleStatements(t *testing.T) {
	if err := ValidateDDLSQL("CREATE TABLE a (id INT); DROP TABLE b"); err == nil {
		t.Fatal("multiple statements should be rejected")
	}
}

func TestValidateDDLSQL_RejectEmpty(t *testing.T) {
	if err := ValidateDDLSQL("  "); err == nil {
		t.Fatal("empty should be rejected")
	}
}

func TestValidateDDLSQL_TrailingSemicolonAllowed(t *testing.T) {
	sql := "CREATE TABLE xs_test (id INT);"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("single trailing semicolon should pass: %v", err)
	}
}

func TestValidateDDLSQL_RejectSet(t *testing.T) {
	if err := ValidateDDLSQL("SET GLOBAL innodb_flush_log_at_trx_commit=0"); err == nil {
		t.Fatal("SET should be rejected")
	}
}

func TestValidateDDLSQL_CreateTableWithCommentString(t *testing.T) {
	// COMMENT 内含逗号/括号等，字面量剥离后不应影响前缀匹配
	sql := "CREATE TABLE xs_test (`a` INT COMMENT 'x, y (z)') ENGINE=InnoDB"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("comment string should be stripped: %v", err)
	}
}

func TestValidateDDLSQL_RejectTooLong(t *testing.T) {
	if err := ValidateDDLSQL("CREATE TABLE xs_test (a INT, " + strings.Repeat("`c` INT,", 2000) + ")"); err == nil {
		t.Fatal("overlong statement should be rejected")
	}
}

func TestValidateDDLSQL_RejectAddIndex(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test ADD INDEX idx_uid (uid)"); err == nil {
		t.Fatal("ADD INDEX should be rejected")
	}
}

func TestValidateDDLSQL_RejectAddPrimaryKey(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test ADD PRIMARY KEY (id)"); err == nil {
		t.Fatal("ADD PRIMARY KEY should be rejected")
	}
}

func TestValidateDDLSQL_RejectAddConstraint(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test ADD CONSTRAINT fk_uid FOREIGN KEY (uid) REFERENCES xs_user (uid)"); err == nil {
		t.Fatal("ADD CONSTRAINT should be rejected")
	}
}

func TestValidateDDLSQL_RejectCreateTableAsSelect(t *testing.T) {
	if err := ValidateDDLSQL("CREATE TABLE xs_copy (id INT) SELECT uid FROM xs_user_profile"); err == nil {
		t.Fatal("CREATE TABLE ... SELECT should be rejected")
	}
}

func TestValidateDDLSQL_RejectMultiClauseAlter(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test ADD COLUMN a INT, ALGORITHM=INPLACE"); err == nil {
		t.Fatal("multi-clause ALTER should be rejected")
	}
}

func TestValidateDDLSQL_RejectMultiColumnAdd(t *testing.T) {
	if err := ValidateDDLSQL("ALTER TABLE xs_test ADD COLUMN (a INT, b INT)"); err == nil {
		t.Fatal("multi-column ADD should be rejected")
	}
}

func TestValidateDDLSQL_AlterAddColumnAfterKeyword(t *testing.T) {
	sql := "ALTER TABLE `xs_bigarea` ADD COLUMN `demo_col` tinyint NOT NULL DEFAULT 0 AFTER `ref_col`"
	if err := ValidateDDLSQL(sql); err != nil {
		t.Fatalf("alter add column with AFTER should pass: %v", err)
	}
}
