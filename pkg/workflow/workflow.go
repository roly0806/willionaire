// Package workflow 負責 Naive Agent Loop 的控制邏輯。
//
// Day3：實作 Loop 結構與 Run 方法，串接 guardrail / tools / logger。
//
// 設計要點（呼應 DESIGN.md）：
//   - 使用 Go 原生 for 迴圈控制整個 Agent 執行流程。
//   - maxSteps = 5 為硬性保險絲，防止無限迴圈或失控消耗。
//   - 每一輪迭代：呼叫模型（經 guardrail.ParseWithRetry） → 依 Status 決定續跑 / 退場。
//
// Execution Tracing：guardrail 本身不做 I/O side effect（見 pkg/guardrail 的設計決策記錄），
// 所以這裡（workflow 層）用 *slog.Logger 記錄每一輪的結果，滿足 DESIGN.md 的
// 「內建 Execution Tracing：任何一次執行都要能被完整重播與追蹤」原則。
//
// Day3 已知簡化（多狀態退場機制留給 Day4）：
//   - 跑滿 MaxSteps 仍未進入退場狀態（COMPLETED/NEEDS_USER_INPUT/UNABLE_TO_SOLVE）時，
//     目前只記錄一筆警告並跳出迴圈，回傳最後一次的 Output，不會強制改寫 Status。
//
// Day3 code review 後的調整：workflow 不再認識任何特定工具（例如 FinMind 的
// get_daily_kline）。改成定義通用的 Tool 介面，Loop 透過「工具名稱 -> Tool」的
// 註冊表來執行 ToolCalls，讓 pkg/workflow 保持跟具體工具無關，未來加新工具只需要
// 在 pkg/tools（或其他套件）實作 Tool 介面並塞進 NewLoop 的 tools 參數，不需要改
// 這個檔案。
package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/roly/willionaire/pkg/guardrail"
)

const (
	// MaxSteps 是 Naive Agent Loop 的硬性保險絲（依 DESIGN.md 規定為 5）。
	MaxSteps = 5

	// maxRetriesPerStep 是每一輪呼叫模型時，guardrail.ParseWithRetry 允許的重試次數。
	maxRetriesPerStep = 3
)

// Tool 是 Agent 可以呼叫的工具的通用介面。
//
// 定義在 pkg/workflow（而非某個特定工具的套件）是因為：Loop 是這個介面的
// 消費者，由消費者定義「工具必須滿足什麼契約」，這跟 pkg/guardrail 定義
// ModelCaller（LLM Provider 必須滿足的契約）是同樣的設計模式。
//
// 具體工具（例如 pkg/tools.GetDailyKLineTool）不需要 import 這個套件就能滿足
// 這個介面——只要方法簽章符合，Go 會透過結構化型別自動判定滿足，避免
// pkg/tools 與 pkg/workflow 互相 import 造成循環依賴。
type Tool interface {
	// Name 回傳這個工具的名稱，對應 guardrail.ToolCall.Name，用於註冊表比對。
	Name() string
	// Description 回傳供組裝進 prompt 的工具說明文字（工具用途、參數格式），
	// 給模型看，讓模型知道有哪些工具可用、怎麼呼叫。
	Description() string
	// Execute 執行這個工具一次呼叫，回傳結果字串（供塞進下一輪 prompt）或錯誤。
	Execute(ctx context.Context, arguments map[string]string) (string, error)
}

// Loop 負責控制 Naive Agent Loop 的執行。
type Loop struct {
	Caller   guardrail.ModelCaller
	Tools    map[string]Tool
	Logger   *slog.Logger
	MaxSteps int
}

// NewLoop 建立一個 Loop。tools 會被整理成「名稱 -> Tool」的註冊表；
// 若 logger 為 nil，改用 slog.Default()；MaxSteps 固定採用 DESIGN.md 規定的 5，
// 不對外開放調整，避免保險絲形同虛設。
func NewLoop(caller guardrail.ModelCaller, tools []Tool, logger *slog.Logger) *Loop {
	if logger == nil {
		logger = slog.Default()
	}
	registry := make(map[string]Tool, len(tools))
	for _, t := range tools {
		registry[t.Name()] = t
	}
	return &Loop{
		Caller:   caller,
		Tools:    registry,
		Logger:   logger,
		MaxSteps: MaxSteps,
	}
}

