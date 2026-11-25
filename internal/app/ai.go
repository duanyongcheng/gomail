package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/duanyongcheng/gomail/internal/ai"
	"github.com/duanyongcheng/gomail/internal/domain"
	"github.com/duanyongcheng/gomail/internal/mailnet"
	"github.com/duanyongcheng/gomail/internal/store"
)

const (
	aiSettingsPurpose = "ai_settings"
	aiSettingsRecord  = "default"

	aiStreamTimeout   = 120 * time.Second
	aiCompleteTimeout = 90 * time.Second
	aiQuickTimeout    = 30 * time.Second

	maxBatchTranslateItems = 30
	maxSingleTranslateLen  = 20000
)

type AIService struct {
	Runtime *Runtime
}

type AIStreamEvent struct {
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Delta     string `json:"delta"`
	Done      bool   `json:"done"`
}

type AIConfigView struct {
	Configured           bool   `json:"configured"`
	Enabled              bool   `json:"enabled"`
	Provider             string `json:"provider"`
	Preset               string `json:"preset"`
	Endpoint             string `json:"endpoint"`
	EndpointHost         string `json:"endpointHost"`
	IsLocalEndpoint      bool   `json:"isLocalEndpoint"`
	HasAPIKey            bool   `json:"hasApiKey"`
	MaskedAPIKey         string `json:"maskedApiKey"`
	Model                string `json:"model"`
	APIMode              string `json:"apiMode"`
	ProxyMode            string `json:"proxyMode"`
	AllowInsecureHTTP    bool   `json:"allowInsecureHttp"`
	SummaryLanguage      string `json:"summaryLanguage"`
	SummaryStyle         string `json:"summaryStyle"`
	SummaryModel         string `json:"summaryModel"`
	TranslateLanguage    string `json:"translateLanguage"`
	TranslateDisplayMode string `json:"translateDisplayMode"`
	TranslateModel       string `json:"translateModel"`
	KeyringError         bool   `json:"keyringError"`
	SummaryCacheCount    int    `json:"summaryCacheCount"`
	TranslateCacheCount  int    `json:"translateCacheCount"`
}

type SaveAIConfigRequest struct {
	Enabled              bool   `json:"enabled"`
	Provider             string `json:"provider"`
	Preset               string `json:"preset"`
	Endpoint             string `json:"endpoint"`
	APIKey               string `json:"apiKey"`
	KeepExistingKey      bool   `json:"keepExistingKey"`
	Model                string `json:"model"`
	APIMode              string `json:"apiMode"`
	ProxyMode            string `json:"proxyMode"`
	AllowInsecureHTTP    bool   `json:"allowInsecureHttp"`
	SummaryLanguage      string `json:"summaryLanguage"`
	SummaryStyle         string `json:"summaryStyle"`
	SummaryModel         string `json:"summaryModel"`
	TranslateLanguage    string `json:"translateLanguage"`
	TranslateDisplayMode string `json:"translateDisplayMode"`
	TranslateModel       string `json:"translateModel"`
}

type TestAIConfigRequest struct {
	Provider          string `json:"provider"`
	Endpoint          string `json:"endpoint"`
	APIKey            string `json:"apiKey"`
	KeepExistingKey   bool   `json:"keepExistingKey"`
	Model             string `json:"model"`
	APIMode           string `json:"apiMode"`
	ProxyMode         string `json:"proxyMode"`
	AllowInsecureHTTP bool   `json:"allowInsecureHttp"`
}

type AITestResult struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Model     string `json:"model"`
	Message   string `json:"message"`
}

type SummarizeMessageRequest struct {
	RequestID    string `json:"requestId"`
	MessageID    string `json:"messageId"`
	TargetLang   string `json:"targetLang"`
	Style        string `json:"style"`
	ForceRefresh bool   `json:"forceRefresh"`
}

