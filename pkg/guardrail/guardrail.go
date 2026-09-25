// Package guardrail 定義 Agent 的 Output Schema 與防禦性驗證邏輯。
//
// Day 1: 僅先建立骨架，尚未實作。
// Day 2 預計實作：Output Schema Struct 與 JSON Auto-Retry Parser。
//
// 設計要點（呼應 DESIGN.md）：
//   - 強制模型輸出 JSON，Schema 至少包含 Thought / Status / ToolCalls。
//   - Status 僅允許：COMPLETED / NEEDS_USER_INPUT / UNABLE_TO_SOLVE / RUNNING。
//   - JSON 解析失敗時，自動帶上錯誤訊息重新請求模型修正輸出。
package guardrail

// TODO(Day2): 定義 Output struct 與 Status 列舉、ParseWithRetry 函式。
