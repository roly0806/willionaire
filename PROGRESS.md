# PROGRESS.md — 開發進度記錄

> 目的：讓不同 agent chat 視窗切換時，能快速掌握前次工作進度與狀態，接續開發。
> 更新時機：每次完成一個階段性任務後，請更新本檔案。

---

## 目前狀態總覽

- **對照 `DESIGN.md` Roadmap 進度**：Day 1、Day 2 ✅ 完成 ／ Day 3、4 ⬜ 尚未開始
- **專案可 build / run / test**：✅（`go build ./...`、`go vet ./...`、`go test ./...` 皆可正常執行）
- **Git 狀態**：目前只有 2 個 commit（`first commit`、`First Commit`），皆為骨架建置，尚未有功能性 commit。`main` 分支與 `origin/main` 同步。（Day 2 完成的變更尚未 commit）

---

## 已完成（Day 2）

- `pkg/guardrail/guardrail.go`：
  - 定義 `Status` 列舉（`COMPLETED` / `NEEDS_USER_INPUT` / `UNABLE_TO_SOLVE` / `RUNNING`），並提供 `(Status).IsValid()`。
  - 定義 Output Schema：`Output{Thought, Status, ToolCalls}`（JSON tag 採 camelCase：`thought` / `status` / `toolCalls`）。
  - `ToolCall` 目前只放 `Name` 欄位，`Arguments` 等留給 Day 3 依實際 FinMind Tool 需求再補（避免過度設計）。
  - `Output.Validate()`：驗證 `Status` 是否為合法列舉值。
  - `ModelCaller` interface：`Call(ctx, prompt) (string, error)`，作為 LLM Provider 的最小抽象，尚未綁定任何具體 SDK，留給 Day 3 workflow 串接時注入實作。
  - `ParseWithRetry(ctx, caller, prompt, maxRetries)`：呼叫模型 → JSON 解析 → `Validate()`；任一步失敗就把錯誤訊息附加進 prompt 重新呼叫，最多重試 `maxRetries` 次（含第一次，`maxRetries<=0` 視為 1）。全部失敗則回傳 error（不自動轉成 `UNABLE_TO_SOLVE`，那是 workflow 層的退場邏輯）。
- `pkg/guardrail/guardrail_test.go`：涵蓋首次成功、非法 JSON 重試後成功、非法 Status 重試後成功、全部重試失敗、caller 回傳 error 時重試、`caller == nil`、`maxRetries<=0` 邊界、`Status.IsValid()`、`Output.Validate()` 等 case，`go test ./...` 全數通過。

### 設計決策記錄：`guardrail` 為何不依賴 `pkg/logger`

- `guardrail.go` 目前用 `fmt.Errorf` 建構/包裝 error 值，**沒有**引入 `pkg/logger`（`log/slog`）。這是刻意的關注點分離，不是疏漏：
  - `fmt.Errorf` 負責「建構錯誤資料」，`slog` 負責「輸出日誌」（Console/File 副作用），兩者職責不同。
  - `guardrail` 定位為**純函式庫**：只回傳 `(*Output, error)`，不做任何 I/O 副作用，方便單元測試、也避免同一錯誤被重複 log。
  - **已知取捨**：`ParseWithRetry` 內部重試時，只有 `lastErr`（最後一次失敗原因）會被保留並透過 `%w` 往外傳；前面幾次重試失敗的細節目前**不會**被記錄下來（無論最終成功或失敗）。這跟 `DESIGN.md` 強調的「Execution Tracing 需完整重播」有一點落差。
  - **已與使用者確認的決議**：暫時維持現狀，不在 `guardrail` 內加 logger 依賴。如果 Day 3 的 `pkg/workflow` 需要逐次追蹤每次重試，**由 workflow 層自己處理**（例如：workflow 的 for 迴圈裡每次呼叫 `guardrail.ParseWithRetry(ctx, caller, prompt, 1)`（單次嘗試），在 loop 外層自己用 `slog` 記錄每個 step 的結果），而不是修改 `guardrail` 本身。下一個 agent 若要動這個設計，請先跟使用者確認。

---

## 已完成（Day 1）

- `pkg/logger/logger.go`：使用 Go 原生 `log/slog` 實作 Execution Logger。
  - `logger.New(Options{Dir, Level})` 回傳 `*slog.Logger` + `closeFn`。
  - 透過 `io.MultiWriter` 同時輸出至 **Console (stdout)** 與 **`logs/agent-<timestamp>.log`** 檔案。
  - 已驗證可正常運作（`go run ./cmd/main.go` 會印出啟動訊息並寫入 log 檔）。
