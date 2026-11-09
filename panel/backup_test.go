package main

import (
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// AWS 官方文档 SigV4 示例（GET Bucket / ListObjects，max-keys=2&prefix=J）
func TestSignV4AWSExample(t *testing.T) {
	ts, _ := time.Parse("20060102T150405Z", "20130524T000000Z")
	q := canonicalQuery(url.Values{"max-keys": {"2"}, "prefix": {"J"}})
	auth, _ := signV4("GET", "examplebucket.s3.amazonaws.com", "/", q, sha256Hex(nil),
		"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", ts)
	want := "Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"
	if !strings.HasSuffix(auth, want) {
		t.Fatalf("签名不一致: %s", auth)
	}
}

func TestAWSEncode(t *testing.T) {
	if got := awsEncode("a b/c~d+e", true); got != "a%20b/c~d%2Be" {
		t.Fatal(got)
	}
}

func TestBackupTimeOrder(t *testing.T) {
	keys := []string{
		"dir/forwardz-panel-backup-20261110-030000.db.gz",
		"dir/forwardz-panel-backup-20261108-030000.db.gz",
		"dir/forwardz-panel-backup-20261109-150000.db.gz",
		"forwardz-panel-backup-20261109-030000.db.gz",
	}
	want := []string{keys[1], keys[3], keys[2], keys[0]}
	sorted := append([]string(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool { return backupTime(sorted[i]) < backupTime(sorted[j]) })
	for i := range want {
		if sorted[i] != want[i] {
			t.Fatalf("排序不对: %v", sorted)
		}
	}
}
