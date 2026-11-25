package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type openAIProvider struct {
	cfg    Config
	client *http.Client
}

func (p *openAIProvider) baseURL() string {
	raw := strings.TrimRight(strings.TrimSpace(p.cfg.Endpoint), "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/models"} {
		if strings.HasSuffix(raw, suffix) {
			raw = strings.TrimSuffix(raw, suffix)
			break
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		u.Path = "/v1"
	} else {
		u.Path = path
	}
	return strings.TrimRight(u.String(), "/")
}

func (p *openAIProvider) endpointURL(action string) string {
	raw := strings.TrimRight(strings.TrimSpace(p.cfg.Endpoint), "/")
	if action == "completions" && strings.HasSuffix(raw, "/chat/completions") {
		return raw
	}
	if action == "responses" && strings.HasSuffix(raw, "/responses") {
		return raw
	}
	base := p.baseURL()
	switch action {
	case "responses":
		return base + "/responses"
	case "models":
		return base + "/models"
	default:
		return base + "/chat/completions"
	}
}

func isReasoningModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		m = m[idx+1:]
	}
	return strings.HasPrefix(m, "o1") ||
		strings.HasPrefix(m, "o3") ||
		strings.HasPrefix(m, "o4") ||
		strings.Contains(m, "deepseek-reasoner") ||
		strings.HasPrefix(m, "gpt-5")
}

func (p *openAIProvider) setAuth(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
}

func (p *openAIProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	if p.cfg.APIMode == APIModeResponses {
		return p.completeResponses(ctx, req)
	}
	return p.completeChat(ctx, req)
}

func (p *openAIProvider) Stream(ctx context.Context, req CompletionRequest, onDelta func(string)) (string, error) {
	if p.cfg.APIMode == APIModeResponses {
		return p.streamResponses(ctx, req, onDelta)
	}
	return p.streamChat(ctx, req, onDelta)
}

func (p *openAIProvider) buildChatPayload(req CompletionRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	msgs := make([]map[string]string, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		msgs = append(msgs, map[string]string{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
	}
	payload := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}
	if req.Temperature != nil && !isReasoningModel(model) {
		payload["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		if isReasoningModel(model) {
			payload["max_completion_tokens"] = req.MaxTokens
		} else {
			payload["max_tokens"] = req.MaxTokens
		}
	}
	if req.JSONMode && !isReasoningModel(model) {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	return payload
}

func (p *openAIProvider) completeChat(ctx context.Context, req CompletionRequest) (string, error) {
	bodyBytes, err := json.Marshal(p.buildChatPayload(req, false))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL("completions"), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setAuth(httpReq)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", classifyHTTPError(resp.StatusCode, raw, p.cfg.APIKey)
	}
	return parseOpenAIChatJSON(raw)
}

func parseOpenAIChatJSON(raw []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", errors.New("Invalid JSON response from AI provider")
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("AI provider returned no completion choices")
	}
	content := extractOpenAIContent(parsed.Choices[0].Message.Content)
	if content == "" {
		content = parsed.Choices[0].Text
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("AI provider returned an empty response")
	}
	return content, nil
}

func extractOpenAIContent(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, part := range t {
			if m, ok := part.(map[string]any); ok {
				if txt, ok := m["text"].(string); ok {
					b.WriteString(txt)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func (p *openAIProvider) streamChat(ctx context.Context, req CompletionRequest, onDelta func(string)) (string, error) {
	bodyBytes, err := json.Marshal(p.buildChatPayload(req, true))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL("completions"), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setAuth(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream, application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := readBounded(resp.Body, maxResponseBytes)
		return "", classifyHTTPError(resp.StatusCode, raw, p.cfg.APIKey)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "event-stream") {
		raw, err := readBounded(resp.Body, maxResponseBytes)
		if err != nil {
			return "", err
		}
		text, err := parseOpenAIChatJSON(raw)
		if err != nil {
			return "", err
		}
		if onDelta != nil && text != "" {
			onDelta(text)
		}
		return text, nil
	}
	var out strings.Builder
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return out.String(), ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content any `json:"content"`
				} `json:"delta"`
				Message struct {
					Content any `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := extractOpenAIContent(chunk.Choices[0].Delta.Content)
		if delta == "" {
			delta = extractOpenAIContent(chunk.Choices[0].Message.Content)
		}
		if delta != "" {
			out.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return out.String(), ctx.Err()
		}
		return out.String(), err
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("AI provider returned an empty stream")
	}
	return out.String(), nil
}

func (p *openAIProvider) buildResponsesPayload(req CompletionRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	input := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		input = append(input, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}
	payload := map[string]any{
		"model":  model,
		"input":  input,
		"stream": stream,
	}
	if strings.TrimSpace(req.System) != "" {
		payload["instructions"] = req.System
	}
	if req.Temperature != nil && !isReasoningModel(model) {
		payload["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_output_tokens"] = req.MaxTokens
	}
	return payload
}

func (p *openAIProvider) completeResponses(ctx context.Context, req CompletionRequest) (string, error) {
	bodyBytes, err := json.Marshal(p.buildResponsesPayload(req, false))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL("responses"), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setAuth(httpReq)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", classifyHTTPError(resp.StatusCode, raw, p.cfg.APIKey)
	}
	return parseOpenAIResponsesJSON(raw)
}

func parseOpenAIResponsesJSON(raw []byte) (string, error) {
	var parsed struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", errors.New("Invalid JSON response from AI provider")
	}
	if strings.TrimSpace(parsed.OutputText) != "" {
		return parsed.OutputText, nil
	}
	var b strings.Builder
	for _, item := range parsed.Output {
		for _, part := range item.Content {
			if part.Text != "" {
				b.WriteString(part.Text)
			}
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("AI provider returned an empty response")
	}
	return b.String(), nil
}

func (p *openAIProvider) streamResponses(ctx context.Context, req CompletionRequest, onDelta func(string)) (string, error) {
	bodyBytes, err := json.Marshal(p.buildResponsesPayload(req, true))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL("responses"), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setAuth(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream, application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := readBounded(resp.Body, maxResponseBytes)
		return "", classifyHTTPError(resp.StatusCode, raw, p.cfg.APIKey)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "event-stream") {
		raw, err := readBounded(resp.Body, maxResponseBytes)
		if err != nil {
			return "", err
		}
		text, err := parseOpenAIResponsesJSON(raw)
		if err != nil {
			return "", err
		}
		if onDelta != nil && text != "" {
			onDelta(text)
		}
		return text, nil
	}
	var out strings.Builder
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxResponseBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return out.String(), ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		if ev.Type == "response.output_text.delta" && ev.Delta != "" {
			out.WriteString(ev.Delta)
			if onDelta != nil {
				onDelta(ev.Delta)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return out.String(), ctx.Err()
		}
		return out.String(), err
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errors.New("AI provider returned an empty stream")
	}
	return out.String(), nil
}

func (p *openAIProvider) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpointURL("models"), nil)
	if err != nil {
		return nil, err
	}
	p.setAuth(httpReq)
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError(resp.StatusCode, raw, p.cfg.APIKey)
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, errors.New("Invalid model list response from AI provider")
	}
	seen := make(map[string]struct{}, len(parsed.Data))
	out := make([]string, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (p *openAIProvider) Test(ctx context.Context) error {
	_, err := p.Complete(ctx, CompletionRequest{
		Model:     p.cfg.Model,
		Messages:  []Message{{Role: "user", Content: "Reply with OK."}},
		MaxTokens: 16,
	})
	return err
}
