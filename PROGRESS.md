# PROGRESS.md — 開發進度記錄

> 目的：讓不同 agent chat 視窗切換時，能快速掌握前次工作進度與狀態，接續開發。
> 更新時機：每次完成一個階段性任務後，請更新本檔案。

---

## 目前狀態總覽

- **對照 `DESIGN.md` Roadmap 進度**：Day 1、Day 2、Day 3 ✅ 完成 ／ Day 4 ⬜ 尚未開始
- **專案可 build / run / test**：✅（`go build ./...`、`go vet ./...`、`go test ./...` 皆可正常執行，`gofmt -l .` 乾淨）
- **端到端手動驗證**：✅ 跑過 `go run ./cmd/main.go "分析 2330 最近走勢"`。因為 Claude API Key 尚未備妥（見下方開放問題），最後一步呼叫 Claude 失敗，但驗證了 Auto-Retry（重試 3 次）、錯誤逐層包裝（`llm` → `guardrail` → `workflow` → `main`）、Console + 檔案雙輸出、以及優雅退出（無 panic，`exit status 1`）皆正常運作。
- **Git 狀態**：Day 2 的變更已 commit（`ede7345`）。Day 3 完成的變更、以及 2026-10-11 的 SDK 換裝尚未 commit。

---

## 已完成（2026-10-11：pkg/llm 換成官方 SDK，最小必要換裝）

背景：使用者學習 Claude 官方 Messages API 規格後（見 `docs/WILLY_STUDY_ROADMAP.md`），決定把 `pkg/llm` 的
raw HTTP 實作換成官方 `github.com/anthropics/anthropic-sdk-go`。討論結論是「最小必要」換裝——只換 transport
層，請求內容維持 Stage 0 等級（單一 user 訊息、純文字 prompt、不帶 tools / output_config），把 Stage 1
（Tool Use）與 Stage 2（stop_reason）牽動的架構重構留到讀完那兩份文件後再一次處理，避免現在先改一半、
之後又因為 tool_use 機制整個打掉重寫。

- `go.mod`：新增 `github.com/anthropics/anthropic-sdk-go v1.80.0` 為直接依賴（`go mod tidy` 後確認，
  不是 indirect）。
- `pkg/llm/claude.go`（改寫）：
  - `ClaudeCaller` 內部改持有 `anthropic.Client`，`NewClaudeCallerFromEnv()` 改用 `anthropic.NewClient(...)`，
    認證資訊（`ANTHROPIC_API_KEY` 等）交給 SDK 自動從環境變數讀取，不用自己組 `x-api-key` header。
  - `Call()` 改用 `client.Messages.New(ctx, anthropic.MessageNewParams{...})`，回應的 `Content`
    （`[]ContentBlockUnion`）用 SDK 的 `block.AsText()` accessor 取文字，不用自己寫 `UnmarshalJSON`。
  - **刪除**（SDK 已涵蓋，不需要自己維護）：手刻的 `messagesRequest` / `messagesReqEntry` /
    `messagesResponse` struct 與其 JSON Marshal/Unmarshal；`pkg/httpclient` 在這裡的用法（SDK 內建
    http client、自動重試、`anthropic-version` header、User-Agent）；`outputJSONSchema()` /
    `output_config`（Structured Outputs）這個完全未經真實 API 驗證的自製 JSON Schema 技巧。
  - 移除 `outputJSONSchema()` 後，`pkg/llm` 也不再需要 import `pkg/guardrail`（純粹因為方法簽章符合
    `guardrail.ModelCaller` 介面就自動滿足，兩個套件完全不耦合）。
  - **刻意不動的部分**（留給 Stage 1 + 2 一起讀完再一次性重構）：`pkg/workflow.Tool` 介面（還沒加
    `InputSchema()`）、`guardrail.Output.Status`（還沒往 `stop_reason` 方向改）、`workflow.Loop` 的
    歷史組裝方式（還是字串拼接，不是 `[]MessageParam` 陣列）。
- `pkg/llm/claude_test.go`（改寫）：改用 `httptest.Server` + `option.WithBaseURL` / `option.WithAPIKey` /
  `option.WithMaxRetries(0)` 指向測試伺服器（而非原本直接組 `*http.Request`），涵蓋：成功呼叫（驗證
  header、`model`、`max_tokens`、`messages` 組裝正確）、多個 text block 依序串接、API 回傳非 2xx（驗證
  SDK 的 `apierror.Error` 訊息內仍帶有 Anthropic 回傳的原始錯誤文字）、回應內容為空、
  `NewClaudeCallerFromEnv` 預設值。
