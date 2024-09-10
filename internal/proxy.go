package internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

const (
	StableCodeModelPrefix = "stable-code"
	DeepSeekCoderModel    = "deepseek-coder"
)

var (
	ErrorConfigureTransport = errors.New("config transport failed")
)

type Pong struct {
	Now    int    `json:"now"`
	Status string `json:"status"`
	Ns1    string `json:"ns1"`
}

type ProxyService struct {
	log    *zap.SugaredLogger
	cfg    *ServiceConfig
	client *http.Client
}

func NewProxyService(cfg *ServiceConfig, log *zap.SugaredLogger) (*ProxyService, error) {
	client, err := getClient(cfg)
	if nil != err {
		return nil, err
	}

	return &ProxyService{
		log:    log,
		cfg:    cfg,
		client: client,
	}, nil
}

func (ps *ProxyService) RegisterGroup(g *gin.RouterGroup) {
	g.GET("/_ping", ps.pong)
	g.GET("/models", ps.models)
	g.GET("/v1/models", ps.models)
	authToken := ps.cfg.AuthToken // replace with your dynamic value as needed
	if authToken != "" {
		// 鉴权
		v1 := g.Group("/:token/v1/", AuthMiddleware(authToken))
		{
			v1.POST("/chat/completions", ps.chatCompletions)
			v1.POST("/engines/copilot-codex/completions", ps.codeCompletions)
			v1.POST("/v1/chat/completions", ps.chatCompletions)
			v1.POST("/v1/engines/copilot-codex/completions", ps.codeCompletions)
		}
	} else {
		g.POST("/v1/chat/completions", ps.chatCompletions)
		g.POST("/v1/engines/copilot-codex/completions", ps.codeCompletions)
		g.POST("/v1/v1/chat/completions", ps.chatCompletions)
		g.POST("/v1/v1/engines/copilot-codex/completions", ps.codeCompletions)
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

func abortCodex(c *gin.Context, status int) {
	c.Header("Content-Type", "text/event-stream")

	c.String(status, "data: [DONE]\n")
	c.Abort()
}

func (s *ProxyService) codeCompletions(c *gin.Context) {
	ctx := c.Request.Context()
	if ctx.Err() != nil {
		abortCodex(c, http.StatusRequestTimeout)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		s.log.Errorf("Failed to read request body: %v", err)
		abortCodex(c, http.StatusBadRequest)
		return
	}

	body = ConstructRequestBody(body, s.cfg, s.log)

	proxyURL := s.cfg.CodexApiBase + "/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(body))
	if err != nil {
		s.log.Errorf("Failed to create request: %v", err)
		abortCodex(c, http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.CodexApiKey)
	if s.cfg.CodexApiOrganization != "" {
		req.Header.Set("OpenAI-Organization", s.cfg.CodexApiOrganization)
	}
	if s.cfg.CodexApiProject != "" {
		req.Header.Set("OpenAI-Project", s.cfg.CodexApiProject)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			abortCodex(c, http.StatusRequestTimeout)
		} else {
			s.log.Errorf("Request completions failed: %v", err)
			abortCodex(c, http.StatusInternalServerError)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		s.log.Errorf("Request completions failed with status code %d: %s", resp.StatusCode, string(body))
		abortCodex(c, resp.StatusCode)
		return
	}

	c.Status(resp.StatusCode)
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}

	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		s.log.Errorf("Failed to copy response body: %v", err)
	}
}

func (s *ProxyService) chatCompletions(c *gin.Context) {

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

func ConstructRequestBody(body []byte, cfg *ServiceConfig, log *zap.SugaredLogger) []byte {
	var err error
	body, err = sjson.DeleteBytes(body, "extra")
	if err != nil {
		// 处理错误，例如记录日志
		log.Errorf("Error deleting fields: %v", err)
	}

	body, err = sjson.DeleteBytes(body, "nwo")
	if err != nil {
		// 处理错误，例如记录日志
		log.Errorf("Error deleting fields: %v", err)
	}

	body, err = sjson.SetBytes(body, "model", cfg.CodeInstructModel)
	if err != nil {
		log.Errorf("Error setting model: %v", err)
	}

	maxTokens := gjson.GetBytes(body, "max_tokens").Int()
	if int(maxTokens) > cfg.CodexMaxTokens {
		body, err = sjson.SetBytes(body, "max_tokens", cfg.CodexMaxTokens)
		if err != nil {
			log.Errorf("Error setting max_tokens: %v", err)
		}
	}

	switch {
	case strings.Contains(cfg.CodeInstructModel, StableCodeModelPrefix):
		return constructWithStableCodeModel(body)
	case strings.HasPrefix(cfg.CodeInstructModel, DeepSeekCoderModel):
		if gjson.GetBytes(body, "n").Int() > 1 {
			body, err = sjson.SetBytes(body, "n", 1)
			if err != nil {
				log.Errorf("Error setting n: %v", err)
			}
		}
		// TODO: 需要实现
		// case strings.HasSuffix(cfg.ChatApiBase, "chat"):
		// 	return constructWithChatModel(body, nil) // 需要实现
	}

	return body
}

func constructWithStableCodeModel(body []byte) []byte {
	suffix := gjson.GetBytes(body, "suffix").String()
	prompt := gjson.GetBytes(body, "prompt").String()
	content := fmt.Sprintf("<fim_prefix>%s<fim_suffix>%s<fim_middle>", prompt, suffix)

	messages := []map[string]string{
		{
			"role":    "user",
			"content": content,
		},
	}
	return constructWithChatModel(body, messages)
}

func constructWithChatModel(body []byte, messages interface{}) []byte {
	var err error
	body, err = sjson.SetBytes(body, "messages", messages)
	if err != nil {
		log.Printf("Error setting messages: %v", err)
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
		return nil, ErrorConfigureTransport
	}

	if cfg.ProxyUrl != "" {
		proxyURL, err := url.Parse(cfg.ProxyUrl)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.Timeout) * time.Second,
	}

	return client, nil
}
