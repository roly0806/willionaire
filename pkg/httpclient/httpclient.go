// Package httpclient 提供一個很輕量的 HTTP 傳輸層封裝。
//
// 設計取捨（呼應 Day3 code review 討論）：
//   - 只負責「建立請求 → 送出 → 讀取完整 body」這段傳輸層邏輯，統一給
//     pkg/tools（FinMind，GET + query params）與 pkg/llm（Claude，POST + JSON body
//   - 自訂 headers）共用，避免同樣的 http.NewRequestWithContext / Do / io.ReadAll
//     樣板程式碼在兩個套件各寫一份。
//   - 刻意不處理「HTTP status code 是否代表成功」或「body 該怎麼 unmarshal」——
//     不同 API 的成功/失敗判斷邏輯不一樣（例如 FinMind 用 body 內的 status 欄位，
//     Anthropic 用 HTTP status code + body 內的 error 欄位），這些業務判斷交給
//     呼叫方（pkg/tools、pkg/llm）自己處理，本套件只回傳原始 StatusCode 跟 Body。
//   - 只有在「連線層」真的失敗時（建立請求失敗、網路錯誤、讀取 body 失敗）才會
//     回傳 error；只要成功收到 HTTP 回應（不論 2xx/4xx/5xx），都會回傳 *Response。
package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client 是 httpclient 的用戶端，內部持有一個設定好 timeout 的 *http.Client。
type Client struct {
	httpClient *http.Client
}

// New 建立一個 Client，timeout 套用到底層的 *http.Client。
func New(timeout time.Duration) *Client {
	return &Client{httpClient: &http.Client{Timeout: timeout}}
}

// Request 描述一次 HTTP 呼叫的內容。
type Request struct {
	// Method 是 HTTP method，例如 http.MethodGet / http.MethodPost。
	Method string
	// URL 是不含 query string 的完整 URL。
	URL string
	// Query 是選填的 query string 參數，會被 Encode 後附加到 URL 後面。
	Query url.Values
	// Body 是選填的 raw request body（呼叫方自行 Marshal 好再傳進來）。
	Body []byte
	// Headers 是選填的額外 request headers。
	Headers map[string]string
}

// Response 是一次 HTTP 呼叫的原始回應。
type Response struct {
	StatusCode int
	Body       []byte
}

// Do 送出一次 HTTP 請求。
//
// 只要成功送出並收到回應（不論 StatusCode 是多少），就會回傳 *Response，
// 由呼叫方自行判斷 StatusCode、解析 Body。只有連線層失敗時才會回傳 error。
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	reqURL := req.URL
	if len(req.Query) > 0 {
		reqURL = reqURL + "?" + req.Query.Encode()
	}

	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = bytes.NewReader(req.Body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("httpclient: 建立請求失敗: %w", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("httpclient: 送出請求失敗: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("httpclient: 讀取回應失敗: %w", err)
	}

	return &Response{StatusCode: resp.StatusCode, Body: body}, nil
}