- **驗證**：`go build ./...`、`go vet ./...`、`go test ./...`、`gofmt -l .` 全部通過；手動跑
  `go run ./cmd/main.go "測試"`（仍無 `ANTHROPIC_API_KEY`）確認錯誤處理鏈路正常——順便發現 SDK 在缺少
  認證時的錯誤訊息比原本手刻版本更友善（會列出 SDK 嘗試過的認證來源與修正建議），算是換 SDK 的附帶好處。

---

## 已完成（Day 3）

開始前已與使用者確認的關鍵決策（詳見 chat 記錄，摘要如下）：

- LLM Provider：選用 **Anthropic Claude**。使用者當時尚未訂閱 API，確認「先把程式碼串接完成，等訂閱後再測真實呼叫」也可以接受。
- FinMind 認證：先用**免登入公開端點**（300 requests/hour），token 認證方式留給使用者之後補上，已記錄在 `pkg/tools/tools.go` 的套件說明與下方開放問題。
- `GetDailyKLine` 固定回傳最近 **N=30** 筆。
- Agent Loop 的初始問題輸入方式：採用 **CLI 參數**（`go run ./cmd/main.go "問題"`），理由是比 stdin 更簡單/robust（不用處理互動輸入），比寫死在程式碼裡更有彈性；沒有給參數時退回 `defaultQuestion`。
- 工具執行結果餵回下一輪 prompt 的格式：採用**精簡結構化格式**（每筆一行：日期/股票代號/開/高/低/收/量），而非原始 JSON 或純文字摘要，兼顧「不遺漏資訊」與「精簡」。
- `maxSteps=5` 保險絲觸發時：只記錄警告並跳出迴圈、回傳最後一次的 `Output`（不報錯、不強制改寫 Status），細緻的多狀態退場機制留給 Day4。

> 以下描述已經是**經過 Code Review 追加重構後的最終狀態**（同一天內完成，見下方「Code Review 決策記錄」），不是最初 Day3 落地的樣子。

### `pkg/httpclient`（新增，Code Review 後追加，DESIGN.md 原本沒規劃這個目錄）

- `Client.Do(ctx, Request) (*Response, error)`：只負責「建立請求 → 送出 → 讀取完整 body」這段傳輸層邏輯，讓 `pkg/tools`（GET + query params）與 `pkg/llm`（POST + JSON body + 自訂 headers）共用，不用各寫一份 `http.NewRequestWithContext` / `Do` / `io.ReadAll` 樣板程式碼。
- 刻意**不**處理「status code 是否代表成功」或「body 怎麼 unmarshal」——只要成功送出並收到回應（不論 2xx/4xx/5xx）都回傳 `*Response`，只有連線層失敗（建立請求失敗、網路錯誤、讀 body 失敗）才回傳 error。業務層的成功/失敗判斷（FinMind 用 body 內 `status` 欄位、Anthropic 用 HTTP status + body 內 `error` 欄位）留給呼叫方自己處理。
- `pkg/httpclient/httpclient_test.go`：用 `httptest.Server` 涵蓋 GET+query+headers、POST+body、非 200 狀態仍正常回傳（不報錯）、連線失敗、建立請求失敗（不合法 method）。

### `pkg/tools`

- `FinMindClient`：封裝 FinMind `/data` 端點（`dataset=TaiwanStockPrice`），內部改用 `pkg/httpclient.Client` 處理傳輸層，`GetDailyKLine` 只保留「組參數 → 解析 FinMind 業務回應（`status`/`msg`）→ 取最後 N 筆」的業務邏輯。
  - `NewFinMindClient()` 會讀取環境變數 `FINMIND_API_TOKEN`；若未設定則不帶 token 呼叫（300 req/hr 限制）。**使用者目前尚未申請 token**，之後補上金鑰即可直接生效，不需要改程式碼。
  - `GetDailyKLine(ctx, stockID)`：往前抓一段時間範圍（`N * 3` 個行事曆天，抓 30*3=90 天緩衝週末/假日）後取最後 30 筆，不做複雜分頁。若實際交易日不足 30 筆（例如新上市股票）則回傳實際筆數。
  - `FormatKLinesForPrompt(klines)`（Code Review 前叫 `FormatForPrompt`，改名讓函式名稱直接表達「格式化的對象是 K 線資料」）：把 K 線資料整理成「精簡結構化格式」字串。
  - `GetDailyKLineTool`（Code Review 後新增）：包裝 `FinMindClient`，實作 `pkg/workflow.Tool` 這個通用介面（`Name()` / `Description()` / `Execute(ctx, arguments)`）。刻意**不** import `pkg/workflow`——單純因為方法簽章符合，Go 用結構化型別自動判定滿足該介面，避免兩個套件互相 import。內部用一個未 export 的 `klineFetcher` interface 包住 `FinMindClient`，方便測試時注入 mock。
