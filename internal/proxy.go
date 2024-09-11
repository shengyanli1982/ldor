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
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

const (
	StableCodeModelPrefix = "stable-code"
	DeepSeekCoderModel    = "deepseek-coder"
	DefaultLocale         = "zh_CN"
)

var ErrorConfigureTransport = errors.New("config transport failed")

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
}

func NewProxyService(cfg *ServiceConfig, log *zap.SugaredLogger, rl *rl.RateLimiter) (*ProxyService, error) {
	client, err := getClient(cfg)
	if nil != err {
		return nil, err
	}

	return &ProxyService{
		log:     log,
		limiter: rl,
		cfg:     cfg,
		client:  client,
	}, nil
}

func (ps *ProxyService) RegisterGroup(g *gin.RouterGroup) {
	// Common routes
	g.GET("/_ping", ps.pong)
	g.GET("/models", ps.models)
	g.GET("/v1/models", ps.models)

	// Chat and code completion routes
	chatRoute := "/chat/completions"
	codeRoute := "/engines/copilot-codex/completions"

	if ps.cfg.AuthToken != "" {
		// Authenticated routes
		v1 := g.Group("/:token/v1", AuthMiddleware(ps.cfg.AuthToken))
		v1.POST(chatRoute, ps.limiter.HandlerFunc(), ps.chatCompletions)
		v1.POST(codeRoute, ps.limiter.HandlerFunc(), ps.codeCompletions)
		v1.POST("/v1"+chatRoute, ps.limiter.HandlerFunc(), ps.chatCompletions)
		v1.POST("/v1"+codeRoute, ps.limiter.HandlerFunc(), ps.codeCompletions)
	} else {
		// Unauthenticated routes
		v1 := g.Group("/v1")
		v1.POST(chatRoute, ps.limiter.HandlerFunc(), ps.chatCompletions)
		v1.POST(codeRoute, ps.limiter.HandlerFunc(), ps.codeCompletions)
		v1.POST("/v1"+chatRoute, ps.limiter.HandlerFunc(), ps.chatCompletions)
		v1.POST("/v1"+codeRoute, ps.limiter.HandlerFunc(), ps.codeCompletions)
	}
}

func (ps *ProxyService) pong(c *gin.Context) {
	c.JSON(http.StatusOK, Pong{
		Now:    time.Now().Second(),
		Status: "ok",
		Ns1:    "200 OK",
	})
}

func (s *ProxyService) models(c *gin.Context) {
	c.JSON(http.StatusOK, defaultModels)
}

func (s *ProxyService) codeCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		abortWithError(c, http.StatusRequestTimeout, "Request timeout")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Errorf("Failed to read request body: %v", err)
		abortWithError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	body = s.constructCodeRequestBody(body)

	proxyURL := s.cfg.CodexApiBase + "/completions"
	req, err := createProxyRequest(ctx, http.MethodPost, proxyURL, body, s.cfg.CodexApiKey, s.cfg.CodexApiOrganization, s.cfg.CodexApiProject)
	if err != nil {
		s.log.Errorf("Failed to create request: %v", err)
		abortWithError(c, http.StatusInternalServerError, "Failed to create request")
		return
	}

	s.handleProxyRequest(c, req, "completions")
}

func (s *ProxyService) chatCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		abortWithError(c, http.StatusRequestTimeout, "Request timeout")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Errorf("Failed to read request body: %v", err)
		abortWithError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	body, err = s.constructChatRequestBody(body)
	if err != nil {
		s.log.Errorf("Failed to construct chat request body: %v", err)
		abortWithError(c, http.StatusInternalServerError, "Failed to construct chat request body")
		return
	}

	proxyURL := s.cfg.ChatApiBase + "/chat/completions"
	req, err := createProxyRequest(ctx, http.MethodPost, proxyURL, body, s.cfg.ChatApiKey, s.cfg.ChatApiOrganization, s.cfg.ChatApiProject)
	if err != nil {
		s.log.Errorf("Failed to create request: %v", err)
		abortWithError(c, http.StatusInternalServerError, "Failed to create request")
		return
	}

	s.handleProxyRequest(c, req, "chat completions")
}

