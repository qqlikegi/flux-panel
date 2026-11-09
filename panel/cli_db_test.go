package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := openDB(filepath.Join(dir, "forwardz.db")); err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	mustExec("INSERT INTO system_setting (name, value) VALUES (?, ?)", "k'1", "中文 \"值\"\n换行")
	mustExec("INSERT INTO user_flow_daily (user_id, day, flow) VALUES (1, '2026-11-09', 123456789012)")

	bak := filepath.Join(dir, "out", "b.db")
	if err := backupDBTo(bak); err != nil {
		t.Fatal(err)
	}
	if err := backupDBTo(bak); err == nil {
		t.Fatal("已存在的文件不应被覆盖")
	}
	dump := filepath.Join(dir, "out", "d.sql")
	if err := exportSQLTo(dump); err != nil {
		t.Fatal(err)
	}

	script, _ := os.ReadFile(dump)
	imp, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "imported.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer imp.Close()
	if _, err := imp.Exec(string(script)); err != nil {
		t.Fatalf("导入 SQL 失败: %v", err)
	}
	for _, p := range []string{bak, filepath.Join(dir, "imported.db")} {
		c, err := sql.Open("sqlite", "file:"+filepath.ToSlash(p))
		if err != nil {
			t.Fatal(err)
		}
		var v string
		var flow int64
		if err := c.QueryRow("SELECT value FROM system_setting WHERE name = ?", "k'1").Scan(&v); err != nil || v != "中文 \"值\"\n换行" {
			t.Fatalf("%s: 设置值不一致 %q %v", p, v, err)
		}
		if err := c.QueryRow("SELECT flow FROM user_flow_daily WHERE user_id = 1").Scan(&flow); err != nil || flow != 123456789012 {
			t.Fatalf("%s: 流量不一致 %d %v", p, flow, err)
		}
		var users int
		c.QueryRow("SELECT COUNT(*) FROM user").Scan(&users)
		if users == 0 {
			t.Fatalf("%s: 缺少用户表数据", p)
		}
		c.Close()
	}
}
