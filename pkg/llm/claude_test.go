package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// newTestCaller 建立一個指向本地 httptest.Server 的 ClaudeCaller，
// 關掉 SDK 內建的自動重試（WithMaxRetries(0)）讓測試結果可預期、不因重試拖慢。
func newTestCaller(baseURL string) *ClaudeCaller {
	return &ClaudeCaller{
		client: anthropic.NewClient(
			option.WithBaseURL(baseURL),
			option.WithAPIKey("test-key"),
			option.WithMaxRetries(0),
		),
		model:     "claude-test",
		maxTokens: 100,
	}
}

func TestClaudeCaller_Call_Success(t *testing.T) {
	var gotAPIKey, gotVersion string
	var gotBody anthropic.MessageNewParams

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-test",
			"content": [{"type": "text", "text": "hello from claude"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	c := newTestCaller(srv.URL)

	out, err := c.Call(context.Background(), "分析 2330")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out != "hello from claude" {
		t.Errorf("預期回應文字為 %q，實際為 %q", "hello from claude", out)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("預期 x-api-key 為 test-key，實際為 %q", gotAPIKey)
	}
	if gotVersion == "" {
		t.Error("預期 SDK 會自動帶上 anthropic-version header，實際為空")
	}
	if gotBody.Model != "claude-test" {
		t.Errorf("預期請求 model 為 claude-test，實際為 %q", gotBody.Model)
	}
	if gotBody.MaxTokens != 100 {
		t.Errorf("預期請求 max_tokens 為 100，實際為 %d", gotBody.MaxTokens)
	}
	if len(gotBody.Messages) != 1 {
		t.Fatalf("預期請求帶有 1 則訊息，實際為 %d 則", len(gotBody.Messages))
	}
}

func TestClaudeCaller_Call_ConcatenatesMultipleTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-test",
			"content": [{"type": "text", "text": "A"}, {"type": "text", "text": "B"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	c := newTestCaller(srv.URL)
	out, err := c.Call(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out != "AB" {
		t.Errorf("預期多個 text block 被依序串接為 %q，實際為 %q", "AB", out)
	}
}

func TestClaudeCaller_Call_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()

	c := newTestCaller(srv.URL)

	_, err := c.Call(context.Background(), "prompt")
	if err == nil {
		t.Fatal("預期 API 回傳非 2xx 狀態時，Call 回傳錯誤")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("預期錯誤訊息包含 Anthropic 回傳的錯誤訊息，實際為 %v", err)
	}
}

func TestClaudeCaller_Call_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-test",
			"content": [],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 0}
		}`))
	}))
	defer srv.Close()

	c := newTestCaller(srv.URL)

	_, err := c.Call(context.Background(), "prompt")
	if err == nil {
		t.Fatal("預期回應內容為空（沒有 text block）時回傳錯誤")
	}
}

func TestNewClaudeCallerFromEnv_UsesDefaults(t *testing.T) {
	c := NewClaudeCallerFromEnv()
	if c.model != defaultClaudeModel {
		t.Errorf("預期 model 為預設值 %q，實際為 %q", defaultClaudeModel, c.model)
	}
	if c.maxTokens != defaultMaxTokens {
		t.Errorf("預期 maxTokens 為預設值 %d，實際為 %d", defaultMaxTokens, c.maxTokens)
	}
}
