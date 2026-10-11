// Package guardrail 定義 Agent 的 Output Schema 與防禦性驗證邏輯。
//
// 設計要點（呼應 DESIGN.md）：
//   - 強制模型輸出 JSON，Schema 至少包含 Thought / Status / ToolCalls。
//   - Status 僅允許：COMPLETED / NEEDS_USER_INPUT / UNABLE_TO_SOLVE / RUNNING。
//   - JSON 解析失敗時，自動帶上錯誤訊息重新請求模型修正輸出（Auto-Retry）。
//
// LLM Provider 尚未選定，因此本套件只定義 ModelCaller 這個最小介面，
// 具體實作（呼叫哪個 SDK / API）留給後續（workflow 串接時）決定並注入。
package guardrail

import (
	"context"
	"encoding/json"
	"fmt"
)

// Status 代表 Agent 每一輪輸出的執行狀態。
type Status string

const (
	// StatusCompleted 表示任務已完成，可以結束 Agent Loop。
	StatusCompleted Status = "COMPLETED"
	// StatusNeedsUserInput 表示 Agent 需要使用者提供額外資訊才能繼續。
	StatusNeedsUserInput Status = "NEEDS_USER_INPUT"
	// StatusUnableToSolve 表示 Agent 判斷自己無法完成任務。
	StatusUnableToSolve Status = "UNABLE_TO_SOLVE"
	// StatusRunning 表示任務尚在進行中，Agent Loop 應該繼續下一輪。
	StatusRunning Status = "RUNNING"
)

// validStatuses 用於驗證輸出的 Status 是否為合法列舉值。
var validStatuses = map[Status]struct{}{
	StatusCompleted:      {},
	StatusNeedsUserInput: {},
	StatusUnableToSolve:  {},
	StatusRunning:        {},
}

// IsValid 回報 s 是否為合法的 Status 列舉值。
func (s Status) IsValid() bool {
	_, ok := validStatuses[s]
	return ok
}

// ToolCall 代表 Agent 這一輪要執行的一次工具呼叫。
//
// Day3：補上 Arguments 欄位（string -> string 的簡易 map），
// 對應 pkg/tools 的 FinMind Tool（例如 get_daily_kline 需要 stock_id 參數）。
// 用 map[string]string 而非為每個工具量身定制的 struct，是刻意的簡化：
// 目前只有單一工具，先不做強型別的參數 schema，避免過度設計。
type ToolCall struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// Output 是模型每一輪回應必須符合的結構化 Schema。
type Output struct {
	Thought   string     `json:"thought"`
	Status    Status     `json:"status"`
	ToolCalls []ToolCall `json:"toolCalls"`
}

// Validate 檢查 Output 是否符合基本的結構性要求。
// 目前僅驗證 Status 是否為合法列舉值；Thought 允許空字串，
// ToolCalls 允許為空清單（例如 Status 為 COMPLETED 時可能不需要工具呼叫）。
func (o *Output) Validate() error {
	if !o.Status.IsValid() {
		return fmt.Errorf("guardrail: 不合法的 status 值: %q（允許值：%s / %s / %s / %s）",
			o.Status, StatusCompleted, StatusNeedsUserInput, StatusUnableToSolve, StatusRunning)
	}
	return nil
}

// ModelCaller 是對 LLM Provider 的最小抽象介面。
// 具體實作（OpenAI / Claude / Gemini 等 SDK）由呼叫方注入，
// guardrail 套件本身不綁定任何特定 Provider。
type ModelCaller interface {
	// Call 帶著 prompt 呼叫模型，回傳模型的原始文字回應。
	Call(ctx context.Context, prompt string) (string, error)
}

// ParseWithRetry 呼叫 caller 取得模型回應，嘗試解析為 Output。
//
// 若解析失敗（JSON 格式錯誤或 Status 不合法），會將錯誤訊息附加到
// prompt 後重新呼叫模型，最多重試 maxRetries 次（含第一次）。
// 若 maxRetries <= 0，視為 1（至少呼叫一次）。
//
// 全部嘗試都失敗時，回傳最後一次的錯誤，不會自動把 Status 轉成
// UNABLE_TO_SOLVE —— 那屬於 Agent Loop（workflow 層）的退場邏輯。
func ParseWithRetry(ctx context.Context, caller ModelCaller, prompt string, maxRetries int) (*Output, error) {
	if caller == nil {
		return nil, fmt.Errorf("guardrail: caller 不可為 nil")
	}
	if maxRetries <= 0 {
		maxRetries = 1
	}

	currentPrompt := prompt
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		raw, err := caller.Call(ctx, currentPrompt)
		if err != nil {
			lastErr = fmt.Errorf("guardrail: 第 %d 次呼叫模型失敗: %w", attempt, err)
			currentPrompt = fmt.Sprintf("%s\n\n[上一輪錯誤]\n%s", prompt, lastErr.Error())
			continue
		}

		var out Output
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			lastErr = fmt.Errorf("guardrail: 第 %d 次 JSON 解析失敗: %w（原始回應: %s）", attempt, err, raw)
			currentPrompt = fmt.Sprintf("%s\n\n[上一輪錯誤：JSON 解析失敗，請修正輸出格式後重新回應]\n%s", prompt, err.Error())
			continue
		}

		if err := out.Validate(); err != nil {
			lastErr = fmt.Errorf("guardrail: 第 %d 次驗證失敗: %w（原始回應: %s）", attempt, err, raw)
			currentPrompt = fmt.Sprintf("%s\n\n[上一輪錯誤：輸出驗證失敗，請修正後重新回應]\n%s", prompt, err.Error())
			continue
		}

		return &out, nil
	}

	return nil, fmt.Errorf("guardrail: 已重試 %d 次仍失敗: %w", maxRetries, lastErr)
}