type SummarizeThreadRequest struct {
	RequestID    string `json:"requestId"`
	ThreadID     string `json:"threadId"`
	TargetLang   string `json:"targetLang"`
	Style        string `json:"style"`
	ForceRefresh bool   `json:"forceRefresh"`
}

type TranslateTextRequest struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
	FromLang  string `json:"fromLang"`
	ToLang    string `json:"toLang"`
	AutoFlip  bool   `json:"autoFlip"`
}

type TranslateTextResponse struct {
	Translated string `json:"translated"`
	TargetLang string `json:"targetLang"`
	Model      string `json:"model"`
}

type TranslateBatchRequest struct {
	RequestID string             `json:"requestId"`
	Items     []ai.TranslateItem `json:"items"`
	FromLang  string             `json:"fromLang"`
	ToLang    string             `json:"toLang"`
}

type CachedTranslationRequest struct {
	MessageID   string `json:"messageId"`
	TargetLang  string `json:"targetLang"`
	ContentHash string `json:"contentHash"`
}

type SaveTranslationCacheRequest struct {
	MessageID   string            `json:"messageId"`
	AccountID   string            `json:"accountId"`
	TargetLang  string            `json:"targetLang"`
	ContentHash string            `json:"contentHash"`
	Model       string            `json:"model"`
	Subject     string            `json:"subject"`
	Segments    map[string]string `json:"segments"`
}

type CachedTranslation struct {
	Found       bool              `json:"found"`
	Subject     string            `json:"subject"`
	Segments    map[string]string `json:"segments"`
	Model       string            `json:"model"`
	GeneratedAt int64             `json:"generatedAt"`
}

type CancelAIRequest struct {
	RequestID string `json:"requestId"`
}

type ClearAICacheResult struct {
	Deleted int64 `json:"deleted"`
}

type cachedTranslationPayload struct {
	Subject  string            `json:"subject"`
	Segments map[string]string `json:"segments"`
}

