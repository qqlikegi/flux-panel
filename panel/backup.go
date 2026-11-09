package main

// 数据库备份到 Cloudflare R2（或任意 S3 兼容存储桶）。
// 每次备份：用 VACUUM INTO 生成一致的数据库快照 → gzip 压缩 → 上传；
// 上传成功后读取备份列表，按时间删除最旧的，最多保留 N 份（默认 3 份），循环滚动。

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------- S3 SigV4 客户端（只实现备份需要的几个接口） ----------

type s3Client struct {
	base      *url.URL // https://<account>.r2.cloudflarestorage.com
	bucket    string
	accessKey string
	secretKey string
	region    string
	http      *http.Client
}

func newS3Client(endpoint, bucket, ak, sk string) (*s3Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("请填写 S3 终结点地址")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("S3 终结点地址格式不正确")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("请填写存储桶名称")
	}
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		return nil, fmt.Errorf("请填写访问密钥 ID 和机密访问密钥")
	}
	return &s3Client{base: u, bucket: strings.TrimSpace(bucket), accessKey: strings.TrimSpace(ak), secretKey: strings.TrimSpace(sk),
		region: "auto", http: &http.Client{Timeout: 5 * time.Minute}}, nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// awsEncode 按 SigV4 规则做 URI 编码（只保留 A-Z a-z 0-9 - _ . ~）
func awsEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string{}, q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, awsEncode(k, false)+"="+awsEncode(v, false))
		}
	}
	return strings.Join(parts, "&")
}

// signV4 计算 Authorization 头（签名 host、x-amz-content-sha256、x-amz-date 三个头）
func signV4(method, host, path, query, payloadHash, accessKey, secretKey, region string, t time.Time) (auth, amzDate string) {
	amzDate = t.UTC().Format("20060102T150405Z")
	date := t.UTC().Format("20060102")
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n"
	creq := strings.Join([]string{method, path, query, canonicalHeaders, signedHeaders, payloadHash}, "\n")
	scope := date + "/" + region + "/s3/aws4_request"
	sts := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(creq))
	k := hmacSHA256([]byte("AWS4"+secretKey), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, sts))
	auth = "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + scope + ", SignedHeaders=" + signedHeaders + ", Signature=" + sig
	return
}

func (c *s3Client) do(method, key string, q url.Values, body []byte) (int, []byte, error) {
	path := "/" + c.bucket
	if key != "" {
		path += "/" + key
	}
	encPath := awsEncode(path, true)
	query := canonicalQuery(q)
	u := *c.base
	u.RawPath, u.Path = encPath, path
	u.RawQuery = query
	payloadHash := sha256Hex(body)
	auth, amzDate := signV4(method, u.Host, encPath, query, payloadHash, c.accessKey, c.secretKey, c.region, time.Now())
	req, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Authorization", auth)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	if method == http.MethodPut && key != "" {
		req.Header.Set("Content-Type", "application/gzip")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连接存储失败: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, data, nil
}

func s3Error(status int, data []byte) error {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	_ = xml.Unmarshal(data, &e)
	switch {
	case status == http.StatusForbidden || e.Code == "SignatureDoesNotMatch" || e.Code == "InvalidAccessKeyId":
		return fmt.Errorf("没有权限（%d %s）：请检查访问密钥 ID / 机密访问密钥是否正确、令牌是否有该存储桶的读写权限", status, e.Code)
	case e.Code != "":
		return fmt.Errorf("存储返回错误 %d %s：%s", status, e.Code, e.Message)
	default:
		return fmt.Errorf("存储返回错误 %d", status)
	}
}

type s3Object struct {
	Key          string `xml:"Key" json:"key"`
	LastModified string `xml:"LastModified" json:"lastModified"`
	Size         int64  `xml:"Size" json:"size"`
}

func (c *s3Client) list(prefix string) ([]s3Object, error) {
	var all []s3Object
	token := ""
	for page := 0; page < 50; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		var r struct {
			Contents  []s3Object `xml:"Contents"`
			Truncated bool       `xml:"IsTruncated"`
			Next      string     `xml:"NextContinuationToken"`
		}
		// 网络抖动导致响应不完整时重试一次
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			status, data, err := c.do(http.MethodGet, "", q, nil)
			if err != nil {
				lastErr = err
				continue
			}
			if status != http.StatusOK {
				return nil, s3Error(status, data)
			}
			r.Contents, r.Truncated, r.Next = nil, false, ""
			if err := xml.Unmarshal(data, &r); err != nil {
				lastErr = fmt.Errorf("解析备份列表失败: %v", err)
				continue
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			return nil, lastErr
		}
		all = append(all, r.Contents...)
		if !r.Truncated || r.Next == "" {
			break
		}
		token = r.Next
	}
	return all, nil
}

func (c *s3Client) put(key string, body []byte) error {
	status, data, err := c.do(http.MethodPut, key, nil, body)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return s3Error(status, data)
	}
	return nil
}

func (c *s3Client) delete(key string) error {
	status, data, err := c.do(http.MethodDelete, key, nil, nil)
	if err != nil {
		return err
	}
	if status/100 != 2 && status != http.StatusNotFound {
		return s3Error(status, data)
	}
	return nil
}

