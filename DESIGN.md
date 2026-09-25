# DESIGN.md — Willionaire

台股盤後分析 AI Agent Harness（Go / Naive-First）

## 1. Project Vision & Core Principles

用 Go 打造一個**輕量但 Production-grade** 的台股盤後分析 Agent Harness。
採「先 Naive 跑通，重防禦與可觀測性」策略，拒絕過度工程化（No over-engineering）。

核心原則：

- **Naive Loop 控制**：用最直白的迴圈邏輯驅動 Agent，避免複雜的 Graph / State Machine 框架。
- **Guardrail 防禦優先**：Agent 的每一步輸出都必須經過結構化驗證，先求不失控，再求聰明。
- **內建 Execution Tracing**：任何一次執行都要能被完整重播與追蹤，方便除錯與事後分析。

## 2. System Architecture (Naive Version)

### Agent Loop — `pkg/workflow`

- 使用 Go 原生 `for` 迴圈控制整個 Agent 執行流程。
- 設定 `maxSteps = 5` 作為**硬性保險絲**，防止無限迴圈或失控消耗。
- 每一輪迭代：呼叫模型 → Guardrail 驗證 → 依 `Status` 決定續跑 / 退場。

### Output Schema & Guardrail — `pkg/guardrail`

- 強制模型輸出 JSON，Schema 至少包含：
  - `Thought`：模型的推理過程（純文字）
  - `Status`：`COMPLETED` / `NEEDS_USER_INPUT` / `UNABLE_TO_SOLVE` / `RUNNING`
  - `ToolCalls`：本輪要執行的工具呼叫清單
- 內建 **Auto-Retry**：JSON 解析失敗時，自動帶上錯誤訊息重新請求模型修正輸出。

### Context & Tools — `pkg/tools`

- 第一階段先專注支援 **FinMind API**（個股日 K 線資料）。
- 簡化資料筆數限制（例如固定回傳最近 N 筆），不做複雜分頁。
- 暫不實作 Tool Pruning / 動態工具篩選，先以單一工具跑通全流程。

### Execution Logger — `pkg/logger`

- 採用 Go 原生 `log/slog`，不引入第三方 logging 套件。
- Execution Tracing 同時輸出至：
  - Terminal Console（開發時即時觀察）
  - 本地 `.log` 檔案（事後追蹤、除錯）

## 3. Directory Structure

```
Willionaire/
├── cmd/
│   └── main.go
├── pkg/
│   ├── workflow/    # Agent Loop 控制 (maxSteps 保險絲)
│   ├── guardrail/   # Output Schema + Auto-Retry Parser
│   ├── tools/        # FinMind API 封裝
│   └── logger/       # log/slog Console + File 輸出
├── DESIGN.md
└── README.md
```

## 4. Development Roadmap

| Day | 目標 |
|---|---|
| Day 1 | 完成 `DESIGN.md`，初始化 `pkg/logger`（`log/slog` Console + File 雙輸出） |
| Day 2 | 定義 Output Schema Struct（`Thought` / `Status` / `ToolCalls`），實作 Auto-Retry Parser |
| Day 3 | 封裝 FinMind Tool（個股日 K 線），串接 `pkg/workflow` 的 Naive Agent Loop |
| Day 4 | 實作多狀態退場機制（`UNABLE_TO_SOLVE` / `NEEDS_USER_INPUT`），補上單元測試 |