func (r *Runtime) decryptAIConfigLocked(raw []byte) error {
	if len(raw) == 0 {
		r.aiConfig = nil
		r.aiConfigLocked = false
		return nil
	}
	if r.crypto == nil {
		r.aiConfig = nil
		r.aiConfigLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	plain, err := r.crypto.Decrypt(aiSettingsPurpose, aiSettingsRecord, raw)
	if err != nil {
		r.aiConfig = nil
		r.aiConfigLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	var cfg ai.Config
	if json.Unmarshal(plain, &cfg) != nil {
		r.aiConfig = nil
		r.aiConfigLocked = true
		return &domain.AccessError{State: domain.ConnectionCredentialsUnavailable}
	}
	cfg.Normalize()
	r.aiConfig = &cfg
	r.aiConfigLocked = false
	return nil
}

func (r *Runtime) ensureAIConfigLoaded(ctx context.Context) error {
	if r == nil || r.DB == nil {
		return nil
	}
	raw, err := r.DB.AISettings(ctx)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		r.mu.Lock()
		r.aiConfig = nil
		r.aiConfigLocked = false
		r.mu.Unlock()
		return nil
	}
	r.mu.RLock()
	ready := r.aiConfig != nil && !r.aiConfigLocked
	r.mu.RUnlock()
	if ready {
		return nil
	}
	cipher, err := r.composeCrypto(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || cipher == nil {
		r.aiConfig = nil
		r.aiConfigLocked = true
		return nil
	}
	_ = r.decryptAIConfigLocked(raw)
	return nil
}

func (r *Runtime) aiConfigProjection(ctx context.Context) AIConfigView {
	def := ai.DefaultConfig()
	view := AIConfigView{
		Provider:             def.Provider,
		Preset:               def.Preset,
		APIMode:              def.APIMode,
		ProxyMode:            def.ProxyMode,
		SummaryStyle:         def.SummaryStyle,
		TranslateDisplayMode: def.TranslateDisplayMode,
	}
	if r != nil && r.DB != nil {
		if stats, err := r.DB.AICacheStats(ctx); err == nil {
			view.SummaryCacheCount = stats.SummaryCount
			view.TranslateCacheCount = stats.TranslationCount
		}
	}
	if r == nil {
		return view
	}
	r.mu.RLock()
	locked := r.aiConfigLocked
	var cfg *ai.Config
	if r.aiConfig != nil {
		copy := *r.aiConfig
		cfg = &copy
	}
	r.mu.RUnlock()
	if locked {
		view.KeyringError = true
		return view
	}
	if cfg == nil {
		return view
	}
	view.Configured = cfg.Endpoint != "" && cfg.Model != ""
	view.Enabled = cfg.Enabled
	view.Provider = cfg.Provider
	view.Preset = cfg.Preset
	view.Endpoint = cfg.Endpoint
	view.EndpointHost = cfg.EndpointHost()
	view.IsLocalEndpoint = cfg.IsLocalEndpoint()
	view.HasAPIKey = cfg.APIKey != ""
	view.MaskedAPIKey = ai.MaskAPIKey(cfg.APIKey)
	view.Model = cfg.Model
	view.APIMode = cfg.APIMode
	view.ProxyMode = cfg.ProxyMode
	view.AllowInsecureHTTP = cfg.AllowInsecureHTTP
	view.SummaryLanguage = cfg.SummaryLanguage
	view.SummaryStyle = cfg.SummaryStyle
	view.SummaryModel = cfg.SummaryModel
	view.TranslateLanguage = cfg.TranslateLanguage
	view.TranslateDisplayMode = cfg.TranslateDisplayMode
	view.TranslateModel = cfg.TranslateModel
	return view
}

func (r *Runtime) activeAIProvider(ctx context.Context) (ai.Provider, ai.Config, error) {
	if r == nil || r.DB == nil {
		return nil, ai.Config{}, errors.New("Profile database unavailable")
	}
	if err := r.ensureAIConfigLoaded(ctx); err != nil {
		return nil, ai.Config{}, errors.New("Cannot read AI configuration")
	}
	r.mu.RLock()
	locked := r.aiConfigLocked
	var cfg *ai.Config
	if r.aiConfig != nil {
		copy := *r.aiConfig
		cfg = &copy
	}
	var proxyCopy *mailnet.Proxy
	if r.globalProxy != nil {
		p := *r.globalProxy
		proxyCopy = &p
	}
	r.mu.RUnlock()

	if locked {
		return nil, ai.Config{}, errors.New("AI_KEYRING_LOCKED: Saved AI settings could not be decrypted; unlock your system keyring or re-save settings")
	}
	if cfg == nil || cfg.Endpoint == "" || cfg.Model == "" {
		return nil, ai.Config{}, errors.New("AI_NOT_CONFIGURED: AI service is not configured yet")
	}
	if !cfg.Enabled {
		return nil, ai.Config{}, errors.New("AI_DISABLED: AI & Translation is currently disabled in Settings")
	}
	provider, err := ai.NewProvider(*cfg, proxyCopy)
	if err != nil {
		return nil, ai.Config{}, err
	}
	return provider, *cfg, nil
}

func (r *Runtime) registerAICancel(ctx context.Context, requestID string, timeout time.Duration) (context.Context, func()) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	var stopRoot func() bool
	if r != nil && r.Context != nil {
		stopRoot = context.AfterFunc(r.Context, cancel)
	}
	id := strings.TrimSpace(requestID)
	if r != nil && id != "" {
		r.aiCancelMu.Lock()
		if r.aiCancels == nil {
			r.aiCancels = map[string]context.CancelFunc{}
		}
		if prev, ok := r.aiCancels[id]; ok {
			prev()
		}
		r.aiCancels[id] = cancel
		r.aiCancelMu.Unlock()
	}
	return reqCtx, func() {
		if stopRoot != nil {
			stopRoot()
		}
		if r != nil && id != "" {
			r.aiCancelMu.Lock()
			delete(r.aiCancels, id)
			r.aiCancelMu.Unlock()
		}
		cancel()
	}
}

func (s *AIService) GetConfig(ctx context.Context, _ Empty) (AIConfigView, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return r.aiConfigProjection(ctx), nil
	}
	if err := r.ensureAIConfigLoaded(ctx); err != nil {
		return AIConfigView{}, errors.New("Cannot read AI configuration")
	}
	return r.aiConfigProjection(ctx), nil
}

