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

const anthropicVersion = "2023-06-01"

type anthropicProvider struct {
	cfg    Config
	client *http.Client
}

func (p *anthropicProvider) baseURL() string {
	raw := strings.TrimRight(strings.TrimSpace(p.cfg.Endpoint), "/")
	for _, suffix := range []string{"/messages", "/models"} {
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

func (p *anthropicProvider) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	if p.cfg.APIKey != "" {
		req.Header.Set("x-api-key", p.cfg.APIKey)
	}
}

func (p *anthropicProvider) buildPayload(req CompletionRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	msgs := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := m.Role
		if role != "assistant" {
			role = "user"
		}
		msgs = append(msgs, map[string]string{"role": role, "content": m.Content})
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   msgs,
		"stream":     stream,
	}
	if strings.TrimSpace(req.System) != "" {
		payload["system"] = req.System
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	return payload
}

func parseAnthropicJSON(raw []byte) (string, error) {
	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", errors.New("Invalid JSON response from Anthropic provider")
	}
	var b strings.Builder
	for _, part := range parsed.Content {
		if part.Type == "text" || part.Text != "" {
			b.WriteString(part.Text)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errors.New("AI provider returned an empty response")
	}
	return b.String(), nil
}

func (p *anthropicProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	bodyBytes, err := json.Marshal(p.buildPayload(req, false))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setHeaders(httpReq)
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
	return parseAnthropicJSON(raw)
}

func (p *anthropicProvider) Stream(ctx context.Context, req CompletionRequest, onDelta func(string)) (string, error) {
	bodyBytes, err := json.Marshal(p.buildPayload(req, true))
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	p.setHeaders(httpReq)
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
		text, err := parseAnthropicJSON(raw)
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
		if data == "" {
			continue
		}
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			out.WriteString(ev.Delta.Text)
			if onDelta != nil {
				onDelta(ev.Delta.Text)
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

func (p *anthropicProvider) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL()+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpReq)
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
		return nil, errors.New("Invalid model list response from Anthropic provider")
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

func (p *anthropicProvider) Test(ctx context.Context) error {
	_, err := p.Complete(ctx, CompletionRequest{
		Model:     p.cfg.Model,
		Messages:  []Message{{Role: "user", Content: "Reply with OK."}},
		MaxTokens: 16,
	})
	return err
}