// ensureBucket 检查存储桶，不存在时自动创建
func (c *s3Client) ensureBucket() (created bool, err error) {
	status, data, err := c.do(http.MethodHead, "", nil, nil)
	if err != nil {
		return false, err
	}
	if status == http.StatusOK {
		return false, nil
	}
	if status != http.StatusNotFound {
		return false, s3Error(status, data)
	}
	status, data, err = c.do(http.MethodPut, "", nil, nil)
	if err != nil {
		return false, err
	}
	if status/100 != 2 {
		return false, fmt.Errorf("存储桶不存在，自动创建失败：%v", s3Error(status, data))
	}
	return true, nil
}

// ---------- 备份配置 ----------

type backupConfig struct {
	Enabled   bool   `json:"enabled"`
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey,omitempty"`
	Prefix    string `json:"prefix"`
	Keep      int    `json:"keep"`
	Mode      string `json:"mode"` // daily：每天固定时间；interval：每隔 N 小时
	Time      string `json:"time"` // HH:MM
	Hours     int    `json:"hours"`
}

const backupFilePrefix = "flux-panel-backup-"

func loadBackupConfig() backupConfig {
	c := backupConfig{
		Enabled:   getSetting("backup_enabled") == "true",
		Endpoint:  getSetting("backup_endpoint"),
		Bucket:    getSetting("backup_bucket"),
		AccessKey: getSetting("backup_access_key"),
		SecretKey: getSetting("backup_secret_key"),
		Prefix:    getSetting("backup_prefix"),
		Mode:      getSetting("backup_mode"),
		Time:      getSetting("backup_time"),
	}
	c.Keep, _ = strconv.Atoi(getSetting("backup_keep"))
	c.Hours, _ = strconv.Atoi(getSetting("backup_hours"))
	c.normalize()
	return c
}

func (c *backupConfig) normalize() {
	c.Endpoint, c.Bucket = strings.TrimSpace(c.Endpoint), strings.TrimSpace(c.Bucket)
	c.AccessKey, c.SecretKey = strings.TrimSpace(c.AccessKey), strings.TrimSpace(c.SecretKey)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	if c.Keep <= 0 {
		c.Keep = 3
	}
	if c.Mode != "interval" {
		c.Mode = "daily"
	}
	if _, err := time.Parse("15:04", c.Time); err != nil {
		c.Time = "03:00"
	}
	if c.Hours <= 0 {
		c.Hours = 24
	}
}

func (c backupConfig) keyPrefix() string {
	if c.Prefix == "" {
		return backupFilePrefix
	}
	return c.Prefix + "/" + backupFilePrefix
}

func (c backupConfig) client() (*s3Client, error) {
	return newS3Client(c.Endpoint, c.Bucket, c.AccessKey, c.SecretKey)
}

// nextRun 下次自动备份时间（anchor 为上次自动备份尝试的时间）
func (c backupConfig) nextRun(anchor time.Time) time.Time {
	if c.Mode == "interval" {
		return anchor.Add(time.Duration(c.Hours) * time.Hour)
	}
	t, _ := time.Parse("15:04", c.Time)
	next := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), t.Hour(), t.Minute(), 0, 0, time.Local)
	if !next.After(anchor) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func backupAnchor() time.Time {
	ms, _ := strconv.ParseInt(getSetting("backup_anchor"), 10, 64)
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func backupGet(c *Ctx) any {
	cfg := loadBackupConfig()
	hasSecret := cfg.SecretKey != ""
	cfg.SecretKey = ""
	var next any
	if cfg.Enabled {
		if a := backupAnchor(); !a.IsZero() {
			next = cfg.nextRun(a).UnixMilli()
		}
	}
	lastTime, _ := strconv.ParseInt(getSetting("backup_last_time"), 10, 64)
	return ok(map[string]any{
		"config": cfg, "hasSecret": hasSecret, "nextRun": next,
		"lastTime": lastTime, "lastStatus": getSetting("backup_last_status"),
		"lastMessage": getSetting("backup_last_msg"), "running": backupRunning(),
	})
}

// formConfig 读取页面提交的配置；机密访问密钥留空表示沿用已保存的
func formConfig(c *Ctx) backupConfig {
	var d backupConfig
	c.bind(&d)
	d.normalize()
	if d.SecretKey == "" {
		d.SecretKey = strings.TrimSpace(getSetting("backup_secret_key"))
	}
	return d
}

func backupSave(c *Ctx) any {
	d := formConfig(c)
	if d.Enabled {
		if _, err := d.client(); err != nil {
			return fail(err.Error())
		}
	}
	setSetting("backup_enabled", strconv.FormatBool(d.Enabled))
	setSetting("backup_endpoint", d.Endpoint)
	setSetting("backup_bucket", d.Bucket)
	setSetting("backup_access_key", d.AccessKey)
	setSetting("backup_secret_key", d.SecretKey)
	setSetting("backup_prefix", d.Prefix)
	setSetting("backup_keep", strconv.Itoa(d.Keep))
	setSetting("backup_mode", d.Mode)
	setSetting("backup_time", d.Time)
	setSetting("backup_hours", strconv.Itoa(d.Hours))
	// 从保存的这一刻开始计时：每天模式等到下一个设定时间，间隔模式等满 N 小时
	setSetting("backup_anchor", strconv.FormatInt(nowMs(), 10))
	return ok()
}

func backupTest(c *Ctx) any {
	d := formConfig(c)
	cl, err := d.client()
	if err != nil {
		return fail(err.Error())
	}
	created, err := cl.ensureBucket()
	if err != nil {
		return fail(err.Error())
	}
	objs, err := cl.list(d.keyPrefix())
	if err != nil {
		return fail(err.Error())
	}
	msg := fmt.Sprintf("连接成功，存储桶 %s 中已有 %d 份备份", d.Bucket, len(objs))
	if created {
		msg = fmt.Sprintf("连接成功，存储桶 %s 不存在，已自动创建", d.Bucket)
	}
	return ok(msg)
}

func backupRun(c *Ctx) any {
	cfg := loadBackupConfig()
	key, err := runBackup(cfg)
	if err != nil {
		return fail(err.Error())
	}
	return ok("备份成功：" + key)
}

func backupList(c *Ctx) any {
	cfg := loadBackupConfig()
	cl, err := cfg.client()
	if err != nil {
		return fail(err.Error())
	}
	objs, err := cl.list(cfg.keyPrefix())
	if err != nil {
		return fail(err.Error())
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key > objs[j].Key })
	return ok(objs)
}