func (s *AIService) SaveConfig(ctx context.Context, req SaveAIConfigRequest) (AIConfigView, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return AIConfigView{}, errors.New("Profile database unavailable")
	}
	cipher, err := r.composeCrypto(ctx)
	if err != nil {
		return AIConfigView{}, err
	}

	apiKey := strings.TrimSpace(req.APIKey)
	if req.KeepExistingKey && apiKey == "" {
		if err := r.ensureAIConfigLoaded(ctx); err != nil {
			return AIConfigView{}, errors.New("Cannot read existing AI configuration")
		}
		r.mu.RLock()
		locked := r.aiConfigLocked
		if r.aiConfig != nil {
			apiKey = r.aiConfig.APIKey
		}
		r.mu.RUnlock()
		if locked {
			return AIConfigView{}, errors.New("Saved AI configuration could not be decrypted; enter a new API key or clear saved settings")
		}
	}

	cfg := ai.Config{
		Enabled:              req.Enabled,
		Provider:             req.Provider,
		Preset:               req.Preset,
		Endpoint:             req.Endpoint,
		APIKey:               apiKey,
		Model:                req.Model,
		APIMode:              req.APIMode,
		ProxyMode:            req.ProxyMode,
		AllowInsecureHTTP:    req.AllowInsecureHTTP,
		SummaryLanguage:      req.SummaryLanguage,
		SummaryStyle:         req.SummaryStyle,
		SummaryModel:         req.SummaryModel,
		TranslateLanguage:    req.TranslateLanguage,
		TranslateDisplayMode: req.TranslateDisplayMode,
		TranslateModel:       req.TranslateModel,
	}
	cfg.Normalize()
	if err := cfg.Validate(cfg.Enabled); err != nil {
		return AIConfigView{}, err
	}

	plain, err := json.Marshal(cfg)
	if err != nil {
		return AIConfigView{}, err
	}
	encrypted, err := cipher.Encrypt(aiSettingsPurpose, aiSettingsRecord, plain)
	if err != nil {
		return AIConfigView{}, err
	}
	if err := r.DB.SaveAISettings(ctx, encrypted, time.Now().Unix()); err != nil {
		return AIConfigView{}, err
	}

	r.mu.Lock()
	r.aiConfig = &cfg
	r.aiConfigLocked = false
	r.mu.Unlock()
	return r.aiConfigProjection(ctx), nil
}

func (s *AIService) ClearConfig(ctx context.Context, _ Empty) (AIConfigView, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return AIConfigView{}, errors.New("Profile database unavailable")
	}
	if err := r.DB.DeleteAISettings(ctx); err != nil {
		return AIConfigView{}, err
	}
	r.mu.Lock()
	r.aiConfig = nil
	r.aiConfigLocked = false
	r.mu.Unlock()
	return r.aiConfigProjection(ctx), nil
}

