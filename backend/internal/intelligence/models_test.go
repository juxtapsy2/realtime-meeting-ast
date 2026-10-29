package intelligence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListOpenAIModels(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o-mini"},{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`))
	}))
	defer srv.Close()

	models, err := listOpenAIModels(context.Background(), srv.Client(), srv.URL, "sk-test")
	if err != nil {
		t.Fatalf("listOpenAIModels() error = %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	want := []string{"gpt-4o", "gpt-4o-mini"} // deduped + sorted
	if !reflect.DeepEqual(models, want) {
		t.Errorf("models = %v, want %v", models, want)
	}
}

func TestListGeminiModels(t *testing.T) {
	var gotKey, gotKeyInURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		gotKeyInURL = r.URL.Query().Get("key")
		w.Write([]byte(`{"models":[
			{"name":"models/gemini-3.8-flash","supportedGenerationMethods":["generateContent"]},
			{"name":"models/text-embedding-004","supportedGenerationMethods":["embedContent"]},
			{"name":"models/gemini-3.8-pro","supportedGenerationMethods":["generateContent"]}
		]}`))
	}))
	defer srv.Close()

	models, err := listGeminiModels(context.Background(), srv.Client(), srv.URL, "g-key")
	if err != nil {
		t.Fatalf("listGeminiModels() error = %v", err)
	}
	if gotKey != "g-key" {
		t.Errorf("x-goog-api-key = %q, want g-key (key must not travel in the URL)", gotKey)
	}
	if gotKeyInURL != "" {
		t.Errorf("key leaked into URL query: %q", gotKeyInURL)
	}
	// Embedding-only model must be excluded; names lose the models/ prefix.
	want := []string{"gemini-3.8-flash", "gemini-3.8-pro"}
	if !reflect.DeepEqual(models, want) {
		t.Errorf("models = %v, want %v", models, want)
	}
}

func TestListModelsRejectsBadInput(t *testing.T) {
	if _, err := ListModels(context.Background(), ProviderGroq, ""); err == nil {
		t.Error("ListModels() with no key: want error, got nil")
	}
	if _, err := ListModels(context.Background(), Provider("azure"), "k"); err == nil {
		t.Error("ListModels() with unsupported provider: want error, got nil")
	}
}

func TestListModelsUpstreamStatusSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid api key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := listOpenAIModels(context.Background(), srv.Client(), srv.URL, "bad")
	if err == nil {
		t.Fatal("listOpenAIModels() with 401: want error, got nil")
	}
	// The key itself must never appear in the error.
	if containsString([]string{err.Error()}, "bad") {
		t.Errorf("error leaks the API key: %v", err)
	}
}
