package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	rl "github.com/shengyanli1982/orbit-contrib/pkg/ratelimiter"
	"github.com/shengyanli1982/retry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

const (
	StableCodeModel    = "stable-code"
	DeepSeekCoderModel = "deepseek-coder"
)

var ErrorConfigureTransport = errors.New("config transport failed")

type retryCallback struct {
	logger *zap.SugaredLogger
}

func (rc *retryCallback) OnRetry(count int64, delay time.Duration, err error) {
	rc.logger.Warnf("Retry attempt %d scheduled after %s, error: %v", count, delay, err)
}

type Pong struct {
	Now    int    `json:"now"`
	Status string `json:"status"`
	Ns1    string `json:"ns1"`
}

type ProxyService struct {
	limiter *rl.RateLimiter
	log     *zap.SugaredLogger
	cfg     *ServiceConfig
	client  *http.Client
	retrier *retry.Retry
}

func NewProxyService(config *ServiceConfig, logger *zap.SugaredLogger, limiter *rl.RateLimiter) (*ProxyService, error) {
	httpClient, err := createHTTPClient(config)
	if nil != err {
		return nil, err
	}

	retryCfg := retry.NewConfig().WithCallback(&retryCallback{logger: logger}).WithInitDelay(500 * time.Millisecond)

	return &ProxyService{
		log:     logger,
		limiter: limiter,
		cfg:     config,
		client:  httpClient,
		retrier: retry.New(retryCfg),
	}, nil
}

func (ps *ProxyService) RegisterGroup(g *gin.RouterGroup) {
	// Common routes
	g.GET("/_ping", ps.handlePing)
	g.GET("/models", ps.getAvailableModels)
	g.GET("/v1/models", ps.getAvailableModels)

	// Chat and code completion routes
	chatRoute := "/chat/completions"
	codeRoute := "/engines/copilot-codex/completions"

	if ps.cfg.AuthToken != "" {
		// Authenticated routes
		v1 := g.Group("/:token/v1", AuthMiddleware(ps.cfg.AuthToken))
		v1.POST(chatRoute, ps.limiter.HandlerFunc(), ps.handleChatCompletions)
		v1.POST(codeRoute, ps.limiter.HandlerFunc(), ps.handleCodeCompletions)
		v1.POST("/v1"+chatRoute, ps.limiter.HandlerFunc(), ps.handleChatCompletions)
		v1.POST("/v1"+codeRoute, ps.limiter.HandlerFunc(), ps.handleCodeCompletions)
	} else {
		// Unauthenticated routes
		v1 := g.Group("/v1")
		v1.POST(chatRoute, ps.limiter.HandlerFunc(), ps.handleChatCompletions)
		v1.POST(codeRoute, ps.limiter.HandlerFunc(), ps.handleCodeCompletions)
		v1.POST("/v1"+chatRoute, ps.limiter.HandlerFunc(), ps.handleChatCompletions)
		v1.POST("/v1"+codeRoute, ps.limiter.HandlerFunc(), ps.handleCodeCompletions)
	}
}

func (ps *ProxyService) handlePing(c *gin.Context) {
	c.JSON(http.StatusOK, Pong{
		Now:    time.Now().Second(),
		Status: "ok",
		Ns1:    "200 OK",
	})
}

func (s *ProxyService) getAvailableModels(c *gin.Context) {
	c.JSON(http.StatusOK, defaultModels)
}

func (s *ProxyService) handleCodeCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		respondWithError(c, http.StatusRequestTimeout, "Request timeout")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Errorf("Failed to read request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	body = s.prepareCodeRequestBody(body)

	proxyURL := s.cfg.CodexAPIBaseURL + "/completions"
	req, err := createProxyRequest(ctx, http.MethodPost, proxyURL, body, s.cfg.CodexAPIKey, s.cfg.CodexAPIOrganization, s.cfg.CodexAPIProject)
	if err != nil {
		s.log.Errorf("Failed to create request: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create request")
		return
	}

	s.handleProxyRequest(c, req, "completions")
}

