package main

import (
	"path/filepath"
	"testing"
)

func TestPanelFingerprint(t *testing.T) {
	if err := openDB(filepath.Join(t.TempDir(), "forwardz.db")); err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	ensurePanelFingerprint()
	fp := getSetting(fingerprintKey)
	if !fingerprintRe.MatchString(fp) {
		t.Fatalf("指纹格式不对: %q", fp)
	}
	ensurePanelFingerprint()
	if getSetting(fingerprintKey) != fp {
		t.Fatal("指纹生成后不应再变化")
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		f := newFingerprint()
		if !fingerprintRe.MatchString(f) || seen[f] {
			t.Fatalf("随机指纹异常: %q", f)
		}
		seen[f] = true
	}
	for _, bad := range []string{"", "abc", "abcdefghi", "abcd-123", "中文中文中文中文"} {
		if fingerprintRe.MatchString(bad) {
			t.Fatalf("不应接受 %q", bad)
		}
	}
}
