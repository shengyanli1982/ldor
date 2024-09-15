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
	DefaultInstructionModel  = "gpt-3.5-turbo-instruct"
	DefaultAPIBaseURL        = "https://api.openai.com/v1"
	DefaultMaxTokenCount     = 2048
	DefaultLocale            = "zh_CN"
	DefaultRequestsPerSecond = math.MaxInt16
)

type ServiceConfig struct {
	BindAddress          string            `json:"bind,omitempty"`
	ProxyURL             string            `json:"proxy_url,omitempty"`
	TimeoutSeconds       int               `json:"timeout,omitempty"`
	CodexAPIBaseURL      string            `json:"codex_api_base,omitempty"`
	CodexAPIKey          string            `json:"codex_api_key,omitempty"`
	CodexAPIOrganization string            `json:"codex_api_organization,omitempty"`
	CodexAPIProject      string            `json:"codex_api_project,omitempty"`
	CodexMaxTokenCount   int               `json:"codex_max_tokens,omitempty"`
	CodeInstructionModel string            `json:"code_instruct_model,omitempty"`
	ChatAPIBaseURL       string            `json:"chat_api_base,omitempty"`
	ChatAPIKey           string            `json:"chat_api_key,omitempty"`
	ChatAPIOrganization  string            `json:"chat_api_organization,omitempty"`
	ChatAPIProject       string            `json:"chat_api_project,omitempty"`
	ChatMaxTokenCount    int               `json:"chat_max_tokens,omitempty"`
	ChatDefaultModel     string            `json:"chat_model_default,omitempty"`
	ChatModelMapping     map[string]string `json:"chat_model_map,omitempty"`
	ChatLocale           string            `json:"chat_locale,omitempty"`
	AuthToken            string            `json:"auth_token,omitempty"`
	MaxRequestsPerSecond int               `json:"requests_per_sec,omitempty"`
}

func NewServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		ChatModelMapping: make(map[string]string),
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
	if sc.BindAddress == "" {
		sc.BindAddress = "127.0.0.1:8181"
	}
	if sc.TimeoutSeconds == 0 {
		sc.TimeoutSeconds = 600
	}
	if sc.CodexAPIBaseURL == "" {
		sc.CodexAPIBaseURL = DefaultAPIBaseURL
	}
	if sc.CodexMaxTokenCount == 0 {
		sc.CodexMaxTokenCount = DefaultMaxTokenCount
	}
	if sc.CodeInstructionModel == "" {
		sc.CodeInstructionModel = DefaultInstructionModel
	}
	if sc.ChatAPIBaseURL == "" {
		sc.ChatAPIBaseURL = DefaultAPIBaseURL
	}
	if sc.ChatMaxTokenCount == 0 {
		sc.ChatMaxTokenCount = DefaultMaxTokenCount
	}
	if sc.ChatDefaultModel == "" {
		sc.ChatDefaultModel = DefaultInstructionModel
	}
	if sc.ChatLocale == "" {
		sc.ChatLocale = DefaultLocale
	}
	if sc.MaxRequestsPerSecond <= 0 {
		sc.MaxRequestsPerSecond = DefaultRequestsPerSecond
	}
}

func (c *ServiceConfig) String() string {
	b := bytes.NewBuffer(make([]byte, 0, 2048))

	b.WriteString("> BindAddress: " + c.BindAddress + "\n")
	b.WriteString("> ProxyURL: " + c.ProxyURL + "\n")
	b.WriteString("> TimeoutSeconds: " + strconv.Itoa(c.TimeoutSeconds) + "\n")
	b.WriteString("> MaxRequestsPerSecond: " + strconv.Itoa(c.MaxRequestsPerSecond) + "\n")
	b.WriteString("> CodexAPIBaseURL: " + c.CodexAPIBaseURL + "\n")
	b.WriteString("> CodexAPIOrganization: " + c.CodexAPIOrganization + "\n")
	b.WriteString("> CodexAPIProject: " + c.CodexAPIProject + "\n")
	b.WriteString("> CodexMaxTokenCount: " + strconv.Itoa(c.CodexMaxTokenCount) + "\n")
	b.WriteString("> CodeInstructionModel: " + c.CodeInstructionModel + "\n")
	b.WriteString("> ChatAPIBaseURL: " + c.ChatAPIBaseURL + "\n")
	b.WriteString("> ChatAPIOrganization: " + c.ChatAPIOrganization + "\n")
	b.WriteString("> ChatAPIProject: " + c.ChatAPIProject + "\n")
	b.WriteString("> ChatMaxTokenCount: " + strconv.Itoa(c.ChatMaxTokenCount) + "\n")
	b.WriteString("> ChatDefaultModel: " + c.ChatDefaultModel + "\n")
	b.WriteString("> ChatModelMapping: " + fmt.Sprintf("%v", c.ChatModelMapping) + "\n")

	return b.String()
}