func createProxyRequest(ctx context.Context, method, url string, body []byte, apiKey, organization, project string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
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
	resp, err := s.client.Do(req)
	if err != nil {
		s.handleProxyError(c, err, requestType)
		return
	}
	defer resp.Body.Close()

	s.handleProxyResponse(c, resp, requestType)
}

func (s *ProxyService) handleProxyError(c *gin.Context, err error, requestType string) {
	if errors.Is(err, context.Canceled) {
		abortWithError(c, http.StatusRequestTimeout, "Request timeout")
	} else {
		s.log.Errorf("Request %s failed: %v", requestType, err)
		abortWithError(c, http.StatusInternalServerError, "Internal server error")
	}
}

func (s *ProxyService) handleProxyResponse(c *gin.Context, resp *http.Response, requestType string) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.log.Errorf("Request %s failed with status code %d: %s", requestType, resp.StatusCode, string(body))
		abortWithError(c, resp.StatusCode, "Proxy request failed")
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

func abortWithError(c *gin.Context, status int, message string) {
	c.Header("Content-Type", "application/json")
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

func (s *ProxyService) constructChatRequestBody(body []byte) ([]byte, error) {
	var err error

	// Set model
	body, err = s.setModelIfMapped(body, "model", s.cfg.ChatModelMap, s.cfg.ChatModelDefault)
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
	body, err = s.setMaxTokensIfExceeded(body, "max_tokens", s.cfg.ChatMaxTokens)
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

func (s *ProxyService) constructCodeRequestBody(body []byte) []byte {
	var err error
	body, err = sjson.DeleteBytes(body, "extra")
	if err != nil {
		s.log.Errorf("Error deleting 'extra' field: %v", err)
	}

	body, err = sjson.DeleteBytes(body, "nwo")
	if err != nil {
		s.log.Errorf("Error deleting 'nwo' field: %v", err)
	}

	body, err = sjson.SetBytes(body, "model", s.cfg.CodeInstructModel)
	if err != nil {
		s.log.Errorf("Error setting model: %v", err)
	}

	maxTokens := gjson.GetBytes(body, "max_tokens").Int()
	if int(maxTokens) > s.cfg.CodexMaxTokens {
		body, err = sjson.SetBytes(body, "max_tokens", s.cfg.CodexMaxTokens)
		if err != nil {
			s.log.Errorf("Error setting max_tokens: %v", err)
		}
	}

	switch {
	case strings.Contains(s.cfg.CodeInstructModel, StableCodeModelPrefix):
		return s.constructWithStableCodeModel(body)
	case strings.HasPrefix(s.cfg.CodeInstructModel, DeepSeekCoderModel):
		if gjson.GetBytes(body, "n").Int() > 1 {
			body, err = sjson.SetBytes(body, "n", 1)
			if err != nil {
				s.log.Errorf("Error setting n: %v", err)
			}
		}
		// TODO: Implement other cases if needed
	}

	return body
}

func (s *ProxyService) constructWithStableCodeModel(body []byte) []byte {
	suffix := gjson.GetBytes(body, "suffix").String()
	prompt := gjson.GetBytes(body, "prompt").String()
	content := fmt.Sprintf("<fim_prefix>%s<fim_suffix>%s<fim_middle>", prompt, suffix)

	messages := []map[string]string{
		{
			"role":    "user",
			"content": content,
		},
	}
	return s.constructWithChatModel(body, messages)
}

func (s *ProxyService) constructWithChatModel(body []byte, messages interface{}) []byte {
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

func getClient(cfg *ServiceConfig) (*http.Client, error) {
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

	if cfg.ProxyUrl != "" {
		proxyURL, err := url.Parse(cfg.ProxyUrl)
		if err != nil {
			return nil, fmt.Errorf("failed to parse proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.Timeout) * time.Second,
	}

	return client, nil
}