func (s *ProxyService) handleChatCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		respondWithError(c, http.StatusRequestTimeout, "Request timeout")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Errorf("Failed to read request body: %v", err)
		respondWithError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	body, err = s.prepareChatRequestBody(body)
	if err != nil {
		s.log.Errorf("Failed to prepare chat request body: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to prepare chat request body")
		return
	}

	proxyURL := s.cfg.ChatAPIBaseURL + "/chat/completions"
	req, err := createProxyRequest(ctx, http.MethodPost, proxyURL, body, s.cfg.ChatAPIKey, s.cfg.ChatAPIOrganization, s.cfg.ChatAPIProject)
	if err != nil {
		s.log.Errorf("Failed to create request: %v", err)
		respondWithError(c, http.StatusInternalServerError, "Failed to create request")
		return
	}

	s.handleProxyRequest(c, req, "chat completions")
}

func createProxyRequest(ctx context.Context, method, targetURL string, body []byte, apiKey, organization, project string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if organization != "" {
		req.Header.Set("OpenAI-Organization", organization)
	}
	if project != "" {
		req.Header.Set("OpenAI-Project", project)
	}

	return req, nil
}

func (s *ProxyService) handleProxyRequest(c *gin.Context, req *http.Request, requestType string) {
	resp, err := s.executeHTTPRequestWithRetry(req)
	if err != nil {
		s.handleProxyError(c, err, requestType)
		return
	}
	defer resp.Body.Close()

	s.handleProxyResponse(c, resp, requestType)
}

func (s *ProxyService) handleProxyError(c *gin.Context, err error, requestType string) {
	if errors.Is(err, context.Canceled) {
		respondWithError(c, http.StatusRequestTimeout, "Request timeout")
	} else {
		s.log.Errorf("Request %s failed: %v", requestType, err)
		respondWithError(c, http.StatusInternalServerError, "Internal server error")
	}
}

func (s *ProxyService) handleProxyResponse(c *gin.Context, resp *http.Response, requestType string) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.log.Errorf("Request %s failed with status code %d: %s", requestType, resp.StatusCode, string(body))
		respondWithError(c, resp.StatusCode, "Proxy request failed")
		return
	}

	c.Status(resp.StatusCode)
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}

	_, err := io.Copy(c.Writer, resp.Body)
	if err != nil {
		s.log.Errorf("Failed to copy response body: %v", err)
	}
}

func respondWithError(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "application/json")
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

