package main

import (
	"fmt"
	"net"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shengyanli1982/gs"
	"github.com/shengyanli1982/law"
	il "github.com/shengyanli1982/ldor/internal"
	"github.com/shengyanli1982/orbit"
	rl "github.com/shengyanli1982/orbit-contrib/pkg/ratelimiter"
	"github.com/shengyanli1982/orbit/utils/log"
	"github.com/shengyanli1982/toolkit/pkg/command"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	var (
		configPath                string
		asyncWriter               *law.WriteAsyncer
		logger                    *zap.SugaredLogger
		releaseMode, plainLogMode bool
	)

	cmd := cobra.Command{
		Use:   "ldor",
		Short: "ldor is copilot(linux do) override service",
		Long:  "ldor is a proxy service that forwards requests to a target server and returns the response.",
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "./config.json", "Configuration file path")
	cmd.Flags().BoolVarP(&releaseMode, "release", "r", false, "Set release mode")
	cmd.Flags().BoolVarP(&plainLogMode, "plain", "p", false, "Set plain text log mode, default is json log mode (only valid in release mode)")

	command.PrettyCobraHelpAndUsage(&cmd)
	if err := cmd.Execute(); err != nil {
		fmt.Printf("Failed to execute command: %v", err)
		os.Exit(-1)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v", err)
		os.Exit(-1)
	}

	host, port, err := parseBindAddress(cfg.Bind)
	if err != nil {
		fmt.Printf("Failed to parse bind address: %v", err)
		os.Exit(-1)
	}

	rlConf := rl.NewConfig().WithRate(float64(cfg.TotalRequestsPerSec)).WithBurst(1)
	limiter := rl.NewRateLimiter(rlConf)

	orbitConfig := orbit.NewConfig()
	orbitOptions := orbit.NewOptions().EnableMetric()

	if releaseMode || gin.Mode() == gin.ReleaseMode {
		orbitConfig.WithRelease()
		asyncWriter = law.NewWriteAsyncer(os.Stdout, law.DefaultConfig())
		if plainLogMode {
			logger = il.NewLogger(zapcore.AddSync(asyncWriter)).GetZapSugaredLogger().Named("default")
		} else {
			logger = log.NewLogger(zapcore.AddSync(asyncWriter)).GetZapSugaredLogger().Named("default")
		}
	} else {
		fmt.Printf("Loading config: [%s], Value:\n==========\n%s==========\n", configPath, cfg.String())
		logger = il.NewLogger(zapcore.AddSync(os.Stdout)).GetZapSugaredLogger().Named("default")
	}

	proxyService, err := il.NewProxyService(cfg, logger, limiter)
	if err != nil {
		logger.Errorf("Failed to create proxy service: %v", err)
		if releaseMode || gin.Mode() == gin.ReleaseMode {
			asyncWriter.Stop()
		}
		os.Exit(-1)
	}

	timeout := uint32(cfg.Timeout * 1000) // 秒转换为毫秒
	orbitConfig.WithSugaredLogger(logger).WithAddress(host).WithPort(uint16(port)).WithHttpReadTimeout(timeout).WithHttpWriteTimeout(timeout)

	orbitEngine := orbit.NewEngine(orbitConfig, orbitOptions)
	orbitEngine.RegisterService(proxyService)

	orbitEngine.Run()

	engineStopSignal := gs.NewTerminateSignal()
	engineStopSignal.RegisterCancelHandles(orbitEngine.Stop, limiter.Stop)

	writerStopSignal := gs.NewTerminateSignal()
	if releaseMode || gin.Mode() == gin.ReleaseMode {
		writerStopSignal.RegisterCancelHandles(asyncWriter.Stop)
	}

	gs.WaitForForceSync(engineStopSignal, writerStopSignal)
}

func loadConfig(configPath string) (*il.ServiceConfig, error) {
	cfg := il.NewServiceConfig()
	if err := cfg.LoadConfig(configPath); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseBindAddress(bind string) (string, int, error) {
	host, p, err := net.SplitHostPort(bind)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
