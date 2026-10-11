package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/roly/willionaire/pkg/httpclient"
)

// newTestServer 建立一個模擬 FinMind /data 端點的測試伺服器，
// 回傳 n 筆假造的日 K 線資料（日期遞增），並記錄收到的請求供斷言。
func newTestServer(t *testing.T, n int, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		var sb strings.Builder
		sb.WriteString(`{"msg":"success","status":200,"data":[`)
		for i := 0; i < n; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, `{"date":"2024-01-%02d","stock_id":"2330","Trading_Volume":1000,"open":100.0,"max":110.0,"min":90.0,"close":105.0}`, i+1)
		}
		sb.WriteString(`]}`)
		_, _ = w.Write([]byte(sb.String()))
	}))
}

func newFinMindClient(baseURL string) *FinMindClient {
	return &FinMindClient{
		http:    httpclient.New(5 * time.Second),
		baseURL: baseURL,
	}
}

func TestGetDailyKLine_ReturnsLastN(t *testing.T) {
	srv := newTestServer(t, 50, http.StatusOK, "")
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	klines, err := c.GetDailyKLine(context.Background(), "2330")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if len(klines) != DefaultDailyKLineCount {
		t.Errorf("預期回傳 %d 筆，實際為 %d 筆", DefaultDailyKLineCount, len(klines))
	}
}

func TestGetDailyKLine_FewerThanN_ReturnsAll(t *testing.T) {
	srv := newTestServer(t, 5, http.StatusOK, "")
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	klines, err := c.GetDailyKLine(context.Background(), "2330")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if len(klines) != 5 {
		t.Errorf("預期回傳 5 筆，實際為 %d 筆", len(klines))
	}
}

func TestGetDailyKLine_EmptyStockID(t *testing.T) {
	c := NewFinMindClient()
	_, err := c.GetDailyKLine(context.Background(), "")
	if err == nil {
		t.Fatal("預期 stockID 為空時回傳錯誤")
	}
}

func TestGetDailyKLine_NonOKHTTPStatus(t *testing.T) {
	srv := newTestServer(t, 0, http.StatusInternalServerError, "internal error")
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	_, err := c.GetDailyKLine(context.Background(), "2330")
	if err == nil {
		t.Fatal("預期非 200 狀態碼時回傳錯誤")
	}
}

func TestGetDailyKLine_FinMindErrorStatus(t *testing.T) {
	srv := newTestServer(t, 0, http.StatusOK, `{"msg":"Requests reach the upper limit.","status":402,"data":[]}`)
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	_, err := c.GetDailyKLine(context.Background(), "2330")
	if err == nil {
		t.Fatal("預期 FinMind 回應 status != 200 時回傳錯誤")
	}
}

func TestGetDailyKLine_InvalidJSON(t *testing.T) {
	srv := newTestServer(t, 0, http.StatusOK, "不是合法 JSON")
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	_, err := c.GetDailyKLine(context.Background(), "2330")
	if err == nil {
		t.Fatal("預期回應無法解析時回傳錯誤")
	}
}

func TestGetDailyKLine_RequestParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"msg":"success","status":200,"data":[]}`))
	}))
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	_, err := c.GetDailyKLine(context.Background(), "2330")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if !strings.Contains(gotQuery, "dataset=TaiwanStockPrice") {
		t.Errorf("預期請求帶有 dataset=TaiwanStockPrice，實際 query 為 %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "data_id=2330") {
		t.Errorf("預期請求帶有 data_id=2330，實際 query 為 %q", gotQuery)
	}
	if strings.Contains(gotQuery, "token=") {
		t.Errorf("預期未設定 token 時請求不帶 token 參數，實際 query 為 %q", gotQuery)
	}
}

func TestGetDailyKLine_WithToken(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"msg":"success","status":200,"data":[]}`))
	}))
	defer srv.Close()

	c := newFinMindClient(srv.URL)
	c.token = "fake-token"
	_, err := c.GetDailyKLine(context.Background(), "2330")
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if !strings.Contains(gotQuery, "token=fake-token") {
		t.Errorf("預期請求帶有 token=fake-token，實際 query 為 %q", gotQuery)
	}
}

