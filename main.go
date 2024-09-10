package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shengyanli1982/gs"
	"github.com/shengyanli1982/law"
	"github.com/shengyanli1982/orbit"
	"github.com/shengyanli1982/orbit/utils/log"
	"github.com/shengyanli1982/toolkit/pkg/command"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	il "github.com/shengyanli1982/ldor/internal"
)

func main() {
	var (
		configPath  string
		asyncWriter *law.WriteAsyncer
		logger      *zap.SugaredLogger
		releaseMode bool
	)

	cmd := cobra.Command{}
	cmd.Flags().StringVarP(&configPath, "config", "c", "./config.json", "Configuration file path")
	cmd.Flags().BoolVarP(&releaseMode, "release", "r", false, "Set release mode")

	command.PrettyCobraHelpAndUsage(&cmd)
	err := cmd.Execute()
	if err != nil {
		fmt.Printf("Failed to execute command: %v\n", err)
		os.Exit(-1)
	}

	cfg := il.NewServiceConfig()
	err = cfg.LoadConfig(configPath)
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(-1)
	}

	host, p, err := net.SplitHostPort(cfg.Bind)
	if err != nil {
		fmt.Printf("Failed to parse bind address: %v\n", err)
		os.Exit(-1)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		fmt.Printf("Failed to conv port: %v\n", err)
		os.Exit(-1)
	}

	orbitConfig := orbit.NewConfig()
	orbitOptions := orbit.NewOptions().EnableMetric()

	if releaseMode || gin.Mode() == gin.ReleaseMode {
		orbitConfig.WithRelease()
		asyncWriter = law.NewWriteAsyncer(os.Stdout, law.DefaultConfig())
		logger = log.NewLogger(zapcore.AddSync(asyncWriter)).GetZapSugaredLogger().Named("default")
	} else {
		fmt.Printf("Loading config: [%s], Value:\n==========\n%s==========\n", configPath, cfg.String())
		logger = log.NewLogger(zapcore.AddSync(os.Stdout)).GetZapSugaredLogger().Named("default")
	}

	proxyService, err := il.NewProxyService(cfg, logger)
	if err != nil {
		logger.Errorf("Failed to create proxy service: %v", err)
		if releaseMode || gin.Mode() == gin.ReleaseMode {
			asyncWriter.Stop()
		}
		os.Exit(-1)
	}

	timeout := uint32(time.Duration(cfg.Timeout) * time.Second)
	orbitConfig.WithSugaredLogger(logger).WithAddress(host).WithPort(uint16(port)).WithHttpReadTimeout(timeout).WithHttpReadTimeout(timeout).WithHttpWriteTimeout(timeout)

	orbitEngine := orbit.NewEngine(orbitConfig, orbitOptions)
	orbitEngine.RegisterService(proxyService)

	orbitEngine.Run()

	engineStopSignal := gs.NewTerminateSignal()
	engineStopSignal.RegisterCancelHandles(orbitEngine.Stop)

	writerStopSignal := gs.NewTerminateSignal()
	if releaseMode || gin.Mode() == gin.ReleaseMode {
		writerStopSignal.RegisterCancelHandles(asyncWriter.Stop)
	}

	gs.WaitForForceSync(engineStopSignal, writerStopSignal)
}
