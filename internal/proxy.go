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
	"golang.org/x/net/http2"
)

const StableCodeModelPrefix = "stable-code"
const DeepSeekCoderModel = "deepseek-coder"

type Pong struct {
	Now    int    `json:"now"`
	Status string `json:"status"`
	Ns1    string `json:"ns1"`
}

type ProxyService struct {
	cfg    *ServiceConfig
	client *http.Client
}

func NewProxyService(cfg *ServiceConfig) (*ProxyService, error) {
	client, err := getClient(cfg)
	if nil != err {
		return nil, err
	}

	return &ProxyService{
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

func (s *ProxyService) codeCompletions(c *gin.Context) {
	ctx := c.Request.Context()

	time.Sleep(200 * time.Millisecond)
	if ctx.Err() != nil {
		abortCodex(c, http.StatusRequestTimeout)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if nil != err {
		abortCodex(c, http.StatusBadRequest)
		return
	}

	body = ConstructRequestBody(body, s.cfg)

	proxyUrl := s.cfg.CodexApiBase + "/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyUrl, io.NopCloser(bytes.NewBuffer(body)))
	if nil != err {
		abortCodex(c, http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.CodexApiKey)
	if "" != s.cfg.CodexApiOrganization {
		req.Header.Set("OpenAI-Organization", s.cfg.CodexApiOrganization)
	}
	if "" != s.cfg.CodexApiProject {
		req.Header.Set("OpenAI-Project", s.cfg.CodexApiProject)
	}

	resp, err := s.client.Do(req)
	if nil != err {
		if errors.Is(err, context.Canceled) {
			abortCodex(c, http.StatusRequestTimeout)
			return
		}

		log.Println("request completions failed:", err.Error())
		abortCodex(c, http.StatusInternalServerError)
		return
	}
	defer closeIO(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Println("request completions failed:", string(body))

		abortCodex(c, resp.StatusCode)
		return
	}

	c.Status(resp.StatusCode)

	contentType := resp.Header.Get("Content-Type")
	if "" != contentType {
		c.Header("Content-Type", contentType)
	}

	_, _ = io.Copy(c.Writer, resp.Body)
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

func ConstructRequestBody(body []byte, cfg *ServiceConfig) []byte {
	body, _ = sjson.DeleteBytes(body, "extra")
	body, _ = sjson.DeleteBytes(body, "nwo")
	body, _ = sjson.SetBytes(body, "model", cfg.CodeInstructModel)

	if int(gjson.GetBytes(body, "max_tokens").Int()) > cfg.CodexMaxTokens {
		body, _ = sjson.SetBytes(body, "max_tokens", cfg.CodexMaxTokens)
	}

	if strings.Contains(cfg.CodeInstructModel, StableCodeModelPrefix) {
		return constructWithStableCodeModel(body)
	} else if strings.HasPrefix(cfg.CodeInstructModel, DeepSeekCoderModel) {
		if gjson.GetBytes(body, "n").Int() > 1 {
			body, _ = sjson.SetBytes(body, "n", 1)
		}
	}

	if strings.HasSuffix(cfg.ChatApiBase, "chat") {
		// @Todo  constructWithChatModel
		// 如果code base以chat结尾则构建chatModel，暂时没有好的prompt
	}

	return body
}

func constructWithStableCodeModel(body []byte) []byte {
	suffix := gjson.GetBytes(body, "suffix")
	prompt := gjson.GetBytes(body, "prompt")
	content := fmt.Sprintf("<fim_prefix>%s<fim_suffix>%s<fim_middle>", prompt, suffix)

	// 创建新的 JSON 对象并添加到 body 中
	messages := []map[string]string{
		{
			"role":    "user",
			"content": content,
		},
	}
	return constructWithChatModel(body, messages)
}

func constructWithChatModel(body []byte, messages interface{}) []byte {

	body, _ = sjson.SetBytes(body, "messages", messages)

	// fmt.Printf("Request Body: %s\n", body)
	// 2. 将转义的字符替换回原来的字符
	jsonStr := string(body)
	jsonStr = strings.ReplaceAll(jsonStr, "\\u003c", "<")
	jsonStr = strings.ReplaceAll(jsonStr, "\\u003e", ">")
	return []byte(jsonStr)
}

func abortCodex(c *gin.Context, status int) {
	c.Header("Content-Type", "text/event-stream")

	c.String(status, "data: [DONE]\n")
	c.Abort()
}

func closeIO(c io.Closer) {
	err := c.Close()
	if nil != err {
		log.Println(err)
	}
}

func getClient(cfg *ServiceConfig) (*http.Client, error) {
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		DisableKeepAlives: false,
	}

	err := http2.ConfigureTransport(transport)
	if nil != err {
		return nil, err
	}

	if "" != cfg.ProxyUrl {
		proxyUrl, err := url.Parse(cfg.ProxyUrl)
		if nil != err {
			return nil, err
		}

		transport.Proxy = http.ProxyURL(proxyUrl)
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.Timeout) * time.Second,
	}

	return client, nil
}