func (s *AIService) buildAdHocProvider(ctx context.Context, req TestAIConfigRequest, requireModel bool) (ai.Provider, ai.Config, error) {
	r := s.Runtime
	apiKey := strings.TrimSpace(req.APIKey)
	var proxyCopy *mailnet.Proxy
	if r != nil {
		if req.KeepExistingKey && apiKey == "" {
			if err := r.ensureAIConfigLoaded(ctx); err != nil {
				return nil, ai.Config{}, errors.New("Cannot read existing AI configuration")
			}
			r.mu.RLock()
			if r.aiConfig != nil {
				apiKey = r.aiConfig.APIKey
			}
			r.mu.RUnlock()
		}
		r.mu.RLock()
		if r.globalProxy != nil {
			p := *r.globalProxy
			proxyCopy = &p
		}
		r.mu.RUnlock()
	}
	cfg := ai.Config{
		Provider:          req.Provider,
		Endpoint:          req.Endpoint,
		APIKey:            apiKey,
		Model:             req.Model,
		APIMode:           req.APIMode,
		ProxyMode:         req.ProxyMode,
		AllowInsecureHTTP: req.AllowInsecureHTTP,
	}
	cfg.Normalize()
	if err := cfg.Validate(requireModel); err != nil {
		return nil, ai.Config{}, err
	}
	if cfg.Endpoint == "" {
		return nil, ai.Config{}, errors.New("API endpoint URL is required")
	}
	provider, err := ai.NewProvider(cfg, proxyCopy)
	return provider, cfg, err
}

func (s *AIService) TestConnection(ctx context.Context, req TestAIConfigRequest) (AITestResult, error) {
	provider, cfg, err := s.buildAdHocProvider(ctx, req, true)
	if err != nil {
		return AITestResult{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, aiQuickTimeout)
	defer cancel()
	start := time.Now()
	if err := provider.Test(reqCtx); err != nil {
		return AITestResult{}, err
	}
	latency := time.Since(start).Milliseconds()
	return AITestResult{
		OK:        true,
		LatencyMs: latency,
		Model:     cfg.Model,
		Message:   fmt.Sprintf("%s (%d ms)", cfg.Model, latency),
	}, nil
}

func (s *AIService) ListModels(ctx context.Context, req TestAIConfigRequest) ([]string, error) {
	provider, _, err := s.buildAdHocProvider(ctx, req, false)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, aiQuickTimeout)
	defer cancel()
	return provider.ListModels(reqCtx)
}