- `pkg/tools/tools_test.go`：涵蓋 `FinMindClient` 本身（回傳筆數超過/不足 30 筆、空 stockID、非 200 HTTP 狀態、FinMind 回應 `status != 200`、非法 JSON、請求參數含/不含 token）、`FormatKLinesForPrompt` 的空值與內容涵蓋、`GetDailyKLineTool` 的 `Name()`/`Description()`/`Execute()`（成功、缺參數、底層 fetcher 錯誤）。

### `pkg/llm`（新增，DESIGN.md 原本沒規劃這個目錄）

- 因為 `pkg/guardrail.ModelCaller` 只是介面，需要一個地方放具體 Provider 實作，所以新增 `pkg/llm` 承接，讓 `pkg/workflow` 保持「只依賴介面、不綁定 Provider」。
- `ClaudeCaller`：實作 `guardrail.ModelCaller`，改用 `pkg/httpclient.Client` 呼叫 Anthropic Messages API（`POST /v1/messages`），沒有引入官方 SDK（呼應 DESIGN.md「拒絕過度工程化」與只用 Go 原生能力的精神）。
  - `NewClaudeCallerFromEnv()`：從環境變數 `ANTHROPIC_API_KEY` 讀取金鑰。**使用者目前尚未訂閱 Anthropic API / 沒有金鑰**，因此本次只完成程式碼串接與 mock 測試，尚未經過真實 API 呼叫驗證（見下方開放問題）。
  - 模型名稱寫在常數 `defaultClaudeModel`（目前為 `claude-sonnet-4-5-20250929`），**訂閱後請重新確認實際可用的模型名稱**，可能需要調整。
  - **（Code Review 後新增，重要且未經實測）** 請求裡加了 `output_config.format`（Anthropic Structured Outputs，`type: "json_schema"`），透過 `outputJSONSchema()` 動態組出對應 `guardrail.Output` 結構的 JSON Schema，在 API 層面強制模型輸出合法 JSON、並用 `enum` 限制 `status` 只能是 `guardrail.Status` 定義的四個合法值（enum 值直接取自 `guardrail` 套件常數，避免兩邊定義飄移）。**這整段完全沒有打過真實 API 驗證**，已知的不確定性（詳見 `claude.go` 內的完整註解）：
    1. Anthropic 文件說 structured outputs 的 object 目前限制「`additionalProperties` 必須設為 `false`」，但我們的 `toolCalls[].arguments` 是「任意 key 的字串 map」（因為工具是通用 `Tool` 介面，`pkg/llm` 不該寫死某個工具的參數名稱），目前用 `additionalProperties: {"type": "string"}` 表達，**不確定 Anthropic 是否真的支援這種寫法**。
    2. `defaultClaudeModel` 是否真的支援 `output_config` 也還沒確認。
  - `guardrail.ParseWithRetry` 的 Auto-Retry 仍然保留，作為 defense-in-depth（即使 API 層強制了 schema，也不代表 100% 不會出錯）。
- `pkg/llm/claude_test.go`：用 `httptest.Server` 模擬 Anthropic API，涵蓋成功呼叫（驗證 Header/Body 組裝）、`output_config.format` 的結構與 `status` enum 值正確性、API 回錯誤（如認證失敗）、回應內容為空。

### `pkg/workflow`