func (s *ProxyService) prepareChatRequestBody(body []byte) ([]byte, error) {
	var err error

	// Set model
	body, err = s.setModelIfMapped(body, "model", s.cfg.ChatModelMapping, s.cfg.ChatDefaultModel)
	if err != nil {
		return nil, err
	}

	// Set locale if necessary
	body, err = s.setLocaleIfNeeded(body)
	if err != nil {
		return nil, err
	}

	// Delete unnecessary fields
	fieldsToDelete := []string{"intent", "intent_threshold", "intent_content"}
	if body, err = s.deleteFields(body, fieldsToDelete); err != nil {
		return nil, err
	}

	// Set max_tokens if necessary
	body, err = s.setMaxTokensIfExceeded(body, "max_tokens", s.cfg.ChatMaxTokenCount)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func (s *ProxyService) setModelIfMapped(body []byte, key string, modelMap map[string]string, defaultModel string) ([]byte, error) {
	model := modelMap[gjson.GetBytes(body, key).String()]
	if model == "" {
		model = defaultModel
	}
	return s.setJSONField(body, key, model)
}

func (s *ProxyService) setLocaleIfNeeded(body []byte) ([]byte, error) {
	if gjson.GetBytes(body, "function_call").Exists() {
		return body, nil
	}

	messages := gjson.GetBytes(body, "messages").Array()
	lastMsg := messages[len(messages)-1].Get("content").String()
	if strings.Contains(lastMsg, "Respond in the following locale") {
		return body, nil
	}

	locale := s.cfg.ChatLocale
	if locale == "" {
		locale = DefaultLocale
	}
	newContent := lastMsg + "Respond in the following locale: " + locale + "."
	return s.setJSONField(body, fmt.Sprintf("messages.%d.content", len(messages)-1), newContent)
}

func (s *ProxyService) deleteFields(body []byte, fields []string) ([]byte, error) {
	var err error
	for _, field := range fields {
		body, err = sjson.DeleteBytes(body, field)
		if err != nil {
			return nil, s.logError("deleting "+field, err)
		}
	}
	return body, nil
}

func (s *ProxyService) setMaxTokensIfExceeded(body []byte, key string, maxAllowed int) ([]byte, error) {
	maxTokens := gjson.GetBytes(body, key).Int()
	if int(maxTokens) > maxAllowed {
		return s.setJSONField(body, key, maxAllowed)
	}
	return body, nil
}

func (s *ProxyService) setJSONField(body []byte, key string, value interface{}) ([]byte, error) {
	newBody, err := sjson.SetBytes(body, key, value)
	if err != nil {
		return nil, s.logError("setting "+key, err)
	}
	return newBody, nil
}

func (s *ProxyService) logError(action string, err error) error {
	s.log.Errorf("Error %s: %v", action, err)
	return fmt.Errorf("%s: %w", action, err)
}

func AuthMiddleware(authToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")
		if token != authToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *ProxyService) prepareCodeRequestBody(body []byte) []byte {
	var err error
	body, err = sjson.DeleteBytes(body, "extra")
	if err != nil {
		s.log.Errorf("Error deleting 'extra' field: %v", err)
	}

	body, err = sjson.DeleteBytes(body, "nwo")
	if err != nil {
		s.log.Errorf("Error deleting 'nwo' field: %v", err)
	}

	body, err = sjson.SetBytes(body, "model", s.cfg.CodeInstructionModel)
	if err != nil {
		s.log.Errorf("Error setting model: %v", err)
	}

	maxTokens := gjson.GetBytes(body, "max_tokens").Int()
	if int(maxTokens) > s.cfg.CodexMaxTokenCount {
		body, err = sjson.SetBytes(body, "max_tokens", s.cfg.CodexMaxTokenCount)
		if err != nil {
			s.log.Errorf("Error setting max_tokens: %v", err)
		}
	}

	switch {
	// stable-code model
	case strings.Contains(s.cfg.CodeInstructionModel, StableCodeModel):
		return s.prepareStableCodeModelRequest(body)
	// deepseek-coder model
	case strings.HasPrefix(s.cfg.CodeInstructionModel, DeepSeekCoderModel):
		return s.prepareDeepSeekCoderModelRequest(body)
		// TODO: Implement other cases if needed (e.g. openai model)
	}

	return body
}

func (s *ProxyService) prepareStableCodeModelRequest(body []byte) []byte {
	suffix := gjson.GetBytes(body, "suffix").String()
	prompt := gjson.GetBytes(body, "prompt").String()
	content := fmt.Sprintf("<fim_prefix>%s<fim_suffix>%s<fim_middle>", prompt, suffix)

	messages := []map[string]string{
		{
			"role":    "user",
			"content": content,
		},
	}
	return s.prepareChatModelRequest(body, messages)
}

func (s *ProxyService) prepareDeepSeekCoderModelRequest(body []byte) []byte {
	var err error
	if gjson.GetBytes(body, "n").Int() > 1 {
		body, err = sjson.SetBytes(body, "n", 1)
		if err != nil {
			s.log.Errorf("Error setting n: %v", err)
		}
	}
	return body
}

func (s *ProxyService) prepareChatModelRequest(body []byte, messages interface{}) []byte {
	var err error
	body, err = sjson.SetBytes(body, "messages", messages)
	if err != nil {
		s.log.Errorf("Error setting messages: %v", err)
	}

	jsonStr := string(body)
	jsonStr = strings.NewReplacer(
		"\\u003c", "<",
		"\\u003e", ">",
	).Replace(jsonStr)
	return []byte(jsonStr)
}

func createHTTPClient(cfg *ServiceConfig) (*http.Client, error) {
	transport := &http.Transport{
		ForceAttemptHTTP2:   true,
		DisableKeepAlives:   false,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if err := http2.ConfigureTransport(transport); err != nil {
		return nil, fmt.Errorf("failed to configure HTTP/2 transport: %w", err)
	}

	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
	}

	return client, nil
}

func (s *ProxyService) executeHTTPRequestWithRetry(req *http.Request) (*http.Response, error) {
	result := s.retrier.TryOnConflict(func() (interface{}, error) {
		return s.client.Do(req)
	})

	if !result.IsSuccess() {
		return nil, result.TryError()
	}

	return result.Data().(*http.Response), nil
}
