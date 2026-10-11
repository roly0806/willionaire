// Package tools 提供 Agent 可呼叫的外部工具封裝。
//
// Day3：實作 FinMind API 封裝，支援個股日 K 線資料查詢（GetDailyKLine）。
//
// 設計要點（呼應 DESIGN.md）：
//   - 第一階段只支援 FinMind API 個股日 K 線資料（dataset=TaiwanStockPrice）。
//   - 簡化資料筆數限制：固定回傳「最近 N 筆」（N=30），不做複雜分頁。
//   - 暫不實作 Tool Pruning，先以單一工具跑通全流程。
//
// 關於 FinMind 認證（見 PROGRESS.md「尚待確認」）：
//   - FinMind /data 端點在「不帶 token」時即可使用，限制為 300 requests/hour；
//     帶 token 後可提升到 600 requests/hour。
//   - 使用者目前尚未申請/訂閱 token，因此本實作先以「不帶 token」的公開端點運作。
//   - Token 欄位已預留（NewFinMindClient 會讀取環境變數 FINMIND_API_TOKEN，
//     若未設定則維持空字串、不帶 token 呼叫），待使用者之後補上金鑰即可直接生效，
//     不需要再改程式碼。
//
// Day3 code review 後的調整：
//   - HTTP 傳輸層改用 pkg/httpclient（與 pkg/llm 共用），FinMindClient.GetDailyKLine
//     只保留「組參數 → 解析 FinMind 業務回應 → 取最後 N 筆」這些業務邏輯。
//   - FormatForPrompt 改名為 FormatKLinesForPrompt，明確表達「格式化的對象是 K 線資料」。
//   - 新增 GetDailyKLineTool，包裝 FinMindClient 並實作 pkg/workflow.Tool 這個通用介面
//     （Name/Description/Execute），讓 pkg/workflow 不需要認識任何 FinMind 專屬的型別或
//     介面 —— 這是刻意用「結構化型別」達成的：GetDailyKLineTool 完全不 import
//     pkg/workflow，單純因為方法簽章（context.Context / map[string]string / string /
//     error）符合 workflow.Tool 的要求，就自動滿足該介面。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/roly/willionaire/pkg/httpclient"
)

const (
	// finMindBaseURL 是 FinMind v4 /data 端點。
	finMindBaseURL = "https://api.finmindtrade.com/api/v4/data"

	// finMindDataset 是本階段唯一支援的資料集：台股日成交資訊。
	finMindDataset = "TaiwanStockPrice"

	// DefaultDailyKLineCount 是「固定回傳最近 N 筆」的 N（依 Day3 確認結果，N=30）。
	DefaultDailyKLineCount = 30

	// lookbackCalendarDaysMultiplier 是為了確保能撈到至少 DefaultDailyKLineCount 筆
	// 交易日資料（需扣除週末/國定假日），往前推算查詢起始日時的行事曆天數緩衝倍數。
	lookbackCalendarDaysMultiplier = 3

	requestTimeout = 10 * time.Second

	// toolNameGetDailyKLine、argStockID 是 GetDailyKLineTool 對外的名稱/參數約定，
	// 會被 pkg/workflow.BuildInitialPrompt 用來組裝進 prompt 的工具說明裡
	// （透過 Tool.Name() / Tool.Description()，workflow 本身不需要知道這兩個常數）。
	toolNameGetDailyKLine = "get_daily_kline"
	argStockID            = "stock_id"
)

// DailyKLine 代表一天的股價資訊。
//
// 欄位對應 FinMind TaiwanStockPrice dataset 的精簡子集：
// 只保留 Agent 分析走勢時需要的欄位（日期/開/高/低/收/量），
// 其餘欄位（例如 Trading_money、spread、Trading_turnover）先不處理，避免過度設計。
type DailyKLine struct {
	Date          string  `json:"date"`
	StockID       string  `json:"stock_id"`
	Open          float64 `json:"open"`
	Max           float64 `json:"max"`
	Min           float64 `json:"min"`
	Close         float64 `json:"close"`
	TradingVolume int64   `json:"Trading_Volume"`
}

// finMindResponse 對應 FinMind /data 端點的原始 JSON 回應結構。
type finMindResponse struct {
	Msg    string       `json:"msg"`
	Status int          `json:"status"`
	Data   []DailyKLine `json:"data"`
}

// FinMindClient 是 FinMind API 的簡易封裝。
type FinMindClient struct {
	http    *httpclient.Client
	baseURL string
	// token 目前預設為空字串（見上方套件說明），保留供使用者之後補上金鑰。
	token string
}

// NewFinMindClient 建立一個 FinMindClient。
//
// 會嘗試從環境變數 FINMIND_API_TOKEN 讀取 token；若未設定，
// 則以不帶 token 的方式呼叫公開端點（300 requests/hour 限制）。
func NewFinMindClient() *FinMindClient {
	return &FinMindClient{
		http:    httpclient.New(requestTimeout),
		baseURL: finMindBaseURL,
		token:   os.Getenv("FINMIND_API_TOKEN"),
	}
}