- `Tool` interface（Code Review 後新增，取代原本定義在這裡的 `KLineFetcher`）：`Name() string` / `Description() string` / `Execute(ctx, arguments map[string]string) (string, error)`。定義在 `pkg/workflow`（消費方）而非某個特定工具的套件，跟 `pkg/guardrail` 定義 `ModelCaller` 是同樣的設計模式——`workflow` 完全不需要知道任何具體工具長什麼樣子。
- `Loop` struct + `NewLoop(caller, tools []Tool, logger)`：內部把 `tools` 整理成「名稱 → Tool」的註冊表（`map[string]Tool`）。`MaxSteps` 固定為 `DESIGN.md` 規定的 5，不對外開放調整（避免保險絲形同虛設）。
- `Loop.Run(ctx, initialPrompt)`：
  - 每輪呼叫 `guardrail.ParseWithRetry(ctx, caller, prompt, maxRetriesPerStep=3)`。
  - `Status != RUNNING`（即 `COMPLETED` / `NEEDS_USER_INPUT` / `UNABLE_TO_SOLVE`）：立刻回傳，結束迴圈。
  - `Status == RUNNING`：透過註冊表查 `call.Name` 找到對應 `Tool` 並呼叫 `Execute(ctx, call.Arguments)`，把結果（或錯誤訊息）**累加**進一個 `history` buffer，下一輪的 prompt = `initialPrompt + history`（確保多輪 RUNNING 時，之前幾輪的工具結果不會被覆蓋掉）。
  - 工具呼叫失敗（未知工具名稱／`Tool.Execute` 回傳錯誤，例如缺參數）：不會直接中止整個 Loop，而是把錯誤訊息帶入下一輪 prompt，讓模型自己看到錯誤並修正（呼應 guardrail 的 Auto-Retry 精神）。
  - 跑滿 `MaxSteps` 仍未退場：記錄一筆 `slog.Warn`，回傳最後一次的 `Output`，**不**強制改寫 Status（Day4 再處理）。
- `BuildInitialPrompt(userQuestion, tools []Tool)`：組合使用者問題 + 工具說明（**動態**依傳入的 `tools` 逐一呼叫 `Description()` 組裝，不寫死任何工具名稱）+ Output Schema 指令。這是目前唯一「教模型怎麼輸出合法 JSON」的地方。
- `pkg/workflow/workflow_test.go`：全部用 mock `ModelCaller` + mock `Tool`（完全不 import `pkg/tools`，驗證 workflow 真的跟具體工具無關），涵蓋：第一輪就 `COMPLETED`、`RUNNING` 一輪後 `COMPLETED`（驗證工具被正確呼叫、參數正確傳遞）、未知工具名稱、工具執行錯誤（皆驗證「不中止、帶錯誤重試」）、跑滿 `MaxSteps` 正常退出（不報錯）、guardrail 全部重試失敗（回傳 error）、`NEEDS_USER_INPUT` 立即退場、`NewLoop` 註冊表建立正確、`BuildInitialPrompt` 動態組裝內容檢查（含無工具時的情況）。

### `pkg/guardrail`（小改）

- `ToolCall` 補上 `Arguments map[string]string`（`json:"arguments,omitempty"`），對應工具需要的參數（例如 `get_daily_kline` 的 `stock_id`）。刻意用簡單的 `map[string]string`，不是幫每個工具寫專屬 struct，因為目前只有一個工具，避免過度設計。
- `guardrail_test.go` 補上 `TestParseWithRetry_ToolCallWithArguments`，驗證 `Arguments` 能正確被解析出來。

### `cmd/main.go`（改寫）

- 改用 CLI 參數接收初始問題（`flag.Args()`，沒給參數時用 `defaultQuestion`）。
- 串起 `llm.NewClaudeCallerFromEnv()` → `tools.NewFinMindClient()` → 包成 `tools.NewGetDailyKLineTool(finMind)` → 組成 `[]workflow.Tool{...}` → `workflow.NewLoop(caller, availableTools, log)` → `workflow.BuildInitialPrompt(question, availableTools)` → `loop.Run(ctx, prompt)`。
- 執行失敗時記錄 `slog.Error` 並 `os.Exit(1)`（不 panic）；成功時用 `fmt` 把最終 `Output` 的 `Status` / `Thought` / `ToolCalls` 印到 console（這是刻意跟 `slog` 分工：`slog` 負責 Execution Tracing 給開發者看，`fmt` 這裡是給執行程式的人看的「最終答案」，兩者職責不同——目前維持現狀，見下方 Code Review 決策記錄 Q5）。

### Code Review 決策記錄（Day3 完成後、commit 前的追加調整）

