// Willionaire — 台股盤後分析 AI Agent Harness
//
// Day3：串接 Naive Agent Loop（pkg/workflow）、FinMind Tool（pkg/tools）
// 與 Claude ModelCaller（pkg/llm），並改用 CLI 參數接收使用者的初始問題。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/roly/willionaire/pkg/llm"
	"github.com/roly/willionaire/pkg/logger"
	"github.com/roly/willionaire/pkg/tools"
	"github.com/roly/willionaire/pkg/workflow"
)

// defaultQuestion 是沒有透過 CLI 參數提供問題時使用的預設問題，
// 方便在還沒設定好 API Key 之類條件時，仍能直接跑起整個流程做基本驗證。
const defaultQuestion = "請幫我分析台積電（2330）最近的股價走勢。"

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

	flag.Parse()
	question := defaultQuestion
	if args := flag.Args(); len(args) > 0 {
		question = strings.Join(args, " ")
	}

	slog.Info("willionaire agent harness 啟動",
		"stage", "day3",
		"maxSteps", workflow.MaxSteps,
		"question", question,
	)

	caller := llm.NewClaudeCallerFromEnv()
	finMind := tools.NewFinMindClient()
	availableTools := []workflow.Tool{tools.NewGetDailyKLineTool(finMind)}

	loop := workflow.NewLoop(caller, availableTools, log)
	prompt := workflow.BuildInitialPrompt(question, availableTools)

	out, err := loop.Run(context.Background(), prompt)
	if err != nil {
		slog.Error("agent loop 執行失敗", "error", err)
		os.Exit(1)
	}

	fmt.Println("=== Agent 最終輸出 ===")
	fmt.Printf("Status:  %s\n", out.Status)
	fmt.Printf("Thought: %s\n", out.Thought)
	if len(out.ToolCalls) > 0 {
		fmt.Printf("ToolCalls: %+v\n", out.ToolCalls)
	}
}