- `cmd/main.go`：目前僅做 Logger 初始化與一次性啟動訊息輸出，尚無 Agent Loop 邏輯。
- `.gitignore`：已排除 `/logs/`、`/bin/`、`willionaire` 執行檔。
- 其餘套件（`pkg/workflow`、`pkg/tools`）**僅有骨架註解 + TODO**，尚無實作（`pkg/guardrail` 已於 Day 2 完成，見上）：
  - `pkg/workflow/workflow.go` → `// TODO(Day3): 定義 Loop 結構與 Run 方法，串接 guardrail / tools / logger。`
  - `pkg/tools/tools.go` → `// TODO(Day3): 定義 FinMind Client 與 GetDailyKLine 方法。`

---

## 待辦（依 DESIGN.md Roadmap）

### Day 2 — Output Schema + Auto-Retry Parser（`pkg/guardrail`）✅ 已完成
- [x] 定義 `Status` 列舉：`COMPLETED` / `NEEDS_USER_INPUT` / `UNABLE_TO_SOLVE` / `RUNNING`
- [x] 定義 Output Schema struct：至少包含 `Thought`（string）、`Status`、`ToolCalls`（清單）
- [x] 實作 `ParseWithRetry`：JSON 解析失敗時，自動帶錯誤訊息重新請求模型修正輸出

### Day 3 — FinMind Tool + Naive Agent Loop（`pkg/tools` + `pkg/workflow`）⬜ 未開始
- [ ] `pkg/tools`：封裝 FinMind API，實作個股日 K 線資料查詢（`GetDailyKLine` 之類方法）
  - 簡化資料筆數限制（固定回傳最近 N 筆），不做複雜分頁
- [ ] `pkg/workflow`：實作 Naive Agent Loop
  - Go 原生 `for` 迴圈
  - `maxSteps = 5` 硬性保險絲
  - 每輪：呼叫模型 → Guardrail 驗證 → 依 `Status` 決定續跑 / 退場
  - 串接 `pkg/tools`、`pkg/guardrail`、`pkg/logger`

### Day 4 — 多狀態退場機制 + 單元測試 ⬜ 未開始
- [ ] 實作 `UNABLE_TO_SOLVE` / `NEEDS_USER_INPUT` 的退場邏輯
- [ ] 補上單元測試（目前整個專案沒有任何 `_test.go` 檔案）

---

## 尚待確認 / 開放問題

- LLM Provider 尚未選定與串接。Day 2 已在 `pkg/guardrail` 定義好 `ModelCaller` interface（`Call(ctx, prompt) (string, error)`）作為抽象邊界，但**尚未有任何具體實作**（OpenAI / Claude / Gemini 等 SDK 都還沒接）。Day 3 的 `pkg/workflow` 需要決定用哪個 Provider，並寫一個實作 `ModelCaller` 的 struct 注入。
- `pkg/guardrail.ToolCall` 目前只有 `Name` 欄位，`Arguments` 之類欄位尚未定義，需等 Day 3 `pkg/tools` 的 FinMind Tool 介面確定後再回頭補齊（可能需要小改 `ToolCall` struct 並補測試）。
- FinMind API 的認證方式（是否需要 token）與 rate limit 尚未確認。
- Day 2 的變更尚未 commit（見上方「Git 狀態」）。

---

## 給下一個 Agent 的建議切入點

1. 先讀 `DESIGN.md` 掌握整體架構與原則（Naive-First、Guardrail 優先、Execution Tracing）。
2. 從 **Day 3（`pkg/tools` + `pkg/workflow`）** 開始接續：
   - 先決定 LLM Provider，寫一個實作 `guardrail.ModelCaller` 的 struct。
   - 再實作 `pkg/tools` 的 FinMind Client，視需要回頭調整 `guardrail.ToolCall` 補上 `Arguments` 欄位。
   - 最後在 `pkg/workflow` 串起 Naive Agent Loop（`maxSteps = 5`），呼叫 `guardrail.ParseWithRetry` 並依 `Status` 決定續跑或退場。
   - Execution Tracing：`guardrail` 本身不 log，**workflow 層要自己用 `pkg/logger`（`slog`）記錄每個 step / 每次重試的結果**，詳見上方「設計決策記錄」。
3. 實作完一小段就跑 `go build ./...`、`go test ./...` 確認可編譯且測試通過，並更新本檔案的「已完成」與「待辦」區塊。
