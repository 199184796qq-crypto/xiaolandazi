// reconcile-live-cost validates an exported supplier request invoice by default.
// It only changes the independent request ledger when --apply is explicit.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/livecost"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "账单核对未完成：", err)
		os.Exit(1)
	}
}
func run() error {
	path := flag.String("input", "", "供应商逐请求账单路径（json/csv）")
	format := flag.String("format", "", "json或csv，默认按扩展名")
	apply := flag.Bool("apply", false, "明确将账单核对写入请求费用账本；默认只校验文件且不连接数据库")
	flag.Parse()
	if strings.TrimSpace(*path) == "" {
		return fmt.Errorf("请指定账单文件")
	}
	file, err := os.Open(*path)
	if err != nil {
		return fmt.Errorf("无法打开账单文件")
	}
	defer file.Close()
	if *format == "" {
		*format = strings.TrimPrefix(strings.ToLower(filepath.Ext(*path)), ".")
	}
	invoices, err := livecost.ParseInvoices(file, *format)
	if err != nil {
		return err
	}
	if !*apply {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"模式": "仅校验，不连接数据库", "有效请求数": len(invoices)})
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		return fmt.Errorf("无法连接费用数据库")
	}
	defer store.Close()
	for index, invoice := range invoices {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = store.ReconcileLiveCostInvoice(ctx, invoice, time.Now().UTC())
		cancel()
		if err != nil {
			return fmt.Errorf("第%d条账单未完成核对：%w", index+1, err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"模式": "请求账单已核对", "有效请求数": len(invoices)})
}