func (s *AIService) SummarizeMessage(ctx context.Context, req SummarizeMessageRequest) (ai.SummaryResult, error) {
	r := s.Runtime
	provider, cfg, err := r.activeAIProvider(ctx)
	if err != nil {
		return ai.SummaryResult{}, err
	}
	msgID := strings.TrimSpace(req.MessageID)
	if msgID == "" {
		return ai.SummaryResult{}, errors.New("Message ID is required")
	}
	msg, err := r.DB.Message(ctx, msgID)
	if err != nil {
		return ai.SummaryResult{}, errors.New("Message not found")
	}

	targetLang := strings.TrimSpace(req.TargetLang)
	if targetLang == "" {
		targetLang = cfg.SummaryLanguage
	}
	if targetLang == "" {
		targetLang = "zh"
	}
	style := strings.TrimSpace(req.Style)
	if style == "" {
		style = cfg.SummaryStyle
	}
	model := cfg.EffectiveSummaryModel()
	extracted := ai.ExtractMessageContent(msg.Subject, msg.FromName, msg.FromAddress, msg.BodyText, msg.BodyHTML, ai.DefaultMaxSummaryRunes)
	if strings.TrimSpace(extracted.Text) == "" {
		return ai.SummaryResult{}, errors.New("This message has no readable text to summarize")
	}
	cacheHash := ai.HashContent(extracted.ContentHash, style)

	if !req.ForceRefresh {
		if cached, err := r.DB.GetAICache(ctx, msg.ID, "summary", targetLang); err == nil && cached != nil && cached.ContentHash == cacheHash {
			var res ai.SummaryResult
			if json.Unmarshal([]byte(cached.Payload), &res) == nil && strings.TrimSpace(res.RawText) != "" {
				res.Cached = true
				return res, nil
			}
		}
	}

	system, user := ai.BuildSummaryPrompt(extracted.Text, targetLang, style, false)
	reqCtx, cleanup := r.registerAICancel(ctx, req.RequestID, aiStreamTimeout)
	defer cleanup()

	temp := 0.2
	raw, err := provider.Stream(reqCtx, ai.CompletionRequest{
		Model:       model,
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: user}},
		Temperature: &temp,
		MaxTokens:   1024,
	}, func(delta string) {
		if r.OnAIStream != nil && req.RequestID != "" {
			r.OnAIStream(AIStreamEvent{
				RequestID: req.RequestID,
				Kind:      "summary",
				Delta:     delta,
			})
		}
	})
	if r.OnAIStream != nil && req.RequestID != "" {
		r.OnAIStream(AIStreamEvent{RequestID: req.RequestID, Kind: "summary", Done: true})
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ai.SummaryResult{}, errors.New("AI_CANCELLED: Request cancelled")
		}
		return ai.SummaryResult{}, err
	}

	now := time.Now().Unix()
	result := ai.ParseSummaryOutput(raw, targetLang, model, extracted.Truncated, now)
	if encoded, err := json.Marshal(result); err == nil {
		_ = r.DB.SaveAICache(ctx, store.AICacheEntry{
			TargetID:    msg.ID,
			AccountID:   msg.AccountID,
			Kind:        "summary",
			TargetLang:  targetLang,
			ContentHash: cacheHash,
			Model:       model,
			Payload:     string(encoded),
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return result, nil
}

func (s *AIService) SummarizeThread(ctx context.Context, req SummarizeThreadRequest) (ai.SummaryResult, error) {
	r := s.Runtime
	provider, cfg, err := r.activeAIProvider(ctx)
	if err != nil {
		return ai.SummaryResult{}, err
	}
	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		return ai.SummaryResult{}, errors.New("Thread ID is required")
	}
	summaries, err := r.DB.ThreadMessages(ctx, threadID)
	if err != nil {
		return ai.SummaryResult{}, err
	}
	if len(summaries) == 0 {
		return ai.SummaryResult{}, errors.New("Thread has no messages to summarize")
	}
	accountID := summaries[0].AccountID
	startIdx := 0
	if len(summaries) > ai.MaxThreadSummaryMessages {
		startIdx = len(summaries) - ai.MaxThreadSummaryMessages
	}
	inputs := make([]ai.ThreadMessageInput, 0, len(summaries)-startIdx)
	for _, m := range summaries[startIdx:] {
		full, err := r.DB.Message(ctx, m.ID)
		if err != nil {
			continue
		}
		from := strings.TrimSpace(full.FromName)
		if from == "" {
			from = full.FromAddress
		}
		inputs = append(inputs, ai.ThreadMessageInput{
			ID:       full.ID,
			From:     from,
			Date:     full.Date,
			Subject:  full.Subject,
			BodyText: full.BodyText,
			BodyHTML: full.BodyHTML,
		})
	}
	if len(inputs) == 0 {
		return ai.SummaryResult{}, errors.New("Thread messages could not be loaded")
	}

	targetLang := strings.TrimSpace(req.TargetLang)
	if targetLang == "" {
		targetLang = cfg.SummaryLanguage
	}
	if targetLang == "" {
		targetLang = "zh"
	}
	style := strings.TrimSpace(req.Style)
	if style == "" {
		style = cfg.SummaryStyle
	}
	model := cfg.EffectiveSummaryModel()
	extracted := ai.ExtractThreadContent(inputs, ai.DefaultMaxThreadSummaryRunes)
	if startIdx > 0 {
		extracted.Truncated = true
	}
	cacheHash := ai.HashContent(extracted.ContentHash, style)

	if !req.ForceRefresh {
		if cached, err := r.DB.GetAICache(ctx, threadID, "thread_summary", targetLang); err == nil && cached != nil && cached.ContentHash == cacheHash {
			var res ai.SummaryResult
			if json.Unmarshal([]byte(cached.Payload), &res) == nil && strings.TrimSpace(res.RawText) != "" {
				res.Cached = true
				return res, nil
			}
		}
	}

	system, user := ai.BuildSummaryPrompt(extracted.Text, targetLang, style, true)
	reqCtx, cleanup := r.registerAICancel(ctx, req.RequestID, aiStreamTimeout)
	defer cleanup()

	temp := 0.2
	raw, err := provider.Stream(reqCtx, ai.CompletionRequest{
		Model:       model,
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: user}},
		Temperature: &temp,
		MaxTokens:   1400,
	}, func(delta string) {
		if r.OnAIStream != nil && req.RequestID != "" {
			r.OnAIStream(AIStreamEvent{
				RequestID: req.RequestID,
				Kind:      "thread_summary",
				Delta:     delta,
			})
		}
	})
	if r.OnAIStream != nil && req.RequestID != "" {
		r.OnAIStream(AIStreamEvent{RequestID: req.RequestID, Kind: "thread_summary", Done: true})
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ai.SummaryResult{}, errors.New("AI_CANCELLED: Request cancelled")
		}
		return ai.SummaryResult{}, err
	}

	now := time.Now().Unix()
	result := ai.ParseSummaryOutput(raw, targetLang, model, extracted.Truncated, now)
	if encoded, err := json.Marshal(result); err == nil {
		_ = r.DB.SaveAICache(ctx, store.AICacheEntry{
			TargetID:    threadID,
			AccountID:   accountID,
			Kind:        "thread_summary",
			TargetLang:  targetLang,
			ContentHash: cacheHash,
			Model:       model,
			Payload:     string(encoded),
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return result, nil
}

func (s *AIService) TranslateText(ctx context.Context, req TranslateTextRequest) (TranslateTextResponse, error) {
	r := s.Runtime
	provider, cfg, err := r.activeAIProvider(ctx)
	if err != nil {
		return TranslateTextResponse{}, err
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return TranslateTextResponse{}, errors.New("Text to translate is empty")
	}
	if len([]rune(text)) > maxSingleTranslateLen {
		return TranslateTextResponse{}, errors.New("Selected text is too long to translate at once")
	}
	requestedLang := strings.TrimSpace(req.ToLang)
	if requestedLang == "" {
		requestedLang = cfg.TranslateLanguage
	}
	targetLang := ai.ResolveSelectionTargetLang(text, requestedLang, req.AutoFlip)
	model := cfg.EffectiveTranslateModel()
	system, user := ai.BuildTranslateTextPrompt(text, req.FromLang, targetLang)

	reqCtx, cleanup := r.registerAICancel(ctx, req.RequestID, aiCompleteTimeout)
	defer cleanup()

	temp := 0.1
	translated, err := provider.Complete(reqCtx, ai.CompletionRequest{
		Model:       model,
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: user}},
		Temperature: &temp,
		MaxTokens:   2048,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return TranslateTextResponse{}, errors.New("AI_CANCELLED: Request cancelled")
		}
		return TranslateTextResponse{}, err
	}
	return TranslateTextResponse{
		Translated: strings.TrimSpace(translated),
		TargetLang: targetLang,
		Model:      model,
	}, nil
}

