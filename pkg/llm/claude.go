// Package llm 提供 guardrail.ModelCaller 的具體 LLM Provider 實作。
//
// Day3 決策記錄：DESIGN.md 的目錄結構原本沒有規劃這個套件，是 Day3 才新增的。
// pkg/guardrail 只定義了 ModelCaller 介面（Call(ctx, prompt) (string, error)），
// 具體要呼叫哪個 SDK/API 需要一個實際落地的地方，因此新增 pkg/llm 承接這件事，
// 讓 pkg/workflow 保持「只依賴介面、不綁定特定 Provider」的原則。
//
// 2026-10-11 更新（SDK 換裝，詳見 docs/WILLY_STUDY_ROADMAP.md 的 SDK 選擇決策）：
// 改用官方 github.com/anthropics/anthropic-sdk-go 取代原本手刻的 net/http 呼叫。
// 這是刻意限定範圍的「最小必要」換裝，只換 transport 層，請求內容維持 Stage 0
// 等級（單一 user 訊息、純文字 prompt），不包含 tools / 多輪訊息歷史 /
// stop_reason 判斷——這些屬於 Stage 1（Tool Use）與 Stage 2（stop_reason）的
// 架構重構範圍，刻意留到讀完那兩份文件、確定新架構長相後再一次處理，避免現在
// 先改一半、之後又要因為 tool_use 機制整個打掉重寫。
//
// 這次換裝順便刪除的東西（SDK 已經涵蓋、不需要自己維護）：
//   - 手刻的 messagesRequest / messagesReqEntry / messagesResponse struct 與
//     JSON Marshal/Unmarshal——改用 SDK 的 anthropic.MessageNewParams /
//     anthropic.Message。
//   - 手刻的 HTTP 呼叫（pkg/httpclient 在這裡的用法）——SDK 內建 http client、
//     自動重試（預設重試 2 次）、anthropic-version header、User-Agent 等。
//     pkg/httpclient 本身保留，繼續給 pkg/tools（FinMind）使用。
//   - outputJSONSchema()／output_config（Structured Outputs）這個完全未經真實
//     API 驗證的自製 JSON Schema 技巧。拿掉的原因：
//     1. 它跟真正的 tool_use 機制（Stage 1 的主題）互斥，要怎麼取捨需要等
//     讀完 TOOL_USE.md 才能決定（例如改用 SDK 原生的 tool_use + tool_choice，
//     或是保留 structured output 但用 SDK 的型別重做）。
//     2. workflow 層的 guardrail.ParseWithRetry 本來就有 Auto-Retry 當
//     defense-in-depth，拿掉 output_config 不影響現有流程能不能動，只是
//     少了一層 API 端的強制力，純靠 prompt 文字指示（見
//     pkg/workflow.outputSchemaInstruction）。
//   - ContentBlock 的多型解析（text/tool_use/... 混雜的陣列）交給 SDK 的
//     ContentBlockUnion.AsText() 等 accessor 處理，不用自己寫
//     UnmarshalJSON（呼應 WILLY_STUDY_ROADMAP.md Stage 0 筆記提到的「做法 B」）。
//
// 目前狀態：使用者尚未訂閱 Anthropic API，本檔案只完成程式碼串接，
// 尚未經過真實 API 呼叫驗證。API Key 透過環境變數 ANTHROPIC_API_KEY 注入
// （anthropic.NewClient 的預設行為）；若未設定，ClaudeCaller 仍可建立成功，
// 只會在真正呼叫 Call() 時因為缺少認證資訊收到 Anthropic 回傳的錯誤——這是
// 刻意的設計，讓 pkg/workflow 可以先串接、編譯、測試（透過 mock），不因為
// 金鑰未備妥而卡住開發。
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	// defaultClaudeModel 是預設使用的模型名稱。
	// 注意：實際可用的模型名稱、以及訂閱方案是否支援，需視使用者訂閱方案而定，
	// 訂閱後請確認並視需要調整。
	defaultClaudeModel = anthropic.ModelClaudeSonnet5

	defaultMaxTokens      = 1024
	defaultRequestTimeout = 60 * time.Second
)

// ClaudeCaller 是 guardrail.ModelCaller 的 Anthropic Claude 實作。
//
// 內部改用官方 anthropic-sdk-go 的 Client（取代原本手刻的 net/http 呼叫，
// 見上方套件說明）。ClaudeCaller 完全不 import pkg/guardrail——
// 單純因為 Call 方法簽章符合 guardrail.ModelCaller 介面，Go 用結構化型別
// 自動判定滿足該介面，兩個套件不需要互相依賴。
type ClaudeCaller struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
}

// NewClaudeCallerFromEnv 建立一個 ClaudeCaller。
//
// 底層的 anthropic.NewClient() 會自動從環境變數讀取 ANTHROPIC_API_KEY
// （以及 ANTHROPIC_BASE_URL 等其他環境變數），不需要自己組 x-api-key header。
func NewClaudeCallerFromEnv() *ClaudeCaller {
	return &ClaudeCaller{
		client:    anthropic.NewClient(option.WithRequestTimeout(defaultRequestTimeout)),
		model:     defaultClaudeModel,
		maxTokens: defaultMaxTokens,
	}
}

// Call 實作 guardrail.ModelCaller：帶著 prompt 呼叫 Claude，回傳模型的原始文字回應。
//
// 目前維持 Stage 0 等級的陽春呼叫：單一 user 訊息、不帶 tools、不帶
// output_config。多輪對話歷史陣列、tool_use/tool_result、依 stop_reason
// 判斷下一步，留給 Stage 1/2 的架構重構（見 docs/WILLY_STUDY_ROADMAP.md）。
func (c *ClaudeCaller) Call(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm: 呼叫 Claude API 失敗: %w", err)
	}

	// resp.Content 是多型的 ContentBlockUnion 陣列（text/tool_use/... 混雜）；
	// 目前還沒有 tools，理論上只會有 text block，但保守起見用 Type 判斷後
	// 把所有 text block 串接起來，而不是只取第一個，避免漏掉內容。
	var text strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			text.WriteString(block.AsText().Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("llm: Claude 回應沒有文字內容")
	}

	return text.String(), nil
}
