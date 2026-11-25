package ai

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

const (
	ProviderOpenAI    = "openai_compatible"
	ProviderAnthropic = "anthropic"

	APIModeCompletions = "completions"
	APIModeResponses   = "responses"

	ProxyInherit  = "inherit"
	ProxyDisabled = "disabled"

	SummaryStyleStandard = "standard"
	SummaryStyleConcise  = "concise"

	TranslateDisplayBilingual  = "bilingual"
	TranslateDisplayTranslated = "translated"

	maxEndpointLen = 1024
	maxAPIKeyLen   = 4096
	maxModelLen    = 256
)

// Config is the encrypted-at-rest configuration for GoMail's unified AI & Translation engine.
type Config struct {
	Enabled              bool   `json:"enabled"`
	Provider             string `json:"provider"`
	Preset               string `json:"preset,omitempty"`
	Endpoint             string `json:"endpoint"`
	APIKey               string `json:"api_key,omitempty"`
	Model                string `json:"model"`
	APIMode              string `json:"api_mode"`
	ProxyMode            string `json:"proxy_mode"`
	AllowInsecureHTTP    bool   `json:"allow_insecure_http,omitempty"`
	SummaryLanguage      string `json:"summary_language"`
	SummaryStyle         string `json:"summary_style"`
	SummaryModel         string `json:"summary_model,omitempty"`
	TranslateLanguage    string `json:"translate_language"`
	TranslateDisplayMode string `json:"translate_display_mode"`
	TranslateModel       string `json:"translate_model,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Provider:             ProviderOpenAI,
		Preset:               "openai",
		APIMode:              APIModeCompletions,
		ProxyMode:            ProxyInherit,
		SummaryStyle:         SummaryStyleStandard,
		TranslateDisplayMode: TranslateDisplayBilingual,
	}
}

func (c *Config) Normalize() {
	c.Provider = strings.TrimSpace(c.Provider)
	if c.Provider == "" {
		c.Provider = ProviderOpenAI
	}
	c.Preset = strings.TrimSpace(c.Preset)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Model = strings.TrimSpace(c.Model)
	c.APIMode = strings.TrimSpace(c.APIMode)
	if c.APIMode == "" {
		c.APIMode = APIModeCompletions
	}
	c.ProxyMode = strings.TrimSpace(c.ProxyMode)
	if c.ProxyMode == "" {
		c.ProxyMode = ProxyInherit
	}
	c.SummaryLanguage = strings.TrimSpace(c.SummaryLanguage)
	c.SummaryStyle = strings.TrimSpace(c.SummaryStyle)
	if c.SummaryStyle == "" {
		c.SummaryStyle = SummaryStyleStandard
	}
	c.SummaryModel = strings.TrimSpace(c.SummaryModel)
	c.TranslateLanguage = strings.TrimSpace(c.TranslateLanguage)
	c.TranslateDisplayMode = strings.TrimSpace(c.TranslateDisplayMode)
	if c.TranslateDisplayMode == "" {
		c.TranslateDisplayMode = TranslateDisplayBilingual
	}
	c.TranslateModel = strings.TrimSpace(c.TranslateModel)
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func IsLoopbackHost(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "" {
		return false
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func ParseEndpoint(raw string, allowInsecureHTTP bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("API endpoint URL is required")
	}
	if len(trimmed) > maxEndpointLen || hasControlChars(trimmed) {
		return nil, errors.New("API endpoint URL is invalid")
	}
	u, err := url.Parse(trimmed)
	if err != nil || u == nil || u.Host == "" {
		return nil, errors.New("API endpoint URL must include a valid host")
	}
	if u.User != nil {
		return nil, errors.New("API endpoint URL must not include embedded credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("API endpoint URL must not include query parameters or fragments")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return nil, errors.New("API endpoint URL must use https:// or http://")
	}
	if scheme == "http" && !IsLoopbackHost(u.Hostname()) && !allowInsecureHTTP {
		return nil, errors.New("Remote API endpoints must use HTTPS (enable insecure HTTP only for trusted LAN servers)")
	}
	u.Scheme = scheme
	return u, nil
}

func (c Config) Validate(requireReady bool) error {
	c.Normalize()
	if c.Provider != ProviderOpenAI && c.Provider != ProviderAnthropic {
		return errors.New("Unsupported AI provider")
	}
	if c.APIMode != APIModeCompletions && c.APIMode != APIModeResponses {
		return errors.New("Unsupported OpenAI API mode")
	}
	if c.ProxyMode != ProxyInherit && c.ProxyMode != ProxyDisabled {
		return errors.New("Unsupported proxy mode")
	}
	if c.SummaryStyle != SummaryStyleStandard && c.SummaryStyle != SummaryStyleConcise {
		return errors.New("Unsupported summary style")
	}
	if c.TranslateDisplayMode != TranslateDisplayBilingual && c.TranslateDisplayMode != TranslateDisplayTranslated {
		return errors.New("Unsupported translation display mode")
	}
	if len(c.APIKey) > maxAPIKeyLen || hasControlChars(c.APIKey) {
		return errors.New("API key is invalid")
	}
	for _, m := range []string{c.Model, c.SummaryModel, c.TranslateModel} {
		if len(m) > maxModelLen || hasControlChars(m) {
			return errors.New("Model name is invalid")
		}
	}
	if c.Endpoint != "" || requireReady || c.Enabled {
		if _, err := ParseEndpoint(c.Endpoint, c.AllowInsecureHTTP); err != nil {
			return err
		}
	}
	if (requireReady || c.Enabled) && c.Model == "" {
		return errors.New("Default model name is required")
	}
	return nil
}

func (c Config) EndpointHost() string {
	u, err := url.Parse(strings.TrimSpace(c.Endpoint))
	if err != nil || u == nil {
		return ""
	}
	return u.Host
}

func (c Config) IsLocalEndpoint() bool {
	u, err := url.Parse(strings.TrimSpace(c.Endpoint))
	if err != nil || u == nil {
		return false
	}
	return IsLoopbackHost(u.Hostname())
}

func (c Config) EffectiveSummaryModel() string {
	if m := strings.TrimSpace(c.SummaryModel); m != "" {
		return m
	}
	return strings.TrimSpace(c.Model)
}

func (c Config) EffectiveTranslateModel() string {
	if m := strings.TrimSpace(c.TranslateModel); m != "" {
		return m
	}
	return strings.TrimSpace(c.Model)
}

func MaskAPIKey(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••"
	}
	return "••••" + k[len(k)-4:]
}