// Run 執行 Naive Agent Loop：
//
//  1. 呼叫模型（經 guardrail.ParseWithRetry 驗證與 Auto-Retry）
//  2. 依 Status 決定續跑（RUNNING：執行 ToolCalls，把結果累積進下一輪 prompt）
//     或退場（COMPLETED / NEEDS_USER_INPUT / UNABLE_TO_SOLVE：直接回傳）
//  3. 跑滿 MaxSteps 仍未退場：記錄警告並跳出，回傳最後一次的 Output（不報錯）
//
// initialPrompt 應該已經包含 Output Schema 說明與可用工具說明
// （見 BuildInitialPrompt），Run 本身不負責組裝 Schema 指令。
func (l *Loop) Run(ctx context.Context, initialPrompt string) (*guardrail.Output, error) {
	maxSteps := l.MaxSteps
	if maxSteps <= 0 {
		maxSteps = MaxSteps
	}

	var history strings.Builder
	var lastOutput *guardrail.Output

	for step := 1; step <= maxSteps; step++ {
		prompt := initialPrompt + history.String()

		l.Logger.Info("workflow: 開始新一輪", "step", step, "maxSteps", maxSteps)

		out, err := guardrail.ParseWithRetry(ctx, l.Caller, prompt, maxRetriesPerStep)
		if err != nil {
			l.Logger.Error("workflow: guardrail 驗證失敗（已重試仍無法取得合法輸出）", "step", step, "error", err)
			return nil, fmt.Errorf("workflow: 第 %d 輪失敗: %w", step, err)
		}

		l.Logger.Info("workflow: 取得合法輸出", "step", step, "status", out.Status, "thought", out.Thought, "toolCalls", out.ToolCalls)
		lastOutput = out

		if out.Status != guardrail.StatusRunning {
			l.Logger.Info("workflow: 進入退場狀態，結束迴圈", "step", step, "status", out.Status)
			return out, nil
		}

		result, toolErr := l.executeToolCalls(ctx, out.ToolCalls)
		if toolErr != nil {
			l.Logger.Warn("workflow: 工具呼叫失敗，將錯誤訊息帶入下一輪", "step", step, "error", toolErr)
			fmt.Fprintf(&history, "\n\n[第 %d 輪工具執行錯誤]\n%s", step, toolErr.Error())
			continue
		}

		fmt.Fprintf(&history, "\n\n[第 %d 輪工具執行結果]\n%s", step, result)
	}

	l.Logger.Warn("workflow: 已達 maxSteps 上限，強制退出迴圈（細緻多狀態退場機制留給 Day4）",
		"maxSteps", maxSteps, "lastStatus", lastOutput.Status)
	return lastOutput, nil
}

// executeToolCalls 依序執行本輪的 ToolCalls，並把結果整理成單一字串回傳。
//
// 透過 l.Tools 這個「名稱 -> Tool」註冊表做派送，完全不需要知道有哪些具體工具、
// 也不需要知道每個工具的參數叫什麼名字——這些都是各個 Tool 實作自己的責任
// （見 pkg/tools.GetDailyKLineTool.Execute）。遇到未知工具名稱視為錯誤，
// 交給呼叫端決定如何處理（目前 Run 的作法是把錯誤帶入下一輪 prompt，
// 讓模型自己修正，而不是直接中止整個 Loop）。
func (l *Loop) executeToolCalls(ctx context.Context, calls []guardrail.ToolCall) (string, error) {
	if len(calls) == 0 {
		return "（本輪沒有工具呼叫）", nil
	}

	var sb strings.Builder
	for _, call := range calls {
		tool, ok := l.Tools[call.Name]
		if !ok {
			return "", fmt.Errorf("workflow: 未知的工具名稱: %q", call.Name)
		}
		result, err := tool.Execute(ctx, call.Arguments)
		if err != nil {
			return "", fmt.Errorf("workflow: 呼叫工具 %q 失敗: %w", call.Name, err)
		}
		sb.WriteString(result)
	}
	return sb.String(), nil
}

// outputSchemaInstruction 說明模型每一輪回應必須遵守的 JSON Schema（呼應 pkg/guardrail.Output）。
//
// 這裡的範例刻意用通用的佔位字樣（「工具名稱」「參數名」「參數值」），不寫死任何
// 具體工具的名稱或參數——具體工具的說明由 BuildInitialPrompt 依傳入的 tools 動態組裝
// （見下方），維持 workflow 對具體工具的無知。
const outputSchemaInstruction = `你必須只回覆一個 JSON 物件，不要有其他文字或 Markdown 標記，格式如下：
{
  "thought": "你的推理過程（純文字）",
  "status": "COMPLETED | NEEDS_USER_INPUT | UNABLE_TO_SOLVE | RUNNING",
  "toolCalls": [
    {"name": "工具名稱", "arguments": {"參數名": "參數值"}}
  ]
}

規則：
- 若還需要呼叫工具取得更多資訊才能回答，status 請設為 RUNNING，並在 toolCalls 填入本輪要呼叫的工具（工具名稱與參數請參考下方「可用工具」列表）。
- 若已經有足夠資訊可以回答使用者的問題，status 請設為 COMPLETED，thought 請包含完整分析結果，toolCalls 可為空陣列。
- 若需要使用者提供更多資訊才能繼續（例如問題中沒有指定股票代號），status 請設為 NEEDS_USER_INPUT。
- 若判斷自己無法完成任務，status 請設為 UNABLE_TO_SOLVE。`

// BuildInitialPrompt 組合使用者問題、可用工具說明（依傳入的 tools 動態組裝，
// 每個工具的說明來自它自己的 Description()）、以及 Output Schema 指令，
// 作為 Agent Loop 第一輪呼叫模型時的 prompt。
func BuildInitialPrompt(userQuestion string, tools []Tool) string {
	var toolsDesc strings.Builder
	toolsDesc.WriteString("可用工具：\n")
	if len(tools) == 0 {
		toolsDesc.WriteString("（本階段沒有可用工具）")
	}
	for _, t := range tools {
		toolsDesc.WriteString(t.Description())
		toolsDesc.WriteString("\n")
	}

	return fmt.Sprintf("使用者問題：%s\n\n%s\n%s", userQuestion, toolsDesc.String(), outputSchemaInstruction)
}
