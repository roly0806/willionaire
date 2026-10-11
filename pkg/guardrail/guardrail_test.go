package guardrail

import (
	"context"
	"errors"
	"testing"
)

// mockCaller 是測試用的 ModelCaller 實作，依照呼叫順序回傳預先設定好的回應。
type mockCaller struct {
	responses []string
	errs      []error
	calls     int
}

func (m *mockCaller) Call(_ context.Context, _ string) (string, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		return "", errors.New("mockCaller: 沒有更多預設回應")
	}
	var err error
	if idx < len(m.errs) {
		err = m.errs[idx]
	}
	return m.responses[idx], err
}

func TestParseWithRetry_SuccessOnFirstTry(t *testing.T) {
	caller := &mockCaller{
		responses: []string{
			`{"thought":"分析完成","status":"COMPLETED","toolCalls":[]}`,
		},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 3)
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if out.Status != StatusCompleted {
		t.Errorf("預期 Status 為 %q，實際為 %q", StatusCompleted, out.Status)
	}
	if out.Thought != "分析完成" {
		t.Errorf("預期 Thought 為 %q，實際為 %q", "分析完成", out.Thought)
	}
	if caller.calls != 1 {
		t.Errorf("預期只呼叫 1 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestParseWithRetry_InvalidJSON_RetriesThenSucceeds(t *testing.T) {
	caller := &mockCaller{
		responses: []string{
			`這不是合法的 JSON`,
			`{"thought":"重試後修正","status":"RUNNING","toolCalls":[{"name":"get_daily_kline"}]}`,
		},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 3)
	if err != nil {
		t.Fatalf("預期第二次成功，卻回傳錯誤: %v", err)
	}
	if out.Status != StatusRunning {
		t.Errorf("預期 Status 為 %q，實際為 %q", StatusRunning, out.Status)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "get_daily_kline" {
		t.Errorf("預期 ToolCalls 包含 get_daily_kline，實際為 %+v", out.ToolCalls)
	}
	if caller.calls != 2 {
		t.Errorf("預期呼叫 2 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestParseWithRetry_ToolCallWithArguments(t *testing.T) {
	caller := &mockCaller{
		responses: []string{
			`{"thought":"需要查股價","status":"RUNNING","toolCalls":[{"name":"get_daily_kline","arguments":{"stock_id":"2330"}}]}`,
		},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 1)
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if len(out.ToolCalls) != 1 {
		t.Fatalf("預期 1 個 ToolCall，實際為 %d 個", len(out.ToolCalls))
	}
	got := out.ToolCalls[0]
	if got.Name != "get_daily_kline" {
		t.Errorf("預期 Name 為 %q，實際為 %q", "get_daily_kline", got.Name)
	}
	if got.Arguments["stock_id"] != "2330" {
		t.Errorf("預期 Arguments[stock_id] 為 %q，實際為 %q", "2330", got.Arguments["stock_id"])
	}
}

func TestParseWithRetry_InvalidStatus_RetriesThenSucceeds(t *testing.T) {
	caller := &mockCaller{
		responses: []string{
			`{"thought":"狀態亂寫","status":"NOT_A_REAL_STATUS","toolCalls":[]}`,
			`{"thought":"修正後","status":"NEEDS_USER_INPUT","toolCalls":[]}`,
		},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 3)
	if err != nil {
		t.Fatalf("預期第二次成功，卻回傳錯誤: %v", err)
	}
	if out.Status != StatusNeedsUserInput {
		t.Errorf("預期 Status 為 %q，實際為 %q", StatusNeedsUserInput, out.Status)
	}
	if caller.calls != 2 {
		t.Errorf("預期呼叫 2 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestParseWithRetry_AllAttemptsFail(t *testing.T) {
	caller := &mockCaller{
		responses: []string{
			`不合法 1`,
			`不合法 2`,
			`不合法 3`,
		},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 3)
	if err == nil {
		t.Fatal("預期回傳錯誤，卻成功了")
	}
	if out != nil {
		t.Errorf("預期 out 為 nil，實際為 %+v", out)
	}
	if caller.calls != 3 {
		t.Errorf("預期呼叫 3 次（maxRetries），實際呼叫 %d 次", caller.calls)
	}
}

func TestParseWithRetry_CallerError_Retries(t *testing.T) {
	caller := &mockCaller{
		responses: []string{"", `{"thought":"成功","status":"COMPLETED","toolCalls":[]}`},
		errs:      []error{errors.New("network error")},
	}

	out, err := ParseWithRetry(context.Background(), caller, "prompt", 3)
	if err != nil {
		t.Fatalf("預期第二次成功，卻回傳錯誤: %v", err)
	}
	if out.Status != StatusCompleted {
		t.Errorf("預期 Status 為 %q，實際為 %q", StatusCompleted, out.Status)
	}
	if caller.calls != 2 {
		t.Errorf("預期呼叫 2 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestParseWithRetry_NilCaller(t *testing.T) {
	_, err := ParseWithRetry(context.Background(), nil, "prompt", 3)
	if err == nil {
		t.Fatal("預期 caller 為 nil 時回傳錯誤")
	}
}

func TestParseWithRetry_MaxRetriesZeroOrNegative_TreatedAsOne(t *testing.T) {
	caller := &mockCaller{
		responses: []string{`不合法`},
	}

	_, err := ParseWithRetry(context.Background(), caller, "prompt", 0)
	if err == nil {
		t.Fatal("預期失敗（唯一一次嘗試就是不合法 JSON）")
	}
	if caller.calls != 1 {
		t.Errorf("預期 maxRetries<=0 時視為 1 次，實際呼叫 %d 次", caller.calls)
	}
}

func TestStatus_IsValid(t *testing.T) {
	cases := []struct {
		status Status
		want   bool
	}{
		{StatusCompleted, true},
		{StatusNeedsUserInput, true},
		{StatusUnableToSolve, true},
		{StatusRunning, true},
		{Status("SOMETHING_ELSE"), false},
		{Status(""), false},
	}
	for _, c := range cases {
		if got := c.status.IsValid(); got != c.want {
			t.Errorf("Status(%q).IsValid() = %v，預期 %v", c.status, got, c.want)
		}
	}
}

func TestOutput_Validate(t *testing.T) {
	valid := &Output{Thought: "ok", Status: StatusCompleted, ToolCalls: nil}
	if err := valid.Validate(); err != nil {
		t.Errorf("預期合法 Output 通過驗證，卻回傳錯誤: %v", err)
	}

	invalid := &Output{Thought: "bad", Status: Status("WRONG"), ToolCalls: nil}
	if err := invalid.Validate(); err == nil {
		t.Error("預期不合法 Status 會回傳錯誤，卻沒有")
	}
}