func (s *AIService) TranslateBatch(ctx context.Context, req TranslateBatchRequest) (ai.TranslateBatchResult, error) {
	r := s.Runtime
	provider, cfg, err := r.activeAIProvider(ctx)
	if err != nil {
		return ai.TranslateBatchResult{}, err
	}
	if len(req.Items) == 0 {
		return ai.TranslateBatchResult{Items: []ai.TranslateItem{}}, nil
	}
	if len(req.Items) > maxBatchTranslateItems {
		return ai.TranslateBatchResult{}, fmt.Errorf("Batch exceeds maximum of %d items", maxBatchTranslateItems)
	}
	targetLang := strings.TrimSpace(req.ToLang)
	if targetLang == "" {
		targetLang = cfg.TranslateLanguage
	}
	if targetLang == "" {
		targetLang = "zh"
	}
	model := cfg.EffectiveTranslateModel()
	system, user, err := ai.BuildTranslateBatchPrompt(req.Items, req.FromLang, targetLang)
	if err != nil {
		return ai.TranslateBatchResult{}, err
	}

	reqCtx, cleanup := r.registerAICancel(ctx, req.RequestID, aiCompleteTimeout)
	defer cleanup()

	temp := 0.1
	raw, err := provider.Complete(reqCtx, ai.CompletionRequest{
		Model:       model,
		System:      system,
		Messages:    []ai.Message{{Role: "user", Content: user}},
		Temperature: &temp,
		MaxTokens:   4096,
		JSONMode:    cfg.Provider == ai.ProviderOpenAI && cfg.APIMode == ai.APIModeCompletions,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return ai.TranslateBatchResult{}, errors.New("AI_CANCELLED: Request cancelled")
		}
		return ai.TranslateBatchResult{}, err
	}
	items := ai.ParseBatchTranslation(raw, req.Items)
	return ai.TranslateBatchResult{
		Items:      items,
		Translated: len(items),
		Total:      len(req.Items),
		Model:      model,
	}, nil
}

