package internal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
)

const (
	DefaultInstructModel  = "gpt-3.5-turbo-instruct"
	DefaultApiBase        = "https://api.openai.com/v1"
	DefaultMaxTokens      = 2048
	DefaultLanguage       = "zh_CN"
	DefaultRequestsPerSec = math.MaxInt16
)

type ServiceConfig struct {
	Bind                 string            `json:"bind,omitempty"`
	ProxyUrl             string            `json:"proxy_url,omitempty"`
	Timeout              int               `json:"timeout,omitempty"`
	CodexApiBase         string            `json:"codex_api_base,omitempty"`
	CodexApiKey          string            `json:"codex_api_key,omitempty"`
	CodexApiOrganization string            `json:"codex_api_organization,omitempty"`
	CodexApiProject      string            `json:"codex_api_project,omitempty"`
	CodexMaxTokens       int               `json:"codex_max_tokens,omitempty"`
	CodeInstructModel    string            `json:"code_instruct_model,omitempty"`
	ChatApiBase          string            `json:"chat_api_base,omitempty"`
	ChatApiKey           string            `json:"chat_api_key,omitempty"`
	ChatApiOrganization  string            `json:"chat_api_organization,omitempty"`
	ChatApiProject       string            `json:"chat_api_project,omitempty"`
	ChatMaxTokens        int               `json:"chat_max_tokens,omitempty"`
	ChatModelDefault     string            `json:"chat_model_default,omitempty"`
	ChatModelMap         map[string]string `json:"chat_model_map,omitempty"`
	ChatLocale           string            `json:"chat_locale,omitempty"`
	AuthToken            string            `json:"auth_token,omitempty"`
	TotalRequestsPerSec  int               `json:"requests_per_sec,omitempty"`
}

func NewServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		ChatModelMap: make(map[string]string),
	}
}

func (sc *ServiceConfig) LoadConfig(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(content, sc); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	sc.setDefaults()
	return nil
}

func (sc *ServiceConfig) setDefaults() {
	if sc.Bind == "" {
		sc.Bind = "127.0.0.1:8181"
	}
	if sc.Timeout == 0 {
		sc.Timeout = 600
	}
	if sc.CodexApiBase == "" {
		sc.CodexApiBase = DefaultApiBase
	}
	if sc.CodexMaxTokens == 0 {
		sc.CodexMaxTokens = DefaultMaxTokens
	}
	if sc.CodeInstructModel == "" {
		sc.CodeInstructModel = DefaultInstructModel
	}
	if sc.ChatApiBase == "" {
		sc.ChatApiBase = DefaultApiBase
	}
	if sc.ChatMaxTokens == 0 {
		sc.ChatMaxTokens = DefaultMaxTokens
	}
	if sc.ChatModelDefault == "" {
		sc.ChatModelDefault = DefaultInstructModel
	}
	if sc.ChatLocale == "" {
		sc.ChatLocale = DefaultLanguage
	}
	if sc.TotalRequestsPerSec <= 0 {
		sc.TotalRequestsPerSec = DefaultRequestsPerSec
	}
}

func (c *ServiceConfig) String() string {
	b := bytes.NewBuffer(make([]byte, 0, 2048))
	b.WriteString("> Bind: " + c.Bind + "\n")
	b.WriteString("> ProxyUrl: " + c.ProxyUrl + "\n")
	b.WriteString("> Timeout(Second): " + strconv.Itoa(c.Timeout) + "\n")
	b.WriteString("> TotalRequestsPerSec: " + strconv.Itoa(c.TotalRequestsPerSec) + "\n")
	b.WriteString("> CodexApiBase: " + c.CodexApiBase + "\n")
	b.WriteString("> CodexApiOrganization: " + c.CodexApiOrganization + "\n")
	b.WriteString("> CodexApiProject: " + c.CodexApiProject + "\n")
	b.WriteString("> CodexMaxTokens: " + strconv.Itoa(c.CodexMaxTokens) + "\n")
	b.WriteString("> CodeInstructModel: " + c.CodeInstructModel + "\n")
	b.WriteString("> ChatApiBase: " + c.ChatApiBase + "\n")
	b.WriteString("> ChatApiOrganization: " + c.ChatApiOrganization + "\n")
	b.WriteString("> ChatApiProject: " + c.ChatApiProject + "\n")
	b.WriteString("> ChatMaxTokens: " + strconv.Itoa(c.ChatMaxTokens) + "\n")
	b.WriteString("> ChatModelDefault: " + c.ChatModelDefault + "\n")
	b.WriteString("> ChatModelMap: " + fmt.Sprintf("%v", c.ChatModelMap) + "\n")

	return b.String()
}
