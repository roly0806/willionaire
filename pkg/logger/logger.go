// Package logger 提供 Agent 執行過程的 Execution Tracing 能力。
//
// 設計原則（呼應 DESIGN.md）：
//   - 只用 Go 原生 log/slog，不引入第三方 logging 套件。
//   - 每次執行的 Trace 同時輸出到：
//     1) Terminal Console（即時觀察）
//     2) 本地 .log 檔案（事後追蹤 / 除錯）
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Options 控制 Logger 的行為。
type Options struct {
	// Dir 是 .log 檔案存放的目錄，預設為 "logs"。
	Dir string
	// Level 是最低輸出層級，預設為 slog.LevelInfo。
	Level slog.Level
}

// New 建立一個同時輸出到 Console 與本地 .log 檔案的 *slog.Logger。
//
// 回傳的 closeFn 應在程式結束前呼叫（例如透過 defer），
// 以確保 log 檔案被正確關閉、資料落盤。
func New(opts Options) (logger *slog.Logger, closeFn func() error, err error) {
	dir := opts.Dir
	if dir == "" {
		dir = "logs"
	}

	if err = os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("logger: 建立 log 目錄失敗: %w", err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("agent-%s.log", time.Now().Format("20060102-150405")))
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("logger: 開啟 log 檔案失敗: %w", err)
	}

	// 同時寫入 Console (Stdout) 與檔案。
	writer := io.MultiWriter(os.Stdout, file)

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: opts.Level,
	})

	logger = slog.New(handler)
	closeFn = file.Close

	return logger, closeFn, nil
}
