package workflow

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/roly/willionaire/pkg/guardrail"
)

// mockCaller 依呼叫順序回傳預先設定好的回應（與 pkg/guardrail 測試中的用法一致）。
type mockCaller struct {
	responses []string
	calls     int
}

func (m *mockCaller) Call(_ context.Context, _ string) (string, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		return "", errors.New("mockCaller: 沒有更多預設回應")
	}
	return m.responses[idx], nil
}

// mockTool 是測試用的 Tool 實作，不依賴任何真實工具（例如 pkg/tools），
// 用來驗證 Loop 對 Tool 介面的通用派送邏輯，跟具體工具無關。
type mockTool struct {
	name        string
	description string
	calls       []map[string]string
	returnValue string
	returnErr   error
}

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return t.description }
func (t *mockTool) Execute(_ context.Context, arguments map[string]string) (string, error) {
	t.calls = append(t.calls, arguments)
	if t.returnErr != nil {
		return "", t.returnErr
	}
	return t.returnValue, nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLoop_Run_CompletesOnFirstStep(t *testing.T) {
	caller := &mockCaller{responses: []string{
		`{"thought":"不需要工具","status":"COMPLETED","toolCalls":[]}`,
	}}

	loop := NewLoop(caller, nil, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusCompleted {
		t.Errorf("預期 Status 為 COMPLETED，實際為 %q", out.Status)
	}
	if caller.calls != 1 {
		t.Errorf("預期模型只被呼叫 1 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestLoop_Run_RunningThenCompleted(t *testing.T) {
	caller := &mockCaller{responses: []string{
		`{"thought":"需要查股價","status":"RUNNING","toolCalls":[{"name":"get_daily_kline","arguments":{"stock_id":"2330"}}]}`,
		`{"thought":"分析完成","status":"COMPLETED","toolCalls":[]}`,
	}}
	tool := &mockTool{name: "get_daily_kline", returnValue: "日期,開,高,低,收\n2024-01-01,100,110,90,105\n"}

	loop := NewLoop(caller, []Tool{tool}, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusCompleted {
		t.Errorf("預期 Status 為 COMPLETED，實際為 %q", out.Status)
	}
	if caller.calls != 2 {
		t.Errorf("預期模型被呼叫 2 次，實際呼叫 %d 次", caller.calls)
	}
	if len(tool.calls) != 1 || tool.calls[0]["stock_id"] != "2330" {
		t.Errorf("預期工具被呼叫 1 次（stock_id=2330），實際為 %v", tool.calls)
	}
}

func TestLoop_Run_UnknownToolName_RetriesWithError(t *testing.T) {
	caller := &mockCaller{responses: []string{
		`{"thought":"用了不存在的工具","status":"RUNNING","toolCalls":[{"name":"not_a_real_tool"}]}`,
		`{"thought":"修正後","status":"COMPLETED","toolCalls":[]}`,
	}}
	tool := &mockTool{name: "get_daily_kline"}

	loop := NewLoop(caller, []Tool{tool}, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期第二輪成功，卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusCompleted {
		t.Errorf("預期 Status 為 COMPLETED，實際為 %q", out.Status)
	}
	if len(tool.calls) != 0 {
		t.Errorf("預期已知工具未被呼叫，實際為 %v", tool.calls)
	}
}

func TestLoop_Run_ToolExecuteError_RetriesWithError(t *testing.T) {
	caller := &mockCaller{responses: []string{
		`{"thought":"查股價","status":"RUNNING","toolCalls":[{"name":"get_daily_kline","arguments":{"stock_id":"9999"}}]}`,
		`{"thought":"修正後","status":"UNABLE_TO_SOLVE","toolCalls":[]}`,
	}}
	tool := &mockTool{name: "get_daily_kline", returnErr: errors.New("network down")}

	loop := NewLoop(caller, []Tool{tool}, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期成功結束（以 UNABLE_TO_SOLVE 退場），卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusUnableToSolve {
		t.Errorf("預期 Status 為 UNABLE_TO_SOLVE，實際為 %q", out.Status)
	}
}

func TestLoop_Run_MaxStepsReached_ReturnsLastOutputWithoutError(t *testing.T) {
	responses := make([]string, 0, MaxSteps)
	for i := 0; i < MaxSteps; i++ {
		responses = append(responses, `{"thought":"一直在跑","status":"RUNNING","toolCalls":[]}`)
	}
	caller := &mockCaller{responses: responses}

	loop := NewLoop(caller, nil, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期跑滿 maxSteps 後正常回傳（不報錯），卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusRunning {
		t.Errorf("預期最後一次的 Status 為 RUNNING，實際為 %q", out.Status)
	}
	if caller.calls != MaxSteps {
		t.Errorf("預期模型被呼叫 %d 次（=MaxSteps），實際呼叫 %d 次", MaxSteps, caller.calls)
	}
}

func TestLoop_Run_GuardrailAllRetriesFail_ReturnsError(t *testing.T) {
	// maxRetriesPerStep = 3，所以 3 次都給不合法 JSON 會讓 ParseWithRetry 失敗。
	caller := &mockCaller{responses: []string{
		"不合法 1", "不合法 2", "不合法 3",
	}}

	loop := NewLoop(caller, nil, silentLogger())
	_, err := loop.Run(context.Background(), "prompt")
	if err == nil {
		t.Fatal("預期 guardrail 重試全部失敗時回傳錯誤")
	}
}

func TestLoop_Run_NeedsUserInput_ExitsImmediately(t *testing.T) {
	caller := &mockCaller{responses: []string{
		`{"thought":"問題不夠明確","status":"NEEDS_USER_INPUT","toolCalls":[]}`,
	}}

	loop := NewLoop(caller, nil, silentLogger())
	out, err := loop.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out.Status != guardrail.StatusNeedsUserInput {
		t.Errorf("預期 Status 為 NEEDS_USER_INPUT，實際為 %q", out.Status)
	}
	if caller.calls != 1 {
		t.Errorf("預期模型只被呼叫 1 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestBuildInitialPrompt_ContainsQuestionAndSchema(t *testing.T) {
	tool := &mockTool{name: "get_daily_kline", description: "- get_daily_kline：查詢日 K 線。\n  參數：{\"stock_id\": \"2330\"}"}

	prompt := BuildInitialPrompt("分析 2330", []Tool{tool})
	if want := "分析 2330"; !strings.Contains(prompt, want) {
		t.Errorf("預期 prompt 包含使用者問題 %q，實際為 %q", want, prompt)
	}
	if !strings.Contains(prompt, tool.description) {
		t.Error("預期 prompt 包含工具自己的 Description() 內容（動態組裝，非寫死）")
	}
	if !strings.Contains(prompt, "COMPLETED") || !strings.Contains(prompt, "RUNNING") {
		t.Error("預期 prompt 包含 Status Schema 說明")
	}
}

func TestBuildInitialPrompt_NoTools(t *testing.T) {
	prompt := BuildInitialPrompt("分析 2330", nil)
	if !strings.Contains(prompt, "沒有可用工具") {
		t.Errorf("預期沒有工具時 prompt 有對應說明，實際為 %q", prompt)
	}
}

func TestNewLoop_BuildsRegistryFromToolsSlice(t *testing.T) {
	toolA := &mockTool{name: "tool_a"}
	toolB := &mockTool{name: "tool_b"}

	loop := NewLoop(&mockCaller{}, []Tool{toolA, toolB}, silentLogger())
	if len(loop.Tools) != 2 {
		t.Fatalf("預期註冊表有 2 個工具，實際為 %d 個", len(loop.Tools))
	}
	if loop.Tools["tool_a"] != toolA {
		t.Error("預期 tool_a 能在註冊表中找到對應的 mockTool")
	}
	if loop.Tools["tool_b"] != toolB {
		t.Error("預期 tool_b 能在註冊表中找到對應的 mockTool")
	}
}
