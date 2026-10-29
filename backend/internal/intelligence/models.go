package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// modelListTimeout bounds a provider model-list query. Listing is an
// interactive settings-page action, so it must fail fast rather than hang
// the UI.
const modelListTimeout = 15 * time.Second

// ListModels returns the model IDs apiKey can use with the given LLM
// provider, by querying the provider's model-list endpoint. It exists so the
// settings UI can offer a real model picker instead of asking users to type
// model names (a wrong name surfaces only as a later 400 from the chat
// endpoint).
//
// Only the providers this system supports are listed; anything else returns
// an error rather than guessing an endpoint.
func ListModels(ctx context.Context, provider Provider, apiKey string) ([]string, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key available for provider %q", provider)
	}
	client := &http.Client{Timeout: modelListTimeout}

	switch provider {
	case ProviderOpenAI:
		return listOpenAIModels(ctx, client, "https://api.openai.com/v1/models", apiKey)
	case ProviderGroq:
		return listOpenAIModels(ctx, client, "https://api.groq.com/openai/v1/models", apiKey)
	case ProviderGemini:
		return listGeminiModels(ctx, client, "https://generativelanguage.googleapis.com/v1beta/models", apiKey)
	default:
		return nil, fmt.Errorf("model listing is not supported for provider %q", provider)
	}
}

// listOpenAIModels reads the OpenAI-style model list ({data:[{id}]}), which
// both api.openai.com and api.groq.com expose.
func listOpenAIModels(ctx context.Context, client *http.Client, url, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model list request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model list failed with status %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decodeJSONBody(resp, &payload); err != nil {
		return nil, fmt.Errorf("parse model list: %w", err)
	}

	ids := make([]string, 0, len(payload.Data))
	for _, d := range payload.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	return normalizeModelIDs(ids), nil
}

// listGeminiModels reads Google's native model list ({models:[{name}]}) and
// strips the "models/" prefix, keeping only models that can generate content
// (the endpoint we actually call), so embedding-only entries stay out of the
// picker.
func listGeminiModels(ctx context.Context, client *http.Client, url, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The key goes in a header, never the URL: URLs end up in logs.
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("model list request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("model list failed with status %d", resp.StatusCode)
	}

	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := decodeJSONBody(resp, &payload); err != nil {
		return nil, fmt.Errorf("parse model list: %w", err)
	}

	ids := make([]string, 0, len(payload.Models))
	for _, m := range payload.Models {
		name := strings.TrimPrefix(m.Name, "models/")
		if name == "" {
			continue
		}
		if len(m.SupportedGenerationMethods) > 0 &&
			!containsString(m.SupportedGenerationMethods, "generateContent") {
			continue
		}
		ids = append(ids, name)
	}
	return normalizeModelIDs(ids), nil
}

// normalizeModelIDs trims, deduplicates, and sorts for a stable picker.
func normalizeModelIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// decodeJSONBody decodes a response body, capping its size: a provider
// response is a model list, not a data stream.
func decodeJSONBody(resp *http.Response, dst any) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}
