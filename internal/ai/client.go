package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/mailnet"
)

const (
	maxResponseBytes = 2 << 20 // 2 MiB safety ceiling
	dialTimeout      = 20 * time.Second
	headerTimeout    = 60 * time.Second
)

type Message struct {
	Role    string
	Content string
}

type CompletionRequest struct {
	Model       string
	System      string
	Messages    []Message
	Temperature *float64
	MaxTokens   int
	JSONMode    bool
}

type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (string, error)
	Stream(ctx context.Context, req CompletionRequest, onDelta func(string)) (string, error)
	ListModels(ctx context.Context) ([]string, error)
	Test(ctx context.Context) error
}

func NewProvider(cfg Config, globalProxy *mailnet.Proxy) (Provider, error) {
	cfg.Normalize()
	if _, err := ParseEndpoint(cfg.Endpoint, cfg.AllowInsecureHTTP); err != nil {
		return nil, err
	}
	var route *mailnet.Proxy
	if cfg.ProxyMode != ProxyDisabled && !cfg.IsLocalEndpoint() && globalProxy != nil {
		copy := *globalProxy
		route = &copy
	}
	transport := (mailnet.Dialer{Proxy: route, Timeout: dialTimeout}).HTTPTransport()
	transport.ResponseHeaderTimeout = headerTimeout
	client := &http.Client{
		Transport: transport,
		// Per-request context controls timeout so long streaming responses are not truncated.
		Timeout: 0,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	switch cfg.Provider {
	case ProviderOpenAI:
		return &openAIProvider{cfg: cfg, client: client}, nil
	case ProviderAnthropic:
		return &anthropicProvider{cfg: cfg, client: client}, nil
	default:
		return nil, errors.New("Unsupported AI provider")
	}
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("AI provider response exceeded size limit")
	}
	return data, nil
}

func classifyHTTPError(status int, body []byte, secret string) error {
	detail := extractProviderErrorDetail(body)
	if secret != "" && len(secret) >= 6 {
		detail = strings.ReplaceAll(detail, secret, "[redacted]")
	}
	if len(detail) > 240 {
		detail = detail[:240] + "…"
	}
	switch {
	case status >= 300 && status < 400:
		return fmt.Errorf("AI endpoint redirected (HTTP %d); check the full base URL", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		if detail != "" {
			return fmt.Errorf("Authentication failed (HTTP %d): %s", status, detail)
		}
		return fmt.Errorf("Authentication failed (HTTP %d): check your API key", status)
	case status == http.StatusNotFound:
		if detail != "" {
			return fmt.Errorf("Endpoint or model not found (HTTP 404): %s", detail)
		}
		return errors.New("Endpoint or model not found (HTTP 404)")
	case status == http.StatusTooManyRequests:
		if detail != "" {
			return fmt.Errorf("Rate limited or quota exceeded (HTTP 429): %s", detail)
		}
		return errors.New("Rate limited or quota exceeded (HTTP 429)")
	case status >= 500:
		if detail != "" {
			return fmt.Errorf("AI provider server error (HTTP %d): %s", status, detail)
		}
		return fmt.Errorf("AI provider server error (HTTP %d)", status)
	default:
		if detail != "" {
			return fmt.Errorf("AI request failed (HTTP %d): %s", status, detail)
		}
		return fmt.Errorf("AI request failed (HTTP %d)", status)
	}
}

func extractProviderErrorDetail(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var envelope struct {
		Error   any    `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		switch e := envelope.Error.(type) {
		case string:
			if s := strings.TrimSpace(e); s != "" {
				return s
			}
		case map[string]any:
			if msg, ok := e["message"].(string); ok && strings.TrimSpace(msg) != "" {
				return strings.TrimSpace(msg)
			}
		}
		if s := strings.TrimSpace(envelope.Message); s != "" {
			return s
		}
		if s := strings.TrimSpace(envelope.Detail); s != "" {
			return s
		}
	}
	// Avoid dumping full HTML error pages.
	if strings.HasPrefix(strings.ToLower(trimmed), "<!doctype") || strings.HasPrefix(strings.ToLower(trimmed), "<html") {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}

func floatPtr(v float64) *float64 { return &v }
