package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shengyanli1982/gs"
	"github.com/shengyanli1982/law"
	il "github.com/shengyanli1982/ldor/internal"
	"github.com/shengyanli1982/orbit"
	rl "github.com/shengyanli1982/orbit-contrib/pkg/ratelimiter"
	"github.com/shengyanli1982/orbit/utils/httptool"
	"github.com/shengyanli1982/orbit/utils/log"
	"github.com/shengyanli1982/toolkit/pkg/command"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	var (
		configFilePath                                 string
		asyncLogWriter                                 *law.WriteAsyncer
		logger                                         *zap.SugaredLogger
		isReleaseMode, isPlainLogMode, isFullDebugMode bool
	)

	rootCmd := cobra.Command{
		Use:   "ldor",
		Short: "ldor is copilot(linux do) override service",
		Long:  "ldor is a proxy service that forwards requests to a target server and returns the response.",
	}
	rootCmd.Flags().StringVarP(&configFilePath, "config", "c", "./config.json", "Configuration file path")
	rootCmd.Flags().BoolVarP(&isReleaseMode, "release", "r", false, "Set release mode")
	rootCmd.Flags().BoolVarP(&isPlainLogMode, "plain", "p", false, "Set plain text log mode, default is json log mode (only valid in release mode)")
	rootCmd.Flags().BoolVarP(&isFullDebugMode, "debug", "d", false, "Set full debug mode, use for debugging, logging all request and response body content")

	command.PrettyCobraHelpAndUsage(&rootCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Failed to execute command: %v", err)
		os.Exit(-1)
	}

	appConfig, err := loadServiceConfig(configFilePath)
	if err != nil {
		fmt.Printf("Failed to load config: %v", err)
		os.Exit(-1)
	}

	host, port, err := parseServerAddress(appConfig.BindAddress)
	if err != nil {
		fmt.Printf("Failed to parse bind address: %v", err)
		os.Exit(-1)
	}

	rateLimiterConfig := rl.NewConfig().WithRate(float64(appConfig.MaxRequestsPerSecond)).WithBurst(1)
	rateLimiter := rl.NewRateLimiter(rateLimiterConfig)

	orbitConfig := orbit.NewConfig().WithAccessLogEventFunc(func(logger *zap.SugaredLogger, event *log.LogEvent) {
		logger.Infow("http server access log", "id", event.ID, "endpoint", event.EndPoint, "method", event.Method, "code", event.Code, "status", event.Status, "latency", event.Latency, "user-agent", event.Agent, "error", event.Error, "stack", event.ErrorStack)
	})

	orbitOptions := orbit.NewOptions().EnableMetric()
	isReleaseMode = isReleaseMode || gin.Mode() == gin.ReleaseMode

	if isReleaseMode {
		orbitConfig.WithRelease()
		asyncLogWriter = law.NewWriteAsyncer(os.Stdout, law.DefaultConfig())
		if isPlainLogMode {
			logger = il.NewLogger(zapcore.AddSync(asyncLogWriter)).GetZapSugaredLogger().Named("default")
		} else {
			logger = log.NewLogger(zapcore.AddSync(asyncLogWriter)).GetZapSugaredLogger().Named("default")
		}
	} else {
		fmt.Printf("Loading config: [%s], Value:\n==========\n%s==========\n", configFilePath, appConfig.String())
		logger = il.NewLogger(zapcore.AddSync(os.Stdout)).GetZapSugaredLogger().Named("default")
	}

	proxyService, err := il.NewProxyService(appConfig, logger, rateLimiter)
	if err != nil {
		logger.Errorf("Failed to create proxy service: %v", err)
		if isReleaseMode {
			asyncLogWriter.Stop()
		}
		os.Exit(-1)
	}

	timeoutMs := uint32(appConfig.TimeoutSeconds * 1000) // Convert seconds to milliseconds
	orbitConfig.WithSugaredLogger(logger).WithAddress(host).WithPort(uint16(port)).WithHttpReadTimeout(timeoutMs).WithHttpWriteTimeout(timeoutMs)

	orbitEngine := orbit.NewEngine(orbitConfig, orbitOptions)

	if isFullDebugMode && !isReleaseMode {
		orbitEngine.RegisterMiddleware(logFullRequestAndResponseBody(logger))
	}

	orbitEngine.RegisterService(proxyService)
	orbitEngine.Run()

	engineStopSignal := gs.NewTerminateSignal()
	engineStopSignal.RegisterCancelHandles(orbitEngine.Stop, rateLimiter.Stop)

	writerStopSignal := gs.NewTerminateSignal()
	if isReleaseMode {
		writerStopSignal.RegisterCancelHandles(asyncLogWriter.Stop)
	}

	gs.WaitForForceSync(engineStopSignal, writerStopSignal)
}

func loadServiceConfig(configFilePath string) (*il.ServiceConfig, error) {
	appConfig := il.NewServiceConfig()
	if err := appConfig.LoadConfig(configFilePath); err != nil {
		return nil, err
	}
	return appConfig, nil
}

func parseServerAddress(address string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func logFullRequestAndResponseBody(logger *zap.SugaredLogger) func(*gin.Context) {
	return func(c *gin.Context) {
		requestBody, err := httptool.GenerateRequestBody(c)
		if err != nil {
			logger.Errorf("Failed to generate request body: %v", err)
			c.AbortWithStatus(http.StatusInternalServerError)
		}

		c.Next()

		logger.Infof("Request body ---> %s", requestBody)

		responseBody, err := httptool.GenerateResponseBody(c)
		if err != nil {
			logger.Errorf("Failed to generate response body: %v", err)
		} else {
			logger.Infof("Response body <--- %s", responseBody)
		}
	}
}
