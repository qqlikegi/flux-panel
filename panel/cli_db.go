package main

import (
	"bufio"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------- 命令行：备份 / 导出数据库（供 zf 菜单调用，面板运行中也可以安全执行） ----------

// backupDBTo 用 VACUUM INTO 生成一份完整、一致的数据库文件，可直接替换 data/gost.db 恢复
func backupDBTo(dst string) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("目标文件已存在: %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM INTO ?", dst); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// exportSQLTo 导出为纯文本 SQL（建表语句 + INSERT），可用 sqlite3 gost.db < xxx.sql 导入
func exportSQLTo(dst string) (err error) {
	dst, err = filepath.Abs(dst)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("目标文件已存在: %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
		}
	}()
	w := bufio.NewWriterSize(f, 1<<20)

	// 在一个只读事务里导出，保证数据前后一致
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type object struct{ typ, name, sql string }
	rows, err := tx.Query(`SELECT type, name, sql FROM sqlite_master
		WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name`)
	if err != nil {
		return err
	}
	var objs []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.typ, &o.name, &o.sql); err != nil {
			rows.Close()
			return err
		}
		objs = append(objs, o)
	}
	rows.Close()

	fmt.Fprintf(w, "-- flux-panel %s 数据库导出 %s\n", version, time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintln(w, "PRAGMA foreign_keys=OFF;")
	fmt.Fprintln(w, "BEGIN TRANSACTION;")
	for _, o := range objs {
		if o.typ != "table" {
			continue
		}
		fmt.Fprintf(w, "%s;\n", o.sql)
		if err := dumpTable(tx, w, o.name); err != nil {
			return fmt.Errorf("导出表 %s 失败: %w", o.name, err)
		}
	}
	for _, o := range objs {
		if o.typ != "table" {
			fmt.Fprintf(w, "%s;\n", o.sql)
		}
	}
	fmt.Fprintln(w, "COMMIT;")
	return w.Flush()
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func dumpTable(tx *sql.Tx, w *bufio.Writer, table string) error {
	rows, err := tx.Query("SELECT * FROM " + quoteIdent(table))
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	prefix := "INSERT INTO " + quoteIdent(table) + " VALUES("
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		w.WriteString(prefix)
		for i, v := range vals {
			if i > 0 {
				w.WriteByte(',')
			}
			w.WriteString(sqlLiteral(v))
		}
		w.WriteString(");\n")
	}
	return rows.Err()
}

func sqlLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		if x {
			return "1"
		}
		return "0"
	case []byte:
		return "X'" + hex.EncodeToString(x) + "'"
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case time.Time:
		return "'" + x.Format("2006-01-02 15:04:05") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(x), "'", "''") + "'"
	}
}