// ---------- 执行备份 ----------

var backupMu sync.Mutex

func backupRunning() bool {
	if backupMu.TryLock() {
		backupMu.Unlock()
		return false
	}
	return true
}

func snapshotDB() ([]byte, error) {
	dir := filepath.Dir(cfg.DBPath)
	tmp := filepath.Join(dir, fmt.Sprintf(".backup-%d.db", time.Now().UnixNano()))
	defer os.Remove(tmp)
	if _, err := db.Exec("VACUUM INTO ?", tmp); err != nil {
		return nil, fmt.Errorf("生成数据库快照失败: %v", err)
	}
	raw, err := os.ReadFile(tmp)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// runBackup 上传一份新备份，然后按时间删除最旧的，最多保留 cfg.Keep 份
func runBackup(cfg backupConfig) (key string, err error) {
	if !backupMu.TryLock() {
		return "", fmt.Errorf("已有备份任务正在进行，请稍后再试")
	}
	defer backupMu.Unlock()
	defer func() {
		status, msg := "success", "备份成功："+key
		if err != nil {
			status, msg = "failed", err.Error()
			log.Printf("数据库备份失败: %v", err)
		} else {
			log.Printf("数据库备份成功: %s", key)
		}
		setSetting("backup_last_time", strconv.FormatInt(nowMs(), 10))
		setSetting("backup_last_status", status)
		setSetting("backup_last_msg", msg)
	}()

	cl, err := cfg.client()
	if err != nil {
		return "", err
	}
	if _, err = cl.ensureBucket(); err != nil {
		return "", err
	}
	data, err := snapshotDB()
	if err != nil {
		return "", err
	}
	key = cfg.keyPrefix() + time.Now().Format("20060102-150405") + ".db.gz"
	if err = cl.put(key, data); err != nil {
		return "", fmt.Errorf("上传备份失败: %v", err)
	}
	// 读取列表，删掉最旧的，循环滚动
	objs, err := cl.list(cfg.keyPrefix())
	if err != nil {
		return key, fmt.Errorf("备份已上传，但读取备份列表失败（未清理旧备份）: %v", err)
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key }) // 文件名带时间，按名称即按时间
	var delErrs []string
	for i := 0; i < len(objs)-cfg.Keep; i++ {
		if objs[i].Key == key {
			continue
		}
		if e := cl.delete(objs[i].Key); e != nil {
			delErrs = append(delErrs, objs[i].Key+": "+e.Error())
		} else {
			log.Printf("已删除旧备份: %s", objs[i].Key)
		}
	}
	if len(delErrs) > 0 {
		return key, fmt.Errorf("备份已上传，但删除旧备份失败：%s", strings.Join(delErrs, "；"))
	}
	return key, nil
}

// backupScheduler 每 30 秒检查一次是否到了自动备份时间
func backupScheduler() {
	for {
		time.Sleep(30 * time.Second)
		func() {
			defer func() {
				if e := recover(); e != nil {
					log.Printf("自动备份检查异常: %v", e)
				}
			}()
			cfg := loadBackupConfig()
			if !cfg.Enabled {
				return
			}
			anchor := backupAnchor()
			if anchor.IsZero() {
				setSetting("backup_anchor", strconv.FormatInt(nowMs(), 10))
				return
			}
			if time.Now().Before(cfg.nextRun(anchor)) {
				return
			}
			// 先记录本次尝试时间，失败也不会每 30 秒重试一次，等下一个周期
			setSetting("backup_anchor", strconv.FormatInt(nowMs(), 10))
			_, _ = runBackup(cfg)
		}()
	}
}