Day3 主要實作完成後，使用者做了一次 code review，討論出以下 5 點並實際落地（除了 Q5 維持現狀）：

| # | 討論點 | 決策 | 落地方式 |
|---|---|---|---|
| Q1 | `FinMindClient.GetDailyKLine` 把 HTTP 傳輸邏輯跟業務邏輯混在一起，能不能抽出來讓未來其他 HTTP API 也能共用？ | **B：直接做跨套件共用的 HTTP client** | 新增 `pkg/httpclient`，`pkg/tools`、`pkg/llm` 都改用它處理傳輸層 |
| Q2 | `FormatForPrompt` 這個函式名稱看不出來是把「什麼」format 成 prompt | **A：改名 `FormatKLinesForPrompt`** | 已重新命名，呼叫端（`pkg/tools` 內部）同步更新 |
| Q3 | `KLineFetcher` 這種特定工具的 interface 定義在通用的 `workflow.go` 裡，職責混雜 | **B：現在就引入通用 `Tool` 介面 + 註冊表機制** | `pkg/workflow` 新增 `Tool` interface（`Name`/`Description`/`Execute`）+ `map[string]Tool` 註冊表；`pkg/tools` 新增 `GetDailyKLineTool` 實作這個介面；`workflow.go` 完全不再 import `pkg/tools` |
| Q4 | Anthropic 有 `output_config`（structured outputs / json_schema）可以在 API 層面強制輸出格式，要不要用？ | **B：現在就先把程式碼提前寫好（即使無法實測）** | `pkg/llm.ClaudeCaller.Call` 加上 `output_config.format`（`outputJSONSchema()`），`status` enum 取自 `guardrail` 套件常數；**完全未經真實 API 驗證**，已知風險（`additionalProperties` 限制、模型是否支援）詳見上方 `pkg/llm` 說明與 `claude.go` 內的完整註解 |
| Q5 | `cmd/main.go` 最終結果用 `fmt` 印，不是 `slog`，`slog` 的職責到底是什麼？ | **C：維持現狀，不是什麼大問題** | 沒有改動；`slog`（Execution Tracing，給開發者）與 `fmt`（給執行者看的最終答案）的分工維持不變 |

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

### Day 3 — FinMind Tool + Naive Agent Loop（`pkg/tools` + `pkg/workflow`）✅ 已完成
- [x] `pkg/tools`：封裝 FinMind API，實作個股日 K 線資料查詢（`GetDailyKLine`）
  - 簡化資料筆數限制（固定回傳最近 30 筆），不做複雜分頁
- [x] `pkg/workflow`：實作 Naive Agent Loop
  - Go 原生 `for` 迴圈
  - `maxSteps = 5` 硬性保險絲
  - 每輪：呼叫模型 → Guardrail 驗證 → 依 `Status` 決定續跑 / 退場
  - 串接 `pkg/tools`、`pkg/guardrail`、`pkg/logger`
- [x]（額外）新增 `pkg/llm`：`ClaudeCaller` 實作 `guardrail.ModelCaller`（尚無法真實驗證，見開放問題）
- [x]（額外）`guardrail.ToolCall` 補上 `Arguments` 欄位
- [x]（額外）`cmd/main.go` 改用 CLI 參數接收初始問題

### Day 4 — 多狀態退場機制 + 單元測試 ⬜ 未開始
- [ ] 實作 `UNABLE_TO_SOLVE` / `NEEDS_USER_INPUT` 的退場邏輯
- [ ] 補上單元測試（目前整個專案沒有任何 `_test.go` 檔案）

---

## 尚待確認 / 開放問題

- **Claude API 尚未真正驗證**：使用者截至 2026-10-11 尚未訂閱 Anthropic API / 沒有 `ANTHROPIC_API_KEY`。`pkg/llm.ClaudeCaller` 已改用官方 SDK 並用 `httptest` 做過 mock 測試（見上方「已完成（2026-10-11）」），但**沒有跑過一次真實的 API 呼叫**。等使用者拿到 Key 之後：
  1. 設定環境變數 `ANTHROPIC_API_KEY`（SDK 會自動讀取，不需要改程式碼）。
  2. 確認 `pkg/llm/claude.go` 裡 `defaultClaudeModel`（目前為 `anthropic.ModelClaudeSonnet4_5_20250929`）在訂閱方案下是否真實可用，不對的話要修正。
  3. 重跑一次 `go run ./cmd/main.go "分析 2330 最近走勢"`，確認能真正拿到模型回應、Guardrail 解析成功、必要時能正確呼叫 FinMind 工具。
  4. ~~確認 `output_config.format`...~~ 已於 2026-10-11 移除（見上方「已完成（2026-10-11）」），不再適用。這個決定留到 Stage 1（Tool Use）讀完後，再決定要不要用 SDK 原生方式重新引入結構化輸出或改用真正的 tool_use。
