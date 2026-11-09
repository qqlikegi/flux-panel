package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// dailyFile 按天写日志文件（logs/2006-01-02.log），保留最近 7 天
type dailyFile struct {
	mu   sync.Mutex
	dir  string
	day  string
	file *os.File
}

func (d *dailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	today := time.Now().Format("2006-01-02")
	if d.file == nil || d.day != today {
		if d.file != nil {
			_ = d.file.Close()
		}
		f, err := os.OpenFile(filepath.Join(d.dir, today+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			d.file = nil
			return len(p), nil // 日志文件写不了不影响服务
		}
		d.file, d.day = f, today
		go cleanupLogs(d.dir, 7)
	}
	return d.file.Write(p)
}

func cleanupLogs(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") && len(e.Name()) == len("2006-01-02.log") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

func setupLogger(dir string) {
	log.SetFlags(log.LstdFlags)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stdout, &dailyFile{dir: dir}))
}
