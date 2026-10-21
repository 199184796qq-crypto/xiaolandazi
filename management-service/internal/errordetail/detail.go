// Package errordetail carries a failure's origin without exposing credentials.
// It never changes retry, playback, billing or approval decisions.
package errordetail

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type Detail struct {
	Code       string `json:"code"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	Action     string `json:"action,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type Fault struct {
	Detail Detail
	Cause  error
}

func (e *Fault) Error() string {
	if e.Cause != nil {
		if e.Detail.Message == "" {
			return e.Cause.Error()
		}
		return e.Detail.Message + ": " + e.Cause.Error()
	}
	return e.Detail.Message
}
func (e *Fault) Unwrap() error { return e.Cause }

func New(code, stage, message, action string, cause error) error {
	return &Fault{Detail: Detail{Code: code, Stage: stage, Message: message, Action: action}, Cause: cause}
}

// WithStage records the operation that actually failed. A more specific typed
// origin (for example a disabled voice) takes precedence over an outer stage.
func WithStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	var fault *Fault
	if errors.As(err, &fault) && fault.Detail.Stage != "" {
		return err
	}
	return &Fault{Detail: Detail{Stage: stage}, Cause: err}
}

var (
	bearer    = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/=\-]+`)
	secret    = regexp.MustCompile(`(?i)((?:"|')?(?:api[_-]?key|access[_-]?key(?:[_-]?secret|[_-]?id)?|secret[_-]?key|password|passwd|token|authorization|cookie|signature|prompt|audio_data)(?:"|')?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}]+)`)
	urls      = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s"'<>）)]+`)
	dsn       = regexp.MustCompile(`[^\s:/]+:[^\s@]+@(?:tcp|unix)\([^)]*\)`)
	apiKey    = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|LTAI[A-Za-z0-9]{12,})`)
	payload   = regexp.MustCompile(`(?is)((?:"|')?(?:messages|request_body|request|input|audio_data)(?:"|')?\s*[:=]\s*)[\[{].*`)
	status    = regexp.MustCompile(`(?i)(?:http|status(?: code)?)[\s:/（(=]*([1-5][0-9]{2})`)
	requestID = regexp.MustCompile(`(?i)request[_ -]?id\s*[:=]\s*["']?([a-z0-9._:-]{1,128})`)
)

// Safe preserves the diagnostic words and codes, redacting values rather than
// replacing every unfamiliar error with "service unavailable".
func Safe(text string) string {
	text = bearer.ReplaceAllString(text, "Bearer [已脱敏]")
	text = urls.ReplaceAllString(text, "[链接已脱敏]")
	text = dsn.ReplaceAllString(text, "[数据库连接凭据已脱敏]")
	text = apiKey.ReplaceAllString(text, "[密钥已脱敏]")
	text = payload.ReplaceAllString(text, "$1[请求内容已脱敏]")
	text = secret.ReplaceAllString(text, "$1[已脱敏]")
	text = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 1200 {
		return string(runes[:1200]) + "…（详情见后台日志）"
	}
	return string(runes)
}

func Describe(err error, fallbackStage string) Detail {
	d := Detail{Code: "runtime_error", Stage: fallbackStage, Message: "后台未提供具体错误详情，请按场次编号核对运行日志。"}
	if d.Stage == "" {
		d.Stage = "运行状态"
	}
	if err == nil {
		return d
	}
	raw := err.Error()
	d.Message = Safe(raw)
	if match := status.FindStringSubmatch(raw); len(match) > 1 {
		d.HTTPStatus, _ = strconv.Atoi(match[1])
	}
	if match := requestID.FindStringSubmatch(raw); len(match) > 1 {
		d.RequestID = match[1]
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "tts") || strings.Contains(lower, "cosyvoice") || strings.Contains(raw, "声音合成"):
		d.Stage, d.Code = "声音合成", "tts_failed"
	case strings.Contains(raw, "声音") || strings.Contains(raw, "音色"):
		d.Stage, d.Code = "声音配置", "voice_configuration_error"
	case strings.Contains(lower, "core"):
		d.Stage, d.Code = "播放服务", "core_request_failed"
	case strings.Contains(lower, "oss") || strings.Contains(raw, "声音归档"):
		d.Stage, d.Code = "音频存储", "audio_storage_failed"
	case strings.Contains(lower, "provider") || strings.Contains(raw, "模型"):
		d.Stage, d.Code = "模型调用", "model_request_failed"
	case strings.Contains(lower, "sql:") || strings.Contains(lower, "mysql") || strings.Contains(lower, "redis"):
		d.Stage, d.Code = "数据读写", "data_access_failed"
	case strings.Contains(raw, "方案") || strings.Contains(raw, "事实") || strings.Contains(raw, "轮盘"):
		d.Stage, d.Code = "方案读取与校验", "plan_validation_failed"
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() || strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timed out") {
		d.Code, d.Action = "request_timeout", "检查该环节的服务响应和超时记录后重试。"
	} else if strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "connection reset") {
		d.Code, d.Action = "connection_failed", "检查该环节的服务连接和网络。"
	} else if strings.Contains(lower, "data_inspection_failed") || strings.Contains(lower, "datainspectionfailed") {
		d.Code, d.Action = "provider_content_rejected", "按上游错误码核查本次提交内容，不代表已经进入本系统的文字审核。"
	} else if d.HTTPStatus == 401 || d.HTTPStatus == 403 {
		d.Code, d.Action = "provider_access_denied", "检查该服务的账号、鉴权及访问权限。"
	} else if d.HTTPStatus == 429 {
		d.Code, d.Action = "provider_request_limited", "按上游具体原因核查请求频率或账号额度。"
	}
	var fault *Fault
	if errors.As(err, &fault) {
		if fault.Detail.Code != "" {
			d.Code = fault.Detail.Code
		}
		if fault.Detail.Stage != "" {
			d.Stage = fault.Detail.Stage
		}
		if fault.Detail.Action != "" {
			d.Action = Safe(fault.Detail.Action)
		}
		if fault.Detail.HTTPStatus != 0 {
			d.HTTPStatus = fault.Detail.HTTPStatus
		}
		if fault.Detail.RequestID != "" {
			d.RequestID = Safe(fault.Detail.RequestID)
		}
		// Keep the outer operation and the nested original failure together.
		// The raw Cause remains available to errors.Is/As and internal logging.
	}
	return d
}

func (d Detail) Text() string {
	text := fmt.Sprintf("发生环节：%s\n具体原因：%s", d.Stage, d.Message)
	if d.Action != "" {
		text += "\n处理建议：" + d.Action
	}
	text += "\n错误码：" + d.Code
	if d.HTTPStatus > 0 {
		text += fmt.Sprintf("；上游HTTP：%d", d.HTTPStatus)
	}
	if d.RequestID != "" {
		text += "\n上游请求编号：" + d.RequestID
	}
	return text
}