// GetDailyKLine 查詢指定股票代號最近 DefaultDailyKLineCount 筆的日 K 線資料，
// 依日期由舊到新排序（與 FinMind 回應順序一致）。
//
// 依 DESIGN.md「簡化資料筆數限制」原則：實作方式是往前抓一段時間範圍的資料，
// 再取最後 N 筆，不做複雜分頁；若該區間內實際交易日不足 N 筆
// （例如新上市股票），則回傳實際能取得的筆數。
func (c *FinMindClient) GetDailyKLine(ctx context.Context, stockID string) ([]DailyKLine, error) {
	if strings.TrimSpace(stockID) == "" {
		return nil, fmt.Errorf("tools: stockID 不可為空")
	}

	end := time.Now()
	start := end.AddDate(0, 0, -DefaultDailyKLineCount*lookbackCalendarDaysMultiplier)

	q := url.Values{}
	q.Set("dataset", finMindDataset)
	q.Set("data_id", stockID)
	q.Set("start_date", start.Format("2006-01-02"))
	q.Set("end_date", end.Format("2006-01-02"))
	if c.token != "" {
		q.Set("token", c.token)
	}

	resp, err := c.http.Do(ctx, httpclient.Request{
		Method: http.MethodGet,
		URL:    c.baseURL,
		Query:  q,
	})
	if err != nil {
		return nil, fmt.Errorf("tools: 呼叫 FinMind API 失敗: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tools: FinMind API 回應非 200（status=%d）: %s", resp.StatusCode, string(resp.Body))
	}

	var parsed finMindResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, fmt.Errorf("tools: 解析 FinMind 回應失敗: %w（原始回應: %s）", err, string(resp.Body))
	}
	if parsed.Status != http.StatusOK {
		return nil, fmt.Errorf("tools: FinMind API 回應錯誤（status=%d）: %s", parsed.Status, parsed.Msg)
	}

	data := parsed.Data
	if len(data) > DefaultDailyKLineCount {
		data = data[len(data)-DefaultDailyKLineCount:]
	}

	return data, nil
}

// FormatKLinesForPrompt 將日 K 線資料整理成「精簡結構化格式」的字串，方便塞進下一輪的模型 prompt。
//
// 設計取捨（呼應 Day3 討論）：保留日期/開/高/低/收/量等技術分析所需的關鍵欄位，
// 但不是塞入 FinMind 的原始 JSON（欄位較多、較浪費 token），也不是整理成
// 一段自然語言摘要（有遺漏關鍵數字風險）；改用一行一筆的精簡結構化文字，
// 兩者兼顧「不遺漏資訊」與「精簡」。
func FormatKLinesForPrompt(klines []DailyKLine) string {
	if len(klines) == 0 {
		return "（查無資料）"
	}

	var sb strings.Builder
	sb.WriteString("日期,股票代號,開盤,最高,最低,收盤,成交量\n")
	for _, k := range klines {
		fmt.Fprintf(&sb, "%s,%s,%.2f,%.2f,%.2f,%.2f,%d\n",
			k.Date, k.StockID, k.Open, k.Max, k.Min, k.Close, k.TradingVolume)
	}
	return sb.String()
}

// klineFetcher 抽象化「取得股票日 K 線」的能力，只在 pkg/tools 內部使用，
// 讓 GetDailyKLineTool 在測試時可以注入 mock、不需要真的打網路呼叫 FinMind API。
// 這個介面刻意不對外 export，也刻意不放在 pkg/workflow（呼應 Day3 code review：
// 「特定工具的介面不應該滲透到通用的 workflow 套件裡」）。
type klineFetcher interface {
	GetDailyKLine(ctx context.Context, stockID string) ([]DailyKLine, error)
}

// GetDailyKLineTool 包裝 FinMindClient，實作 pkg/workflow.Tool 這個通用介面
// （透過方法簽章結構化滿足，不需要 import pkg/workflow）。
type GetDailyKLineTool struct {
	fetcher klineFetcher
}

// NewGetDailyKLineTool 建立一個 GetDailyKLineTool，包裝傳入的 FinMindClient。
func NewGetDailyKLineTool(client *FinMindClient) *GetDailyKLineTool {
	return &GetDailyKLineTool{fetcher: client}
}

// Name 回傳這個工具在 ToolCall.Name 裡對應的名稱。
func (t *GetDailyKLineTool) Name() string {
	return toolNameGetDailyKLine
}

// Description 回傳供組裝進 prompt 的工具說明文字（給模型看，告訴它這個工具能做什麼、
// 參數格式是什麼）。
func (t *GetDailyKLineTool) Description() string {
	return fmt.Sprintf(
		"- %s：查詢指定台股股票代號最近 %d 個交易日的日 K 線資料（開/高/低/收/量）。\n  參數：{\"%s\": \"股票代號，例如 2330\"}",
		toolNameGetDailyKLine, DefaultDailyKLineCount, argStockID,
	)
}

// Execute 執行這個工具：從 arguments 取出 stock_id，呼叫 FinMind 查詢日 K 線，
// 並把結果整理成精簡結構化格式的字串回傳（供塞進下一輪 prompt）。
func (t *GetDailyKLineTool) Execute(ctx context.Context, arguments map[string]string) (string, error) {
	stockID := arguments[argStockID]
	if stockID == "" {
		return "", fmt.Errorf("tools: %s 缺少 %s 參數", toolNameGetDailyKLine, argStockID)
	}

	klines, err := t.fetcher.GetDailyKLine(ctx, stockID)
	if err != nil {
		return "", fmt.Errorf("tools: 呼叫 %s 失敗: %w", toolNameGetDailyKLine, err)
	}

	return FormatKLinesForPrompt(klines), nil
}
