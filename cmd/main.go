// Willionaire — 台股盤後分析 AI Agent Harness
//
// Day 1: 目前僅初始化 Execution Logger（log/slog），
// 驗證 Console + 本地 .log 檔案雙輸出是否正常運作。
package main

import (
	"log/slog"
	"os"

	"github.com/roly/willionaire/pkg/logger"
)

func main() {
	log, closeFn, err := logger.New(logger.Options{
		Dir:   "logs",
		Level: slog.LevelInfo,
	})
	if err != nil {
		panic(err)
	}
	defer func() {
		if cerr := closeFn(); cerr != nil {
			// Logger 已可能無法使用，改用標準錯誤輸出。
			_, _ = os.Stderr.WriteString("logger close error: " + cerr.Error() + "\n")
		}
	}()

	slog.SetDefault(log)

	slog.Info("willionaire agent harness 啟動",
		"stage", "day1",
		"maxSteps", 5,
	)
	slog.Info("Execution Logger 初始化完成，Console 與 .log 檔案雙輸出已就緒")
}