func TestFormatKLinesForPrompt_Empty(t *testing.T) {
	got := FormatKLinesForPrompt(nil)
	if got != "（查無資料）" {
		t.Errorf("預期查無資料訊息，實際為 %q", got)
	}
}

func TestFormatKLinesForPrompt_ContainsAllRows(t *testing.T) {
	klines := []DailyKLine{
		{Date: "2024-01-01", StockID: "2330", Open: 100, Max: 110, Min: 90, Close: 105, TradingVolume: 1000},
		{Date: "2024-01-02", StockID: "2330", Open: 105, Max: 115, Min: 95, Close: 110, TradingVolume: 2000},
	}
	got := FormatKLinesForPrompt(klines)
	if !strings.Contains(got, "2024-01-01") || !strings.Contains(got, "2024-01-02") {
		t.Errorf("預期輸出包含兩筆日期，實際為 %q", got)
	}
	if !strings.Contains(got, "2330") {
		t.Errorf("預期輸出包含股票代號，實際為 %q", got)
	}
}

// mockKLineFetcher 是測試用的 klineFetcher 實作，不打真實網路。
type mockKLineFetcher struct {
	calls      []string
	returnData []DailyKLine
	returnErr  error
}

func (f *mockKLineFetcher) GetDailyKLine(_ context.Context, stockID string) ([]DailyKLine, error) {
	f.calls = append(f.calls, stockID)
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.returnData, nil
}

func TestGetDailyKLineTool_Name(t *testing.T) {
	tool := &GetDailyKLineTool{fetcher: &mockKLineFetcher{}}
	if tool.Name() != "get_daily_kline" {
		t.Errorf("預期 Name() 為 get_daily_kline，實際為 %q", tool.Name())
	}
}

func TestGetDailyKLineTool_Description_ContainsNameAndArg(t *testing.T) {
	tool := &GetDailyKLineTool{fetcher: &mockKLineFetcher{}}
	desc := tool.Description()
	if !strings.Contains(desc, "get_daily_kline") {
		t.Errorf("預期 Description 包含工具名稱，實際為 %q", desc)
	}
	if !strings.Contains(desc, "stock_id") {
		t.Errorf("預期 Description 包含參數名稱 stock_id，實際為 %q", desc)
	}
}

func TestGetDailyKLineTool_Execute_Success(t *testing.T) {
	fetcher := &mockKLineFetcher{
		returnData: []DailyKLine{
			{Date: "2024-01-01", StockID: "2330", Open: 100, Max: 110, Min: 90, Close: 105, TradingVolume: 1000},
		},
	}
	tool := &GetDailyKLineTool{fetcher: fetcher}

	result, err := tool.Execute(context.Background(), map[string]string{"stock_id": "2330"})
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if !strings.Contains(result, "2330") || !strings.Contains(result, "2024-01-01") {
		t.Errorf("預期結果包含股票代號與日期，實際為 %q", result)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0] != "2330" {
		t.Errorf("預期 fetcher 被呼叫 1 次（stock_id=2330），實際為 %v", fetcher.calls)
	}
}

func TestGetDailyKLineTool_Execute_MissingStockID(t *testing.T) {
	fetcher := &mockKLineFetcher{}
	tool := &GetDailyKLineTool{fetcher: fetcher}

	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("預期缺少 stock_id 參數時回傳錯誤")
	}
	if len(fetcher.calls) != 0 {
		t.Errorf("預期 fetcher 未被呼叫，實際為 %v", fetcher.calls)
	}
}

func TestGetDailyKLineTool_Execute_FetcherError(t *testing.T) {
	fetcher := &mockKLineFetcher{returnErr: errors.New("network down")}
	tool := &GetDailyKLineTool{fetcher: fetcher}

	_, err := tool.Execute(context.Background(), map[string]string{"stock_id": "2330"})
	if err == nil {
		t.Fatal("預期 fetcher 回傳錯誤時 Execute 也回傳錯誤")
	}
}

func TestNewGetDailyKLineTool_WrapsFinMindClient(t *testing.T) {
	client := NewFinMindClient()
	tool := NewGetDailyKLineTool(client)
	if tool.fetcher != client {
		t.Error("預期 NewGetDailyKLineTool 內部的 fetcher 就是傳入的 FinMindClient")
	}
}
