package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	xlogger "github.com/qqlikegi/forwardz/agent/x/logger"
	"github.com/qqlikegi/forwardz/agent/x/service"
	"github.com/qqlikegi/forwardz/agent/x/socket"
	"github.com/judwhite/go-svc"
)

type stringList []string

func (l *stringList) String() string {
	return fmt.Sprintf("%s", *l)
}
func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

// agentVersion ForwardZ 节点端版本号（同时上报给面板），发布时通过 -ldflags "-X main.agentVersion=..." 注入
var agentVersion = "2.0.7"

var (
	cfgFile      string
	outputFormat string
	services     stringList
	nodes        stringList
	debug        bool
	trace        bool
	apiAddr      string
	metricsAddr  string
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)

	args := strings.Join(os.Args[1:], "  ")

	if strings.Contains(args, " -- ") {
		var (
			wg  sync.WaitGroup
			ret int
		)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		for wid, wargs := range strings.Split(" "+args+" ", " -- ") {
			wg.Add(1)
			go func(wid int, wargs string) {
				defer wg.Done()
				defer cancel()
				worker(wid, strings.Split(wargs, "  "), &ctx, &ret)
			}(wid, strings.TrimSpace(wargs))
		}

		wg.Wait()

		os.Exit(ret)
	}
}

func worker(id int, args []string, ctx *context.Context, ret *int) {
	cmd := exec.CommandContext(*ctx, os.Args[0], args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), fmt.Sprintf("_FORWARDZ_ID=%d", id))

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	if cmd.ProcessState.Exited() {
		*ret = cmd.ProcessState.ExitCode()
	}
}

func init() {
	var printVersion bool

	flag.Var(&services, "L", "service list")
	flag.Var(&nodes, "F", "chain node list")
	flag.StringVar(&cfgFile, "C", "", "configuration file")
	flag.BoolVar(&printVersion, "V", false, "print version")
	flag.StringVar(&outputFormat, "O", "", "output format, one of yaml|json format")
	flag.BoolVar(&debug, "D", false, "debug mode")
	flag.BoolVar(&trace, "DD", false, "trace mode")
	flag.StringVar(&apiAddr, "api", "", "api service address")
	flag.StringVar(&metricsAddr, "metrics", "", "metrics service address")
	flag.Parse()

	if printVersion {
		fmt.Fprintf(os.Stdout, "forwardz %s (based on gost %s; %s %s/%s)\n",
			agentVersion, gostBaseVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}
}

func main() {
	// 加载配置文件
	config, err := LoadConfig("config.json")
	if err != nil {
		fmt.Printf("❌ 配置加载失败: %v\n", err)
		fmt.Println("请确保当前目录存在 config.json 文件")
		os.Exit(1)
	}

	fmt.Printf("✅ 配置加载成功 - addr: %s\n", config.Addr)

	// 面板下发的配置始终保存在工作目录的 forwardz.json（见 socket.saveConfig），这里固定读取它，
	// 不去系统目录里找配置，避免和机器上的其他转发程序互相干扰。
	// 旧版本保存的是 gost.json：首次启动时自动改名为 forwardz.json。
	// 工作目录没有配置时（手动运行、文件被误删）自动创建空配置，避免启动直接退出。
	if cfgFile == "" {
		if _, err := os.Stat("forwardz.json"); os.IsNotExist(err) {
			if _, err := os.Stat("gost.json"); err == nil {
				if err := os.Rename("gost.json", "forwardz.json"); err == nil {
					fmt.Println("📦 已将旧配置 gost.json 迁移为 forwardz.json")
				}
			}
		}
		if _, err := os.Stat("forwardz.json"); os.IsNotExist(err) {
			_ = os.WriteFile("forwardz.json", []byte("{}\n"), 0644)
		}
		if _, err := os.Stat("forwardz.json"); err == nil {
			cfgFile = "forwardz.json"
		}
	}

	log := xlogger.NewLogger()
	logger.SetDefault(log)

	wsReporter := socket.StartWebSocketReporterWithConfig(config.Addr, config.Secret, config.Http, config.Tls, config.Socks, agentVersion)
	defer wsReporter.Stop()
	service.SetHTTPReportURL(config.Addr, config.Secret)

	p := &program{}
	if err := svc.Run(p); err != nil {
		logger.Default().Fatal(err)
	}
}

// GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o forwardz_agent
// upx --best --lzma forwardz_agent