func (s *AIService) GetTranslationCache(ctx context.Context, req CachedTranslationRequest) (CachedTranslation, error) {
	r := s.Runtime
	if r == nil || r.DB == nil || strings.TrimSpace(req.MessageID) == "" || strings.TrimSpace(req.TargetLang) == "" {
		return CachedTranslation{Segments: map[string]string{}}, nil
	}
	entry, err := r.DB.GetAICache(ctx, req.MessageID, "translation", req.TargetLang)
	if err != nil || entry == nil {
		return CachedTranslation{Segments: map[string]string{}}, nil
	}
	if req.ContentHash != "" && entry.ContentHash != req.ContentHash {
		return CachedTranslation{Segments: map[string]string{}}, nil
	}
	var payload cachedTranslationPayload
	if json.Unmarshal([]byte(entry.Payload), &payload) != nil {
		return CachedTranslation{Segments: map[string]string{}}, nil
	}
	if payload.Segments == nil {
		payload.Segments = map[string]string{}
	}
	return CachedTranslation{
		Found:       true,
		Subject:     payload.Subject,
		Segments:    payload.Segments,
		Model:       entry.Model,
		GeneratedAt: entry.UpdatedAt,
	}, nil
}

func (s *AIService) SaveTranslationCache(ctx context.Context, req SaveTranslationCacheRequest) (Empty, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return Empty{}, errors.New("Profile database unavailable")
	}
	if strings.TrimSpace(req.MessageID) == "" || strings.TrimSpace(req.TargetLang) == "" {
		return Empty{}, errors.New("Message ID and target language are required")
	}
	encoded, err := json.Marshal(cachedTranslationPayload{
		Subject:  req.Subject,
		Segments: req.Segments,
	})
	if err != nil {
		return Empty{}, err
	}
	now := time.Now().Unix()
	err = r.DB.SaveAICache(ctx, store.AICacheEntry{
		TargetID:    req.MessageID,
		AccountID:   req.AccountID,
		Kind:        "translation",
		TargetLang:  req.TargetLang,
		ContentHash: req.ContentHash,
		Model:       req.Model,
		Payload:     string(encoded),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	return Empty{}, err
}

func (s *AIService) Cancel(_ context.Context, req CancelAIRequest) (Empty, error) {
	r := s.Runtime
	id := strings.TrimSpace(req.RequestID)
	if r == nil || id == "" {
		return Empty{}, nil
	}
	r.aiCancelMu.Lock()
	cancel, ok := r.aiCancels[id]
	if ok {
		delete(r.aiCancels, id)
	}
	r.aiCancelMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	return Empty{}, nil
}

func (s *AIService) ClearCache(ctx context.Context, _ Empty) (ClearAICacheResult, error) {
	r := s.Runtime
	if r == nil || r.DB == nil {
		return ClearAICacheResult{}, errors.New("Profile database unavailable")
	}
	deleted, err := r.DB.ClearAICache(ctx)
	if err != nil {
		return ClearAICacheResult{}, err
	}
	return ClearAICacheResult{Deleted: deleted}, nil
}
