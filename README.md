# LDOR (Linux Do Override)

LDOR 是一个代理服务，用于转发请求到目标服务器并返回响应。它主要用作 Copilot 的代理服务 (使用别的 LLM 模型)。

## 功能特点

-   支持配置文件自定义服务设置
-   支持日志记录和异步写入
-   内置速率限制功能
-   支持 HTTP 请求的超时设置
-   提供调试模式，可记录完整的请求和响应内容
-   支持优雅停机
-   支持代理请求自动重试

## 配置

服务配置通过 JSON 文件进行管理。主要配置项包括：

-   绑定地址和端口
-   代理 URL
-   超时设置
-   API 相关配置（基础 URL、密钥、组织、项目等）
-   模型设置
-   令牌数量限制
-   请求速率限制

详细配置示例请参考 `config.json` 文件。

### 配置文件

`json` 配置文件示例：

```json
{
    "bind": "127.0.0.1:8181",
    "proxy_url": "",
    "timeout": 600,
    "requests_per_sec": 32767,
    "codex_api_base": "https://api-proxy.oaipro.com/v1",
    "codex_api_key": "sk-xxx",
    "codex_api_organization": "",
    "codex_api_project": "",
    "codex_max_tokens": 500,
    "code_instruct_model": "gpt-3.5-turbo-instruct",
    "chat_api_base": "https://api-proxy.oaipro.com/v1",
    "chat_api_key": "sk-xxx",
    "chat_api_organization": "",
    "chat_api_project": "",
    "chat_max_tokens": 4096,
    "chat_model_default": "gpt-4o",
    "chat_model_map": {},
    "chat_locale": "zh_CN",
    "auth_token": ""
}
```

## 使用方法

### 命令行参数

```bash
$ ./ldor -h

Usage:
	ldor [flags]

Flags:
	-c, --config             Configuration file path
	-d, --debug              Set full debug mode, use for debugging, logging all request and response body content
	-h, --help               help for ldor
	-l, --logs               Output console log save file path (default: ""). All log files will be saved 500mb per file, 30 store days, and the maximum number of log files is 10.
	-p, --plain              Set plain text log mode, default is json log mode (only valid in release mode)
	-r, --release            Set release mode
```

### 启动服务

1. 准备配置文件 `config.json`
2. 运行命令：

```bash
./ldor -c /path/to/config.json
```

## 注意事项

-   在生产环境中使用时，建议启用 release 模式 (`-r` 标志)
-   可以通过 `-l` 参数指定日志文件保存路径
-   调试时可以使用 `-d` 标志启用完整的请求和响应日志记录

## 贡献

欢迎提交 issues 和 pull requests 来改进这个项目。
