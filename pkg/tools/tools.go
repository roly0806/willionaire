// Package tools 提供 Agent 可呼叫的外部工具封裝。
//
// Day 1: 僅先建立骨架，尚未實作。
// Day 3 預計實作：FinMind API（個股日 K 線）封裝。
//
// 設計要點（呼應 DESIGN.md）：
//   - 第一階段只支援 FinMind API 個股日 K 線資料。
//   - 簡化資料筆數限制（例如固定回傳最近 N 筆），不做複雜分頁。
//   - 暫不實作 Tool Pruning，先以單一工具跑通全流程。
package tools

// TODO(Day3): 定義 FinMind Client 與 GetDailyKLine 方法。
