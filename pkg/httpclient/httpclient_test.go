package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClient_Do_GETWithQueryAndHeaders(t *testing.T) {
	var gotMethod, gotQuery, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("X-Test-Header")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	c := New(5 * time.Second)
	resp, err := c.Do(context.Background(), Request{
		Method:  http.MethodGet,
		URL:     srv.URL,
		Query:   url.Values{"foo": []string{"bar"}},
		Headers: map[string]string{"X-Test-Header": "abc"},
	})
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("預期 method 為 GET，實際為 %q", gotMethod)
	}
	if !strings.Contains(gotQuery, "foo=bar") {
		t.Errorf("預期 query 帶有 foo=bar，實際為 %q", gotQuery)
	}
	if gotHeader != "abc" {
		t.Errorf("預期 header X-Test-Header 為 abc，實際為 %q", gotHeader)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("預期 StatusCode 為 200，實際為 %d", resp.StatusCode)
	}
	if string(resp.Body) != "hello" {
		t.Errorf("預期 Body 為 hello，實際為 %q", string(resp.Body))
	}
}

func TestClient_Do_POSTWithBody(t *testing.T) {
	var gotMethod, gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(5 * time.Second)
	resp, err := c.Do(context.Background(), Request{
		Method:  http.MethodPost,
		URL:     srv.URL,
		Body:    []byte(`{"key":"value"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	})
	if err != nil {
		t.Fatalf("預期成功，卻回傳錯誤: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("預期 method 為 POST，實際為 %q", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("預期 Content-Type 為 application/json，實際為 %q", gotContentType)
	}
	if gotBody != `{"key":"value"}` {
		t.Errorf("預期送出的 body 為 %q，實際為 %q", `{"key":"value"}`, gotBody)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("預期 StatusCode 為 201，實際為 %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("預期 Body 為 %q，實際為 %q", `{"ok":true}`, string(resp.Body))
	}
}

func TestClient_Do_NonOKStatusStillReturnsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := New(5 * time.Second)
	resp, err := c.Do(context.Background(), Request{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatalf("預期不因非 200 status 回傳 error，卻回傳: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("預期 StatusCode 為 500，實際為 %d", resp.StatusCode)
	}
	if string(resp.Body) != "boom" {
		t.Errorf("預期 Body 為 boom，實際為 %q", string(resp.Body))
	}
}

func TestClient_Do_ConnectionError(t *testing.T) {
	c := New(1 * time.Second)
	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, URL: "http://127.0.0.1:0"})
	if err == nil {
		t.Fatal("預期連線失敗時回傳錯誤")
	}
}

func TestClient_Do_InvalidMethod(t *testing.T) {
	c := New(1 * time.Second)
	_, err := c.Do(context.Background(), Request{Method: "IN VALID", URL: "http://example.com"})
	if err == nil {
		t.Fatal("預期建立請求失敗時回傳錯誤")
	}
}
