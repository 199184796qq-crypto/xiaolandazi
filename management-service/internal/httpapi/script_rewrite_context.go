package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/sharedfilelock"
)

type scriptRewriteContextScope struct {
	TenantID  int64  `json:"tenant_id"`
	PlanID    int64  `json:"plan_id"`
	SourceKey string `json:"source_key"`
	FactsKey  string `json:"facts_key"`
}

func newScriptRewriteContextScope(tenant, plan int64, sample, facts string) scriptRewriteContextScope {
	return scriptRewriteContextScope{TenantID: tenant, PlanID: plan,
		SourceKey: scriptRewriteContextFingerprint(sample),
		FactsKey:  scriptRewriteContextFingerprint(facts)}
}

func scriptRewriteContextFingerprint(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

func (s scriptRewriteContextScope) path(root string) string {
	return filepath.Join(root, "script-rewrite-contexts", strconv.FormatInt(s.TenantID, 10), strconv.FormatInt(s.PlanID, 10), s.SourceKey+"-"+s.FactsKey+".json")
}

type scriptRewriteContextRecord struct {
	Schema    int                       `json:"schema"`
	Scope     scriptRewriteContextScope `json:"scope"`
	Result    scriptRewriteOutput       `json:"result"`
	UpdatedAt string                    `json:"updated_at"`
}

type scriptRewriteContextLock struct {
	gate chan struct{}
	refs int
}

// Serialize read -> model response -> durable context for this source only.
// Audio synthesis happens after releasing this lock. Idle entries are removed.
var scriptRewriteContextLocks = struct {
	sync.Mutex
	entries map[string]*scriptRewriteContextLock
}{entries: map[string]*scriptRewriteContextLock{}}

func lockScriptRewriteContext(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scriptRewriteContextLocks.Lock()
	entry := scriptRewriteContextLocks.entries[path]
	if entry == nil {
		entry = &scriptRewriteContextLock{gate: make(chan struct{}, 1)}
		scriptRewriteContextLocks.entries[path] = entry
	}
	entry.refs++
	scriptRewriteContextLocks.Unlock()
	dropRef := func() {
		scriptRewriteContextLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(scriptRewriteContextLocks.entries, path)
		}
		scriptRewriteContextLocks.Unlock()
	}
	select {
	case entry.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-entry.gate
			dropRef()
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { <-entry.gate; dropRef() }) }, nil
	case <-ctx.Done():
		dropRef()
		return nil, ctx.Err()
	}
}

type scriptRewriteTurn struct {
	path     string
	scope    scriptRewriteContextScope
	Previous scriptRewriteOutput
	close    func()
}

func beginScriptRewriteTurn(ctx context.Context, root string, scope scriptRewriteContextScope) (*scriptRewriteTurn, error) {
	if scope.TenantID <= 0 || scope.PlanID <= 0 {
		return nil, errors.New("改写上下文缺少租户或方案")
	}
	path := scope.path(root)
	release, err := lockScriptRewriteContext(ctx, path)
	if err != nil {
		return nil, err
	}
	fileRelease, err := sharedfilelock.Lock(ctx, path+".lock")
	if err != nil {
		release()
		return nil, err
	}
	localRelease := release
	var closeOnce sync.Once
	release = func() { closeOnce.Do(func() { fileRelease(); localRelease() }) }
	turn := &scriptRewriteTurn{path: path, scope: scope, close: release}
	var record scriptRewriteContextRecord
	if err := readVolcengineJSON(path, &record); errors.Is(err, os.ErrNotExist) {
		return turn, nil
	} else if err != nil {
		release()
		return nil, fmt.Errorf("读取上次改写上下文失败: %w", err)
	}
	if record.Schema != 1 || record.Scope != scope || strings.TrimSpace(record.Result.Script) == "" {
		release()
		return nil, errors.New("改写上下文索引不匹配，已保留原文件")
	}
	turn.Previous = record.Result
	return turn, nil
}

func (turn *scriptRewriteTurn) Close() { turn.close() }

func (turn *scriptRewriteTurn) Save(result scriptRewriteOutput) error {
	if strings.TrimSpace(result.Script) == "" {
		return errors.New("空改写不能覆盖上次上下文")
	}
	// Also save legacy responses with an empty context: never label an older
	// summary as the summary of the latest response. No extra summary LLM call.
	return writeVolcengineJSON(turn.path, scriptRewriteContextRecord{Schema: 1, Scope: turn.scope,
		Result: result, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
}

func renderScriptRewriteRequest(prompt, sample, openings, openingKind string, previous scriptRewriteOutput) (string, error) {
	// Strip before injecting sample/history: those are literal data, not plan
	// sections. Enforce the program contract even for old or edited plans.
	prompt = planprompt.StripOutputFormatSection(prompt)
	if !strings.Contains(prompt, "{{样本话术}}") {
		prompt += "\n\n【样本话术】\n{{样本话术}}"
	}
	const placeholder = "{{近期写作记录}}"
	hasSlot := strings.Contains(prompt, placeholder)
	block := "（暂无上次写作记录）"
	if strings.TrimSpace(previous.Context) != "" {
		template, _, err := loadReplaceablePromptAsset("SCRIPT_REWRITE_PREVIOUS_CONTEXT_PATH", "configs/prompts/anchor-style/rewrite-previous-context.md", "上次话术写作记录模板")
		if err != nil {
			return "", err
		}
		// Substitute once; model-produced placeholder text remains literal data.
		block = strings.NewReplacer("{{上次骨架}}", previous.Skeleton, "{{上次上下文}}", previous.Context).Replace(template)
	}
	// All runtime values are inserted in one pass, so placeholders contained
	// inside a sample or previous metadata cannot rewrite each other.
	prompt = strings.NewReplacer("{{样本话术}}", strings.TrimSpace(sample),
		"{{已有开场}}", strings.TrimSpace(openings), "{{开场方式}}", strings.TrimSpace(openingKind),
		placeholder, block).Replace(prompt)
	if !hasSlot && strings.TrimSpace(previous.Context) != "" {
		prompt += "\n\n" + block
	}
	return strings.TrimSpace(prompt) + "\n\n" + planprompt.RewriteOutputContract, nil
}

func scriptRewriteMessages(prompt string) []agentgateway.Message {
	// The format is system-owned. Business rules, sample and previous model
	// metadata cannot turn it into editable user instructions.
	body := strings.TrimSpace(strings.TrimSuffix(prompt, planprompt.RewriteOutputContract))
	return []agentgateway.Message{
		{Role: "system", Content: planprompt.RewriteOutputContract},
		{Role: "user", Content: body},
	}
}
