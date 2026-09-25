// Package workflow 負責 Naive Agent Loop 的控制邏輯。
//
// Day 1: 僅先建立骨架，尚未實作。
// Day 3 預計串接：Guardrail 驗證後的 Output Schema 與 FinMind Tool。
//
// 設計要點（呼應 DESIGN.md）：
//   - 使用 Go 原生 for 迴圈控制整個 Agent 執行流程。
//   - maxSteps = 5 為硬性保險絲，防止無限迴圈或失控消耗。
package workflow

// TODO(Day3): 定義 Loop 結構與 Run 方法，串接 guardrail / tools / logger。