- **FinMind token 尚未申請**：目前 `pkg/tools.NewFinMindClient()` 是以不帶 token 的公開端點運作（300 requests/hour）。已預留環境變數 `FINMIND_API_TOKEN`，使用者申請到 token 後只要設定環境變數即可生效（600 requests/hour），不需要改程式碼。
- `pkg/workflow.Loop.Run` 目前的多輪 prompt 組裝方式是「`initialPrompt` + 所有輪次工具結果的累加字串」，沒有把模型每一輪的 `thought` 也放進歷史紀錄。如果之後發現模型因為看不到自己前幾輪的推理過程而表現不好，可能需要調整（目前先求簡單跑通）。
- Day 3 完成的變更尚未 commit（見上方「Git 狀態」）。

---

## 給下一個 Agent 的建議切入點

1. 先讀 `DESIGN.md` 掌握整體架構與原則（Naive-First、Guardrail 優先、Execution Tracing）。
2. **優先處理「尚待確認 / 開放問題」裡的 Claude API Key**：這是目前唯一擋住「真實端到端跑通」的因素。有 Key 之後，先手動跑一次 `go run ./cmd/main.go "問題"`，確認模型回應、`defaultClaudeModel` 名稱是否正確、FinMind 工具呼叫是否正常，再往下開發（`pkg/llm` 已於 2026-10-11 換成官方 SDK，`output_config` 已移除，見上方「已完成（2026-10-11）」）。
3. **讀完 `TOOL_USE.md` + `STOP_REASON_FALLBACK.md` 後，進行 Stage 1/2 架構重構**：這兩份文件互相耦合（真正的 `tool_use`/`tool_result` 機制 + `stop_reason` 退場判斷是同一個架構決策的兩面），建議一次讀完、一次重構，範圍包含：
   - `pkg/workflow.Tool` 介面補上 `InputSchema()`，改用 SDK 的 `anthropic.ToolUnionParam` 組 `tools` 請求參數。
   - `guardrail.Output.Status` 可能被 SDK 原生的 `Message.StopReason` 取代或並存，需要重新設計退場判斷邏輯。
   - `pkg/workflow.Loop` 的歷史組裝方式從字串拼接改成 `[]anthropic.MessageParam` 陣列（`assistant(tool_use)` + `user(tool_result)` 成對 append）。
   - 詳見 `docs/WILLY_STUDY_ROADMAP.md` 的 Stage 1/2 說明與「待釐清／下次可以接續的方向」。
4. 接著進入 **Day 4（多狀態退場機制 + 補測試）**：
   - `UNABLE_TO_SOLVE` / `NEEDS_USER_INPUT` 目前在 `pkg/workflow.Loop.Run` 裡只是「遇到就直接回傳」，還沒有比較細緻的處理（例如：對使用者顯示什麼訊息、要不要有重試/降級邏輯）。
   - `maxSteps` 保險絲觸發時目前只是 log 警告 + 回傳最後一次 `Output`，Day4 可能要決定是否要在這種情況也視為一種特殊的退場狀態並讓上層知道「這是因為跑滿保險絲，不是模型自己判斷完成」。
   - 專案目前已經有 `_test.go`（Day2 的 `pkg/guardrail`、Day3 新增的 `pkg/tools` / `pkg/llm` / `pkg/workflow`），可以檢查是否有覆蓋率上的明顯缺口再補齊。
5. Execution Tracing：`guardrail` 本身不 log，**workflow 層用 `pkg/logger`（`slog`）記錄每個 step 的結果**，細節見上方「已完成（Day 3）」與 Day2 的「設計決策記錄」。
6. 實作完一小段就跑 `go build ./...`、`go vet ./...`、`go test ./...`（必要時 `gofmt -l .`）確認可編譯且測試通過，並更新本檔案的「已完成」與「待辦」區塊。
