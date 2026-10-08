package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Opt-in verification replays one explicitly scoped recorded failure. It does
// not save/publish plans or start playback. Actual correction calls and their
// original results are recorded through the normal model exchange mechanism.
func TestProductScopeRecordedProviderCorrection(t *testing.T) {
	if os.Getenv("ANCHOR_SCOPE_PROVIDER_PROBE") != "1" {
		t.Skip("explicit recorded provider probe required")
	}
	ids := make([]int64, 4)
	for i, key := range []string{"PROBE_TENANT_ID", "PROBE_PLAN_ID", "PROBE_ROOM_ID", "PROBE_EXCHANGE_ID"} {
		var err error
		ids[i], err = strconv.ParseInt(os.Getenv(key), 10, 64)
		if err != nil || ids[i] <= 0 {
			t.Fatal("explicit probe scope required")
		}
	}
	cfg := config.Load()
	dsn := mysql.NewConfig()
	dsn.User, dsn.Passwd, dsn.Net, dsn.Addr, dsn.DBName = cfg.DBUser, cfg.DBPassword, "tcp", net.JoinHostPort(cfg.DBHost, cfg.DBPort), cfg.DBName
	dsn.Timeout = 5 * time.Second
	database, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var rawRequest, rawResponse, profile string
	// Legacy save traces did not include room_id; an explicit exchange ID plus
	// tenant and plan is required even for that migration-only probe case.
	err = database.QueryRowContext(ctx, "SELECT request_json,raw_text,profile_id FROM live_model_exchanges WHERE tenant_id=? AND plan_id=? AND (room_id=? OR room_id=0) AND id=?", ids[0], ids[1], ids[2], ids[3]).Scan(&rawRequest, &rawResponse, &profile)
	if err != nil {
		t.Fatal("scoped recorded exchange unavailable")
	}
	var recorded agentgateway.Request
	if json.Unmarshal([]byte(rawRequest), &recorded) != nil {
		t.Fatal("recorded request malformed")
	}
	var input struct {
		Products []anchorcycle.FocusProduct `json:"products"`
		Facts    []cycleScopeFactInput      `json:"facts"`
		Tasks    []cycleScopeTaskInput      `json:"tasks"`
	}
	var original string
	for _, message := range recorded.Messages {
		if message.Role == "user" {
			original = message.Content
			break
		}
	}
	if json.Unmarshal([]byte(original), &input) != nil {
		t.Fatal("original inputs malformed")
	}
	scope := &anchorcycle.ProductFocus{Products: input.Products}
	var tasks []anchorcycle.Task
	for _, task := range input.Tasks {
		tasks = append(tasks, anchorcycle.Task{Key: fmt.Sprintf("probe-%d", task.ID), SellingPoint: task.SellingPoint, Angle: task.Angle})
	}
	if parseCycleProductScope(rawResponse, scope, input.Facts, tasks) == nil {
		t.Fatal("selected exchange is not a failed validation")
	}
	scope.Facts = nil
	scope.Tasks = nil
	prompt, _, err := loadCycleProductScopePrompt()
	if err != nil {
		t.Fatal("scope mother unavailable")
	}
	repair, _, err := loadReplaceablePromptAsset("ANCHOR_CYCLE_PRODUCT_SCOPE_REPAIR_PROMPT_PATH", "configs/prompts/anchor-style/cycle-product-scope-repair.md", "repair")
	if err != nil {
		t.Fatal("repair mother unavailable")
	}
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal("configured model store unavailable")
	}
	defer store.Close()
	server := &Server{store: store}
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: ids[0], PlanID: ids[1], RoomID: ids[2]})
	calls := 0
	err = validatedCycleProductScope(ctx, scope, input.Facts, tasks, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: original}}, repair, func(callCtx context.Context, stage string, messages []agentgateway.Message) (string, error) {
		calls++
		if calls == 1 {
			return rawResponse, nil
		}
		result, _, callErr := server.completeAnchorStyleWorkbench(callCtx, profile, stage+"_probe", agentgateway.ResponseJSON, messages, recorded.MaxTokens, false)
		return result.Text, callErr
	})
	if err != nil {
		t.Fatal("real provider correction failed; inspect scoped model exchange diagnostics")
	}
	t.Logf("real correction validated: facts=%d tasks=%d provider_calls=%d; no plan/publication/playback mutation", len(scope.Facts), len(scope.Tasks), calls-1)
}
