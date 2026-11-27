// Command fact-rerank-lab 是一个独立的本地测试页面：连接线上数据库，把某个方案
// 的事实库（live_agent_plan_material_claims）实时交给阿里 embedding 模型生成向量，
// 用问题向量做 cosine 召回，再把召回结果交给 qwen3.7-text-rerank 重新排序，
// 在同一页面对比两次排序。
//
// 它只读数据库，不写任何业务表，不启动 Management/Core 服务，也不影响线上。
// 典型用法见 scripts/start-fact-rerank-lab.ps1（自动拉取线上 DB 配置并建立 SSH 隧道）。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/danmakutest"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/rerank"
	"livecompanion/management/internal/semantic"
)

const (
	defaultAddr        = "127.0.0.1:8123"
	defaultEmbedModel  = "qwen3.7-text-embedding-flash"
	defaultEmbedBase   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultEmbedDims   = 1024
	embedBatchSize     = 20
	rerankCandidateCap = 30
	defaultTopK        = 20
	maxTopK            = 50
	defaultFeedSize    = 6
)

// docVersion 变化时强制重新向量化，避免旧缓存继续使用不含条件/例外的文本。
const docVersion = "material-document-v7"

// predicate 是 conditions_json / details_json.exceptions 里的一个条件。
type predicate struct {
	Op     string
	Raw    string
	Field  string
	Values []string
}

type fact struct {
	ID         int64    `json:"id"`
	ClaimKey   string   `json:"claim_key,omitempty"`
	Topic      string   `json:"topic"`
	Attribute  string   `json:"attribute,omitempty"` // 线上真实属性名（UI 展示的就是它）
	Content    string   `json:"content"`
	Quote      string   `json:"quote,omitempty"`
	ScopeKind  string   `json:"scope_kind,omitempty"`
	StoreName  string   `json:"store_name,omitempty"`
	LinkID     int64    `json:"link_id,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Scope      string   `json:"scope,omitempty"`  // 归属短标签：全局 / 门店 / 1号链接…
	Owners     []string `json:"owners,omitempty"` // 全部归属（一条事实可适用多个对象）
	Polarity   string   `json:"polarity,omitempty"`
	Conditions []string `json:"conditions,omitempty"` // 页面展示用：人类可读条件
	Exceptions []string `json:"exceptions,omitempty"`

	// 线上已存向量（embedding_json）。vector_source=stored 时直接使用。
	StoredVector   []float32 `json:"-"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`

	// 产品入库格式：每条 predicate 序列化后的 JSON，和 materialDocumentText 一致。
	conditionRaw []string
	exceptionRaw []string
	// 自然语言拼接用（doc_mode=sentence）。
	conditionPredicates []predicate
	exceptionPredicates []predicate
}

func (f fact) attributeName() string {
	return f.Attribute
}

// documentText 生成真正送进向量库的 doc。
//   - "lines"   ：线上同款分行格式（对象 / 主题 / 内容 / 条件 / 例外）
//   - "compact" ：对象+主题+内容合并成短语
//   - "sentence"：拼成一句自然语言，优先用原句摘要（source_quote），
//     条件用“如果…”接在前面，例如“1号链接如果要礼品包装需要额外添加5元”
func (f fact) documentText(mode string) string {
	switch mode {
	case "compact":
		return f.compactText()
	case "sentence":
		return f.sentenceText()
	default:
		return f.linesText()
	}
}

func (f fact) linesText() string {
	parts := []string{}
	if subject := strings.TrimSpace(f.Subject); subject != "" {
		parts = append(parts, subject)
	}
	if topic := strings.TrimSpace(f.Topic); topic != "" {
		parts = append(parts, topic)
	}
	if content := strings.TrimSpace(f.Content); content != "" {
		parts = append(parts, content)
	} else if quote := strings.TrimSpace(f.Quote); quote != "" {
		parts = append(parts, quote)
	}
	if f.Polarity == "negative" {
		parts = append(parts, "极性：否定")
	}
	for _, predicate := range f.conditionRaw {
		parts = append(parts, "条件："+predicate)
	}
	for _, predicate := range f.exceptionRaw {
		parts = append(parts, "例外："+predicate)
	}
	return strings.Join(parts, "\n")
}

func (f fact) compactText() string {
	parts := []string{}
	subject := strings.TrimSpace(f.Subject)
	topic := strings.TrimSpace(f.Topic)
	content := strings.TrimSpace(f.Content)
	if content == "" {
		content = strings.TrimSpace(f.Quote)
	}
	owner := ""
	if subject != "" && !strings.HasPrefix(subject, "全局") {
		owner = subject
	}
	head := owner + topic
	switch {
	case head == "":
	case content == "":
		parts = append(parts, head)
	case strings.HasPrefix(content, head), strings.HasPrefix(content, topic):
		parts = append(parts, content)
	default:
		parts = append(parts, head+"："+content)
	}
	return strings.Join(append(parts, f.ruleLines()...), "\n")
}

// sentenceText 是用户提出的“一条自然语言”拼法：
//
//	1号链接口味有麻辣和五香味两种口味
//	1号链接如果要礼品包装需要额外添加5元
//	全店通用如果收货地区是海南、新疆…发货方式只能发空运，通过京东空运发货
func (f fact) sentenceText() string {
	owner := strings.TrimSpace(f.Scope)
	if owner == "" || owner == "全局" {
		owner = "全店通用"
	}
	topic := strings.TrimSpace(f.Topic)
	// 有条件的回答类事实（包装方式/保存期限/加热方式等）条件必须显式出现在
	// 文本里，否则“抽真空吗”会命中“不抽真空（限川渝地区）”却看不出条件。
	phrase := conditionPhrase(f.conditionPredicates)
	body := strings.TrimSpace(f.Content)
	if phrase == "" {
		if quote := strings.TrimSpace(f.Quote); quote != "" {
			body = quote
		}
	}
	// 原句摘要里如果已经带了归属名，避免重复。
	body = strings.TrimSpace(strings.ReplaceAll(body, owner, ""))
	body = strings.TrimSpace(strings.ReplaceAll(body, "全店通用", ""))
	if body == "" {
		body = strings.TrimSpace(f.Quote)
	}
	head := owner
	if phrase != "" {
		head += phrase + "，"
	}
	if topic != "" && !strings.Contains(body, topic) && !strings.Contains(phrase, topic) {
		head += topic
	}
	doc := head + body
	if doc == owner {
		// 原句完全为空时退回 content。
		doc += strings.TrimSpace(f.Content)
	}
	if f.Polarity == "negative" && !strings.HasPrefix(body, "不") && !strings.Contains(doc, "不") {
		doc = strings.Replace(doc, topic, "不"+topic, 1)
	}
	if exceptions := conditionPhrase(f.exceptionPredicates); exceptions != "" && !mentionsCondition(body, f.exceptionPredicates) {
		doc += "；但" + exceptions + "除外"
	}
	return strings.TrimSpace(doc)
}

func conditionPhrase(predicates []predicate) string {
	parts := []string{}
	for _, predicate := range predicates {
		text := strings.TrimSpace(predicate.Raw)
		if predicate.Field != "" && len(predicate.Values) > 0 && (text == "" || !strings.Contains(text, predicate.Field)) {
			text = predicate.Field + "是" + strings.Join(predicate.Values, "、")
		}
		text = strings.TrimSpace(strings.TrimPrefix(text, "如果"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "若"))
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return ""
	}
	result := "如果" + parts[0]
	for _, part := range parts[1:] {
		result += "，且" + part
	}
	return result
}

// mentionsCondition 判断正文里是否已经写了条件，避免同一条件重复拼接。
func mentionsCondition(body string, predicates []predicate) bool {
	for _, predicate := range predicates {
		if raw := strings.TrimSpace(predicate.Raw); raw != "" && strings.Contains(body, raw) {
			return true
		}
		for _, value := range predicate.Values {
			if value != "" && strings.Contains(body, value) {
				return true
			}
		}
	}
	return false
}

func (f fact) ruleLines() []string {
	parts := []string{}
	if f.Polarity == "negative" {
		parts = append(parts, "极性：否定")
	}
	for _, predicate := range f.conditionRaw {
		parts = append(parts, "条件："+predicate)
	}
	for _, predicate := range f.exceptionRaw {
		parts = append(parts, "例外："+predicate)
	}
	return parts
}

// displayText 只用于页面展示，避免把 JSON 条件直接铺在正文里。
func (f fact) displayText() string {
	topic := strings.TrimSpace(f.Topic)
	content := strings.TrimSpace(f.Content)
	if content == "" {
		content = strings.TrimSpace(f.Quote)
	}
	if topic == "" || content == "" {
		return content
	}
	// 事实库里的 content 通常已经带主题前缀（例如“价格：9.9 元 3 斤”），
	// 这里不重复拼接，避免展示成“价格：价格：…”。
	if strings.HasPrefix(content, topic) {
		return content
	}
	return topic + "：" + content
}

// directory 是方案的对象目录（live_agent_plan_material_objects.directory_json），
// 事实的 object_targets 里只有 object_id，展示名（如“1号链接”）需要在这里查。
type directory struct {
	Objects    map[int64]string
	GlobalName string
}

func (l *lab) loadDirectory(ctx context.Context, tenantID, planID int64) *directory {
	var raw []byte
	err := l.db.QueryRowContext(ctx, `SELECT directory_json FROM live_agent_plan_material_objects WHERE tenant_id=? AND plan_id=?`, tenantID, planID).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var parsed struct {
		GlobalDisplayName string `json:"global_display_name"`
		Objects           []struct {
			ObjectID    int64  `json:"object_id"`
			DisplayName string `json:"display_name"`
		} `json:"objects"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	dir := &directory{Objects: map[int64]string{}, GlobalName: strings.TrimSpace(parsed.GlobalDisplayName)}
	for _, object := range parsed.Objects {
		if name := strings.TrimSpace(object.DisplayName); name != "" {
			dir.Objects[object.ObjectID] = name
		}
	}
	return dir
}

func (d *directory) name(objectID int64) string {
	if d != nil {
		if name, ok := d.Objects[objectID]; ok {
			return name
		}
	}
	return fmt.Sprintf("对象%d", objectID)
}

func subjectFromDetails(raw []byte, dir *directory) string {
	labels := targetLabels(raw, dir)
	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, "、")
}

// scopeLabel 返回归属短标签，用于页面显示和归属分布统计。
func scopeLabel(raw []byte, dir *directory) string {
	labels := targetLabels(raw, dir)
	if len(labels) == 0 {
		return "全局"
	}
	first := labels[0]
	if strings.HasPrefix(first, "全局") {
		return "全局"
	}
	return first
}

func targetLabels(raw []byte, dir *directory) []string {
	var details struct {
		ObjectTargets []struct {
			Kind      string `json:"kind"`
			ObjectID  int64  `json:"object_id"`
			LinkID    int64  `json:"link_id"`
			LinkKey   string `json:"link_key"`
			LinkNo    int64  `json:"link_no"`
			StoreName string `json:"store_name"`
		} `json:"object_targets"`
		ScopeName string `json:"scope_name"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &details) != nil {
		return nil
	}
	if scope := strings.TrimSpace(details.ScopeName); scope != "" && len(details.ObjectTargets) == 0 {
		return []string{scope}
	}
	labels := []string{}
	for _, target := range details.ObjectTargets {
		switch target.Kind {
		case "plan":
			labels = append(labels, "全局（本方案全部链接通用）")
		case "store":
			labels = append(labels, "门店："+target.StoreName)
		case "object":
			labels = append(labels, dir.name(target.ObjectID))
		default:
			switch {
			case target.LinkKey != "":
				key := strings.TrimSpace(target.LinkKey)
				if _, err := strconv.ParseInt(key, 10, 64); err == nil {
					labels = append(labels, key+"号链接")
				} else {
					labels = append(labels, key)
				}
			case target.LinkNo > 0:
				labels = append(labels, fmt.Sprintf("%d号链接", target.LinkNo))
			case target.LinkID > 0:
				labels = append(labels, fmt.Sprintf("链接%d", target.LinkID))
			}
		}
	}
	return labels
}

// predicateTexts 返回两套形式：人类可读条件（页面展示）和产品入库用的 JSON。
// parsePredicates 解析 conditions_json / details_json.exceptions。
func parsePredicates(raw []byte) []predicate {
	if len(raw) == 0 {
		return nil
	}
	var rows []any
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	out := []predicate{}
	for _, row := range rows {
		switch value := row.(type) {
		case string:
			out = append(out, predicate{Op: "raw", Raw: value})
		case map[string]any:
			item := predicate{}
			item.Op, _ = value["op"].(string)
			item.Raw, _ = value["raw"].(string)
			item.Field, _ = value["field"].(string)
			if list, ok := value["values"].([]any); ok {
				for _, entry := range list {
					if text, ok := entry.(string); ok {
						item.Values = append(item.Values, text)
					}
				}
			}
			out = append(out, item)
		}
	}
	return out
}

func predicatesReadable(predicates []predicate) []string {
	out := []string{}
	for _, predicate := range predicates {
		if text := predicateReadable(predicate); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func predicatesCompact(predicates []predicate) []string {
	out := []string{}
	for _, predicate := range predicates {
		encoded, err := json.Marshal(map[string]any{
			"op": predicate.Op, "raw": predicate.Raw, "field": predicate.Field, "values": predicate.Values,
		})
		if err != nil {
			continue
		}
		out = append(out, string(encoded))
	}
	return out
}

func predicateReadable(predicate predicate) string {
	if predicate.Field != "" && len(predicate.Values) > 0 {
		return predicate.Field + "：" + strings.Join(predicate.Values, "、")
	}
	if strings.TrimSpace(predicate.Raw) != "" {
		return predicate.Raw
	}
	encoded, _ := json.Marshal(predicate)
	return string(encoded)
}

func predicateTexts(raw []byte) (readable []string, compact []string) {
	predicates := parsePredicates(raw)
	return predicatesReadable(predicates), predicatesCompact(predicates)
}

type plan struct {
	ID       int64          `json:"id"`
	TenantID int64          `json:"tenant_id"`
	Name     string         `json:"name"`
	Status   string         `json:"status"`
	Facts    int            `json:"facts"`
	Scopes   map[string]int `json:"scope_counts,omitempty"`
}

// entry 是真正进入向量库的一条索引：一条事实按归属对象展开，每个对象一条，
// 全局事实展开成“每个对象一条”（例如“1号链接搭配主食当下酒菜”）。
type entry struct {
	fact   fact
	owner  string
	doc    string
	global bool // 原始归属是否为全局（展开的副本会标为 true）
}

type cachedPlan struct {
	plan                   plan
	facts                  []fact
	entries                []entry
	objectNames            []string
	globalObjectName       string
	roomIntro              string
	intentCtx              planIntentContext
	attributeResolver      danmakutest.IntentAttributeResolver
	attributeDirectory     danmakutest.IntentAttributeDirectory
	vectors                [][]float32
	model                  string
	docVersion             string
	mode                   string
	vectorSource           string
	vectorModel            string
	missingVectors         int
	attributeVectors       map[string][]float32
	attributeVectorsLoaded bool
	at                     time.Time
}

// loadRoomIntro 和线上 LiveAgentPlanRoomIntro 同一来源：最新非 prepared/abandoned 版本
// 的 generation_context_json.room_intro。意图和回答都必须带上它。
func (l *lab) loadRoomIntro(ctx context.Context, tenantID, planID int64) string {
	var intro string
	err := l.db.QueryRowContext(ctx, `
		SELECT CASE WHEN JSON_TYPE(JSON_EXTRACT(generation_context_json, '$.room_intro')) = 'STRING'
			THEN JSON_UNQUOTE(JSON_EXTRACT(generation_context_json, '$.room_intro')) ELSE '' END
		FROM live_agent_plan_versions
		WHERE tenant_id=? AND plan_id=? AND lifecycle_status NOT IN ('prepared','abandoned')
		  AND JSON_CONTAINS_PATH(generation_context_json, 'one', '$.room_intro')
		ORDER BY id DESC LIMIT 1`, tenantID, planID).Scan(&intro)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(intro)
}

type lab struct {
	db        *sql.DB
	embedBase string
	embedKey  string
	embedMod  string
	embedDims int
	chatMod   string
	chatBase  string
	chatKey   string
	reranker  *rerank.Client
	geo       *geoTable
	http      *http.Client

	mu       sync.Mutex
	cache    map[string]*cachedPlan
	lexicons map[int64]*danmakuLexicon
}

func main() {
	addr := flag.String("addr", defaultAddr, "监听地址")
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("LAB_DSN"))
	if dsn == "" {
		dsn = dsnFromEnv()
	}
	if dsn == "" {
		log.Fatal("缺少数据库配置：设置 DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASSWORD 或 LAB_DSN")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Fatalf("ping mysql: %v", err)
	}

	rerankConfig := rerank.ConfigFromEnv()
	instance := &lab{
		db:        db,
		embedBase: embeddingBaseURL(),
		embedKey:  rerankConfig.APIKey,
		embedMod:  embeddingModel(),
		embedDims: embeddingDimensions(),
		chatMod:   chatModel(),
		geo:       loadGeoTable(),
		reranker:  rerank.New(rerankConfig),
		http:      &http.Client{Timeout: 30 * time.Second},
		cache:     map[string]*cachedPlan{},
	}
	if instance.embedKey == "" {
		log.Fatal("缺少 DASHSCOPE_API_KEY，无法调用 embedding 模型")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", instance.handlePage)
	mux.HandleFunc("GET /api/plans", instance.handlePlans)
	mux.HandleFunc("POST /api/recall", instance.handleRecall)
	mux.HandleFunc("POST /api/refresh", instance.handleRefresh)
	mux.HandleFunc("POST /api/pipeline", instance.handlePipeline)
	mux.HandleFunc("POST /api/intent", instance.handleIntent)
	mux.HandleFunc("POST /api/recall-asks", instance.handleRecallAsks)
	mux.HandleFunc("POST /api/answer-facts", instance.handleAnswerFacts)
	mux.HandleFunc("POST /api/correct", instance.handleCorrect)
	mux.HandleFunc("GET /api/danmaku", instance.handleDanmaku)

	// 优先使用线上模型池里启用的模型（当前是 deepseek4.1flash / deepseek-flash），
	// 密钥从 mgmt_model_pool_entries.sealed_key 解密；失败再退回 DASHSCOPE 默认配置。
	providerCtx, cancelProvider := context.WithTimeout(context.Background(), 10*time.Second)
	instance.loadChatProvider(providerCtx)
	cancelProvider()

	log.Printf("fact-rerank-lab listening on http://%s (embed=%s chat=%s rerank_enabled=%v rerank_model=%s)",
		*addr, instance.embedMod, instance.chatMod, instance.reranker.Enabled(), instance.reranker.Model())
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func (l *lab) chatURL() string {
	if base := strings.TrimRight(strings.TrimSpace(l.chatBase), "/"); base != "" {
		return base
	}
	return strings.TrimRight(l.embedBase, "/")
}

func (l *lab) chatAPIKey() string {
	if key := strings.TrimSpace(l.chatKey); key != "" {
		return key
	}
	return l.embedKey
}

// loadChatProvider 从线上模型池读取启用中的聊天模型（如 deepseek-flash），
// 解密 sealed_key 后作为意图/回答模型；失败保持 DASHSCOPE 默认。
func (l *lab) loadChatProvider(ctx context.Context) {
	rows, err := l.db.QueryContext(ctx, `SELECT id, account_name, model, profile_json, sealed_key
		FROM mgmt_model_pool_entries WHERE enabled=1 ORDER BY created_at, id`)
	if err != nil {
		log.Printf("model pool unavailable, keep default chat model: %v", err)
		return
	}
	defer rows.Close()
	type candidate struct {
		id, account, model string
		profile            []byte
		sealed             []byte
	}
	var chosen *candidate
	for rows.Next() {
		var id, account, model string
		var profile, sealed []byte
		if err := rows.Scan(&id, &account, &model, &profile, &sealed); err != nil {
			continue
		}
		item := candidate{id: id, account: account, model: model, profile: profile, sealed: sealed}
		if chosen == nil {
			chosen = &item
		}
		if strings.Contains(strings.ToLower(account), "deepseek") || strings.Contains(strings.ToLower(model), "deepseek") {
			chosen = &item
			break
		}
	}
	if chosen == nil {
		return
	}
	var profile agentgateway.SpeechModel
	_ = json.Unmarshal(chosen.profile, &profile)
	base := strings.TrimRight(strings.TrimSpace(profile.BaseURL), "/")
	model := strings.TrimSpace(profile.Model)
	if model == "" {
		model = chosen.model
	}
	key, err := agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), chosen.id, chosen.sealed)
	if err != nil || strings.TrimSpace(key) == "" {
		log.Printf("model pool key open failed, keep default chat model: %v", err)
		return
	}
	l.chatBase = base
	l.chatMod = model
	l.chatKey = key
	suffix := ""
	if len(key) > 4 {
		suffix = "…" + key[len(key)-4:]
	}
	log.Printf("chat provider from model pool: account=%s model=%s base=%s key_len=%d%s",
		chosen.account, model, base, len(key), suffix)
}

func dsnFromEnv() string {
	host := strings.TrimSpace(os.Getenv("DB_HOST"))
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv("DB_PORT"))
	if port == "" {
		port = "3306"
	}
	config := mysqldriver.NewConfig()
	config.User = strings.TrimSpace(os.Getenv("DB_USER"))
	config.Passwd = os.Getenv("DB_PASSWORD")
	config.Net = "tcp"
	config.Addr = host + ":" + port
	config.DBName = strings.TrimSpace(os.Getenv("DB_NAME"))
	config.ParseTime = true
	config.Timeout = 5 * time.Second
	config.ReadTimeout = 60 * time.Second
	config.WriteTimeout = 15 * time.Second
	return config.FormatDSN()
}

func embeddingBaseURL() string {
	for _, name := range []string{"DASHSCOPE_EMBEDDING_BASE_URL", "DASHSCOPE_BASE_URL"} {
		if value := strings.TrimRight(strings.TrimSpace(os.Getenv(name)), "/"); value != "" {
			return value
		}
	}
	return defaultEmbedBase
}

func embeddingModel() string {
	if value := strings.TrimSpace(os.Getenv("DASHSCOPE_EMBEDDING_MODEL")); value != "" {
		return value
	}
	return defaultEmbedModel
}

func embeddingDimensions() int {
	raw := strings.TrimSpace(os.Getenv("DASHSCOPE_EMBEDDING_DIMENSIONS"))
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err == nil && value > 0 {
		return value
	}
	return defaultEmbedDims
}

// chatModel 是意图/回答阶段使用的大模型，默认与弹幕测试一致。
func chatModel() string {
	if value := strings.TrimSpace(os.Getenv("DASHSCOPE_CHAT_MODEL")); value != "" {
		return value
	}
	return "qwen3.7-flash"
}

func (l *lab) plans(ctx context.Context) ([]plan, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id, tenant_id, name, status FROM live_agent_plans ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []plan{}
	index := map[int64]int{}
	for rows.Next() {
		var item plan
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Status); err != nil {
			return nil, err
		}
		index[item.ID] = len(items)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts, err := l.db.QueryContext(ctx, `SELECT plan_id, scope_kind, IFNULL(product_link_id,0), COUNT(*)
		FROM live_agent_plan_material_claims WHERE status='active' GROUP BY plan_id, scope_kind, product_link_id`)
	if err != nil {
		return nil, err
	}
	defer counts.Close()
	for counts.Next() {
		var planID, linkID int64
		var scopeKind string
		var count int
		if err := counts.Scan(&planID, &scopeKind, &linkID, &count); err != nil {
			return nil, err
		}
		position, ok := index[planID]
		if !ok {
			continue
		}
		items[position].Facts += count
		if items[position].Scopes == nil {
			items[position].Scopes = map[string]int{}
		}
		switch {
		case scopeKind == "plan":
			items[position].Scopes["全局"] += count
		case scopeKind == "store":
			items[position].Scopes["门店"] += count
		case linkID > 0 || scopeKind == "product":
			items[position].Scopes["链接"] += count
		default:
			items[position].Scopes["对象"] += count
		}
	}
	return items, counts.Err()
}

func (l *lab) loadFacts(ctx context.Context, target plan) ([]fact, []string, error) {
	dir := l.loadDirectory(ctx, target.TenantID, target.ID)
	rows, err := l.db.QueryContext(ctx, `SELECT id, claim_key, topic, IFNULL(attribute,''), content, IFNULL(source_quote,''), scope_kind, IFNULL(store_name,''), IFNULL(product_link_id,0),
		polarity, COALESCE(conditions_json, JSON_ARRAY()), COALESCE(details_json, JSON_OBJECT()),
		IFNULL(embedding_model,''), IFNULL(CAST(embedding_json AS CHAR),'')
		FROM live_agent_plan_material_claims
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY id`, target.TenantID, target.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	facts := []fact{}
	for rows.Next() {
		var item fact
		var conditions, details []byte
		var storedRaw string
		if err := rows.Scan(&item.ID, &item.ClaimKey, &item.Topic, &item.Attribute, &item.Content, &item.Quote, &item.ScopeKind, &item.StoreName, &item.LinkID,
			&item.Polarity, &conditions, &details, &item.EmbeddingModel, &storedRaw); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(storedRaw) != "" {
			_ = json.Unmarshal([]byte(storedRaw), &item.StoredVector)
		}
		item.Subject = subjectFromDetails(details, dir)
		item.Scope = scopeLabel(details, dir)
		for _, label := range targetLabels(details, dir) {
			if strings.HasPrefix(label, "全局") {
				item.Owners = append(item.Owners, "全局")
			} else {
				item.Owners = append(item.Owners, label)
			}
		}
		if len(item.Owners) == 0 {
			item.Owners = []string{"全局"}
		}
		item.conditionPredicates = parsePredicates(conditions)
		item.Conditions = predicatesReadable(item.conditionPredicates)
		item.conditionRaw = predicatesCompact(item.conditionPredicates)
		if len(details) > 0 {
			var parsed struct {
				Exceptions json.RawMessage `json:"exceptions"`
			}
			if json.Unmarshal(details, &parsed) == nil {
				item.exceptionPredicates = parsePredicates(parsed.Exceptions)
				item.Exceptions = predicatesReadable(item.exceptionPredicates)
				item.exceptionRaw = predicatesCompact(item.exceptionPredicates)
			}
		}
		if strings.TrimSpace(item.documentText("lines")) == "" {
			continue
		}
		facts = append(facts, item)
	}
	return facts, dirObjectNames(dir), rows.Err()
}

func dirObjectNames(dir *directory) []string {
	if dir == nil || len(dir.Objects) == 0 {
		return nil
	}
	names := make([]string, 0, len(dir.Objects))
	seen := map[string]bool{}
	for _, name := range dir.Objects {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (l *lab) dropCache(planID int64) {
	prefix := fmt.Sprintf("%d|", planID)
	l.mu.Lock()
	defer l.mu.Unlock()
	for key := range l.cache {
		if strings.HasPrefix(key, prefix) {
			delete(l.cache, key)
		}
	}
}

func dirGlobalName(dir *directory) string {
	if dir == nil {
		return ""
	}
	return strings.TrimSpace(dir.GlobalName)
}

func (l *lab) cachedFacts(ctx context.Context, target plan, mode, vectorSource string) (*cachedPlan, error) {
	if vectorSource == "" {
		vectorSource = "stored"
	}
	if vectorSource != "stored" {
		vectorSource = "live"
	}
	cacheKey := fmt.Sprintf("%d|%s|%s", target.ID, mode, vectorSource)
	l.mu.Lock()
	cached := l.cache[cacheKey]
	l.mu.Unlock()
	if cached != nil && cached.model == l.embedMod && cached.docVersion == docVersion && cached.mode == mode && cached.vectorSource == vectorSource {
		return cached, nil
	}
	started := time.Now()
	dir := l.loadDirectory(ctx, target.TenantID, target.ID)
	facts, objectNames, err := l.loadFacts(ctx, target)
	if err != nil {
		return nil, err
	}
	intentCtx := l.loadIntentContext(ctx, target.TenantID, target.ID)
	globalName := dirGlobalName(dir)
	resolver, attributeDirectory, err := l.intentAttributeDirectory(ctx, target, facts, intentCtx, globalName)
	if err != nil {
		return nil, fmt.Errorf("读取已确认的事实属性: %w", err)
	}
	entries := []entry{}
	for _, item := range facts {
		owners := item.Owners
		isGlobal := len(owners) == 1 && owners[0] == "全局"
		if isGlobal {
			// 全局事实保留一条“全店通用”主索引；没点名对象的查询只用这条，
			// 避免展开副本把任意对象名（1号链接/门店）混进全局召回。
			canonical := item
			canonical.Subject = "全店通用"
			canonical.Scope = "全店通用"
			canonical.Owners = []string{"全店通用"}
			entries = append(entries, entry{fact: canonical, owner: "全店通用", doc: canonical.documentText(mode), global: true})
		}
		// 全局事实按“每个对象各一条”展开；没有对象目录时只保留主索引。
		if isGlobal {
			if len(objectNames) == 0 {
				continue
			}
			owners = objectNames
		}
		seen := map[string]bool{}
		for _, owner := range owners {
			if owner == "" || seen[owner] {
				continue
			}
			seen[owner] = true
			expanded := item
			expanded.Subject = owner
			expanded.Scope = owner
			expanded.Owners = []string{owner}
			entries = append(entries, entry{fact: expanded, owner: owner, doc: expanded.documentText(mode), global: isGlobal})
		}
	}
	vectors := make([][]float32, len(entries))
	missing := 0
	if vectorSource == "stored" {
		for index := range entries {
			if stored := entries[index].fact.StoredVector; len(stored) > 0 {
				vectors[index] = stored
			} else {
				missing++
			}
		}
	} else {
		texts := make([]string, len(entries))
		for index := range entries {
			texts[index] = entries[index].doc
		}
		vectors, err = l.embed(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("事实库向量化失败: %w", err)
		}
	}
	vectorModel := ""
	for _, item := range facts {
		if item.EmbeddingModel != "" {
			vectorModel = item.EmbeddingModel
			break
		}
	}
	l.mu.Lock()
	l.cache[cacheKey] = &cachedPlan{plan: target, facts: facts, entries: entries, objectNames: objectNames,
		globalObjectName: globalName,
		roomIntro:        l.loadRoomIntro(ctx, target.TenantID, target.ID),
		intentCtx:        intentCtx, attributeResolver: resolver, attributeDirectory: attributeDirectory,
		vectors: vectors, model: l.embedMod, docVersion: docVersion, mode: mode,
		vectorSource: vectorSource, vectorModel: vectorModel, missingVectors: missing, at: started}
	l.mu.Unlock()
	log.Printf("vectors plan=%d source=%s facts=%d entries=%d missing=%d model=%s elapsed=%s", target.ID, vectorSource, len(facts), len(entries), missing, vectorModel, time.Since(started).Round(time.Millisecond))
	return l.cache[cacheKey], nil
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (l *lab) embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := l.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		copy(out[start:end], vectors)
	}
	return out, nil
}

func (l *lab) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, err := json.Marshal(map[string]any{
		"model":           l.embedMod,
		"input":           texts,
		"dimensions":      l.embedDims,
		"encoding_format": "float",
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, l.embedBase+"/embeddings", strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+l.embedKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := l.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var decoded embeddingResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := http.StatusText(response.StatusCode)
		if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
			message = decoded.Error.Message
		}
		return nil, fmt.Errorf("embedded http %d: %s", response.StatusCode, message)
	}
	if len(decoded.Data) != len(texts) {
		return nil, fmt.Errorf("embedding data=%d want=%d", len(decoded.Data), len(texts))
	}
	vectors := make([][]float32, len(texts))
	for position, item := range decoded.Data {
		if len(item.Embedding) != l.embedDims {
			return nil, fmt.Errorf("embedding dimensions=%d want=%d", len(item.Embedding), l.embedDims)
		}
		vectors[position] = item.Embedding
	}
	return vectors, nil
}

type recallRequest struct {
	PlanID   int64  `json:"plan_id"`
	Question string `json:"question"`
	TopK     int    `json:"top_k"`
	Refresh  bool   `json:"refresh"`
	// DocMode="compact" 时把“对象+主题+内容”拼成自然短语；默认按线上分行格式。
	DocMode string `json:"doc_mode"`
	// VectorSource="stored" 用线上 embedding_json；"live" 现场重新生成。
	VectorSource string `json:"vector_source"`
}

type candidate struct {
	Rank        int      `json:"rank"`
	ID          int64    `json:"id"`
	Topic       string   `json:"topic"`
	Text        string   `json:"text"`
	Subject     string   `json:"subject,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Conditions  []string `json:"conditions,omitempty"`
	Exceptions  []string `json:"exceptions,omitempty"`
	Cos         float64  `json:"cosine_score"`
	RerankScore float64  `json:"rerank_score,omitempty"`
	CosRank     int      `json:"cosine_rank"`
	Delta       int      `json:"delta,omitempty"`
	// OriginGlobal 表示这条事实原始归属是“全局”，只是被展开到对象下。
	OriginGlobal bool `json:"origin_global,omitempty"`
	// Conditional 表示这条事实带条件/例外，问题里若没确认条件，回答必须带条件说明。
	Conditional bool `json:"conditional,omitempty"`
	// Doc 是真正送去 embedding/rerank 的入库文本（对象+主题+内容+条件+例外）。
	Doc string `json:"doc"`
}

type recallResponse struct {
	Plan           plan               `json:"plan"`
	Question       string             `json:"question"`
	EmbedModel     string             `json:"embedding_model"`
	FactCount      int                `json:"fact_count"`
	EntryCount     int                `json:"entry_count"`
	TopK           int                `json:"top_k"`
	DocMode        string             `json:"doc_mode"`
	VectorSource   string             `json:"vector_source"`
	VectorModel    string             `json:"vector_model"`
	MissingVectors int                `json:"missing_vectors"`
	Recall         []candidate        `json:"recall"`
	Rerank         []candidate        `json:"rerank"`
	RerankModel    string             `json:"rerank_model"`
	RerankError    string             `json:"rerank_error,omitempty"`
	Feed           []candidate        `json:"feed"`
	FeedSize       int                `json:"feed_size"`
	Timings        map[string]float64 `json:"timings_ms"`
}

func (l *lab) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	l.dropCache(request.PlanID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (l *lab) planByID(ctx context.Context, planID int64) (plan, error) {
	items, err := l.plans(ctx)
	if err != nil {
		return plan{}, err
	}
	for _, item := range items {
		if item.ID == planID {
			return item, nil
		}
	}
	return plan{}, errors.New("方案不存在")
}

func (l *lab) handlePlans(w http.ResponseWriter, r *http.Request) {
	items, err := l.plans(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":           items,
		"embedding_model": l.embedMod,
		"rerank_model":    l.reranker.Model(),
		"rerank_enabled":  l.reranker.Enabled(),
	})
}

type recallAskItem struct {
	ID           string   `json:"id"`
	Query        string   `json:"query"`
	Attribute    string   `json:"attribute"`
	Attributes   []string `json:"attributes"`
	Operation    string   `json:"operation"`
	AttrScope    string   `json:"attribute_scope"`
	Subjects     []string `json:"subjects"`
	CompareGroup string   `json:"compare_group"`
}

type objectFeed struct {
	Object string      `json:"object"`
	Count  int         `json:"count"`
	Feed   []candidate `json:"feed"`
}

type askRecallResult struct {
	ID            string       `json:"id"`
	Attribute     string       `json:"attribute"`
	Attributes    []string     `json:"attributes"`
	Query         string       `json:"query"`
	Subjects      []string     `json:"subjects,omitempty"`
	CompareGroup  string       `json:"compare_group,omitempty"`
	Full          bool         `json:"full"`
	FeedCount     int          `json:"feed_count"`
	Feed          []candidate  `json:"feed"`
	ObjectFeeds   []objectFeed `json:"object_feeds,omitempty"`
	Recall        []candidate  `json:"recall"`
	Rerank        []candidate  `json:"rerank"`
	FallbackNote  string       `json:"fallback_note,omitempty"`
	StepBackQuery string       `json:"step_back_query,omitempty"`
	Error         string       `json:"error,omitempty"`
	ElapsedMS     float64      `json:"elapsed_ms"`
}

type mergedEvidence struct {
	ID          int64    `json:"id"`
	Topic       string   `json:"topic"`
	Text        string   `json:"text"`
	Subject     string   `json:"subject,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Conditions  []string `json:"conditions,omitempty"`
	Exceptions  []string `json:"exceptions,omitempty"`
	BestScore   float64  `json:"best_score"`
	CosineMax   float64  `json:"cosine_max"`
	RerankMax   float64  `json:"rerank_max,omitempty"`
	HitAsks     []string `json:"hit_asks"`
	Conditional bool     `json:"conditional,omitempty"`
}

type recallAsksResponse struct {
	Question       string            `json:"question"`
	Asks           []askRecallResult `json:"asks"`
	Shared         []candidate       `json:"shared"`
	Merged         []mergedEvidence  `json:"merged"`
	DedupeBefore   int               `json:"dedupe_before"`
	DedupeAfter    int               `json:"dedupe_after"`
	EmbedModel     string            `json:"embed_model"`
	RerankModel    string            `json:"rerank_model"`
	VectorSource   string            `json:"vector_source"`
	VectorModel    string            `json:"vector_model"`
	MissingVectors int               `json:"missing_vectors"`
}

// handleRecallAsks 按意图拆出的多个 ask 分别召回、分别精排，
// 再把同一 fact 去重合并成一份证据（保留最高分，记录哪些 ask 命中）。
func (l *lab) handleRecallAsks(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID       int64           `json:"plan_id"`
		Question     string          `json:"question"`
		Asks         []recallAskItem `json:"asks"`
		TopK         int             `json:"top_k"`
		DocMode      string          `json:"doc_mode"`
		VectorSource string          `json:"vector_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	if len(request.Asks) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有可召回的 ask"})
		return
	}
	if request.TopK <= 0 {
		request.TopK = defaultTopK
	}
	if request.TopK > maxTopK {
		request.TopK = maxTopK
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	mode := strings.TrimSpace(request.DocMode)
	if mode == "" {
		mode = "sentence"
	}
	cached, err := l.cachedFacts(r.Context(), target, mode, request.VectorSource)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	response := recallAsksResponse{
		Question:       request.Question,
		Asks:           []askRecallResult{},
		Merged:         []mergedEvidence{},
		EmbedModel:     l.embedMod,
		RerankModel:    l.reranker.Model(),
		VectorSource:   cached.vectorSource,
		VectorModel:    cached.vectorModel,
		MissingVectors: cached.missingVectors,
	}
	type mergedState struct {
		item mergedEvidence
		seen map[string]bool
	}
	mergedByID := map[int64]*mergedState{}
	for _, ask := range request.Asks {
		query := strings.TrimSpace(ask.Query)
		if query == "" {
			query = strings.TrimSpace(ask.ID)
		}
		if query == "" {
			continue
		}
		started := time.Now()
		var evidence []candidate
		var recallErr error
		askAttributes := cleanStrings(ask.Attributes)
		if len(askAttributes) == 0 && strings.TrimSpace(ask.Attribute) != "" && strings.TrimSpace(ask.Attribute) != "其他" {
			askAttributes = []string{strings.TrimSpace(ask.Attribute)}
		}
		// 属性扩展：模型给的属性和目录里相近但不同名时（价格↔价格规格/折算单价、配送↔是否邮寄），
		// 先扩成一组近义属性再过滤，避免只命中其中一种写法。
		attrNote := ""
		if len(askAttributes) > 0 {
			expanded, notes := l.normalizeAttributes(r.Context(), cached, askAttributes)
			if len(notes) > 0 {
				askAttributes = expanded
				attrNote = "属性扩展：" + strings.Join(notes, "、")
			}
			// 用问题本身找最近属性，最多补 3 个；扩展结果可审计。
			queryExpanded, queryNotes := l.expandAttributesFromQuery(r.Context(), cached, query, askAttributes)
			if len(queryNotes) > 0 {
				askAttributes = queryExpanded
				prefix := "问题扩展：" + strings.Join(queryNotes, "、")
				if attrNote != "" {
					attrNote += "；" + prefix
				} else {
					attrNote = prefix
				}
			}
		}
		fullFeed := len(askAttributes) == 0
		// 规则：有属性约束才按属性过滤/截取；没有属性才取该对象全量。
		// 对象可为空：无对象时用属性本身做过滤（例如“上海可以买吗”只取购买方式/门店/配送类事实）。
		compareAsk := strings.TrimSpace(ask.CompareGroup) != ""
		// 对比类且没有点名具体属性（scope=全部）：覆盖该对象的全部事实；
		// 只有明确点名属性（如“3号和5号价格区别”）才按属性过滤。
		compareFull := compareAsk && strings.TrimSpace(ask.AttrScope) == "全部"
		subject := ""
		if len(ask.Subjects) == 1 {
			subject = strings.TrimSpace(ask.Subjects[0])
		}
		var objectFeeds []objectFeed
		switch {
		case subject != "" && (fullFeed || compareFull):
			evidence, recallErr = l.allObjectFacts(r.Context(), cached, query, subject, false)
		case subject == "" && len(askAttributes) > 0:
			// 没点名对象：公共（全店通用）事实给一份，然后每个对象各召回一次再叠加。
			var shared []candidate
			if sharedFacts, sharedErr := l.globalByAttributes(r.Context(), cached, query, askAttributes); sharedErr == nil {
				shared = sharedFacts
				response.Shared = shared
			}
			// 每个对象按属性各召回一次，再叠加。
			perObject := map[string][]candidate{}
			for _, object := range cached.objectNames {
				object = strings.TrimSpace(object)
				if object == "" {
					continue
				}
				feeds, err := l.recallByAttributes(r.Context(), cached, query, object, askAttributes, request.TopK, false)
				if err != nil {
					recallErr = err
					break
				}
				perObject[object] = feeds
			}
			// 第二遍：按对象叠加；数据按属性归一后，属性召回本身就能带出对象例外，
			// 不再额外做相似度扫描，避免把价格/口感等噪声带进来。
			for _, object := range cached.objectNames {
				object = strings.TrimSpace(object)
				if object == "" {
					continue
				}
				feeds := append([]candidate{}, perObject[object]...)
				objectFeeds = append(objectFeeds, objectFeed{Object: object, Count: len(feeds), Feed: feeds})
				evidence = append(evidence, feeds...)
			}
		case subject == "":
			// 没有对象也没有属性：公共事实一份 + 每个对象全量。
			if shared, sharedErr := l.globalFacts(r.Context(), cached, query); sharedErr == nil {
				response.Shared = shared
			}
			for _, object := range cached.objectNames {
				object = strings.TrimSpace(object)
				if object == "" {
					continue
				}
				feeds, err := l.allObjectFacts(r.Context(), cached, query, object, false)
				if err != nil {
					recallErr = err
					break
				}
				objectFeeds = append(objectFeeds, objectFeed{Object: object, Count: len(feeds), Feed: feeds})
				evidence = append(evidence, feeds...)
			}
		case len(askAttributes) > 0:
			evidence, recallErr = l.recallByAttributes(r.Context(), cached, query, subject, askAttributes, request.TopK, true)
		case compareAsk:
			evidence, recallErr = l.recallObjectFacts(r.Context(), cached, query, subject, request.TopK)
		default:
			evidence, recallErr = l.allObjectFacts(r.Context(), cached, query, subject, true)
		}
		item := askRecallResult{
			ID: ask.ID, Attribute: ask.Attribute, Attributes: ask.Attributes, Query: query,
			Subjects: ask.Subjects, CompareGroup: ask.CompareGroup,
			Full: fullFeed || compareFull, Feed: []candidate{}, Recall: []candidate{}, Rerank: []candidate{},
			ObjectFeeds:  objectFeeds,
			FallbackNote: attrNote,
			ElapsedMS:    float64(time.Since(started).Microseconds()) / 1000,
		}
		item.Attributes = askAttributes
		if recallErr != nil {
			item.Error = recallErr.Error()
		} else {
			// 兜底 1：属性归一（模型给的属性不在目录时，embedding 找最近属性重试）。
			// 兜底 2：仍为空时 step-back 改写问题，去掉属性限制再召回一次。
			if len(evidence) == 0 && len(askAttributes) > 0 && attrNote == "" {
				if normalized, changed := l.normalizeAttributes(r.Context(), cached, askAttributes); len(changed) > 0 {
					if subject != "" {
						if retry, retryErr := l.recallByAttributes(r.Context(), cached, query, subject, normalized, request.TopK, true); retryErr == nil && len(retry) > 0 {
							evidence = retry
							askAttributes = normalized
							item.Attributes = normalized
							item.FallbackNote = "属性归一：" + strings.Join(changed, "、")
						}
					} else {
						var newFeeds []objectFeed
						var all []candidate
						for _, object := range cached.objectNames {
							object = strings.TrimSpace(object)
							if object == "" {
								continue
							}
							feeds, retryErr := l.recallByAttributes(r.Context(), cached, query, object, normalized, request.TopK, false)
							if retryErr == nil && len(feeds) > 0 {
								newFeeds = append(newFeeds, objectFeed{Object: object, Count: len(feeds), Feed: feeds})
								all = append(all, feeds...)
							}
						}
						if len(all) > 0 {
							evidence = all
							objectFeeds = newFeeds
							item.ObjectFeeds = newFeeds
							askAttributes = normalized
							item.Attributes = normalized
							item.FallbackNote = "属性归一：" + strings.Join(changed, "、")
						}
					}
				}
			}
			if len(evidence) == 0 {
				// 对比类：一边属性名对不上时，退回该对象全量事实，保证对比两边都有内容。
				if compareAsk && subject != "" {
					if retry, retryErr := l.allObjectFacts(r.Context(), cached, query, subject, false); retryErr == nil && len(retry) > 0 {
						evidence = retry
						item.FallbackNote = "对比兜底：对象全量"
					}
				}
				if len(evidence) == 0 {
					if stepBack, stepErr := l.stepBackQuery(r.Context(), query); stepErr == nil && stepBack != "" {
						if subject != "" {
							if retry, retryErr := l.recallObjectFacts(r.Context(), cached, stepBack, subject, 5); retryErr == nil && len(retry) > 0 {
								evidence = retry
								objectFeeds = nil
								item.ObjectFeeds = nil
								item.StepBackQuery = stepBack
								item.FallbackNote = "step-back：" + stepBack
							}
						} else {
							var newFeeds []objectFeed
							var all []candidate
							for _, object := range cached.objectNames {
								object = strings.TrimSpace(object)
								if object == "" {
									continue
								}
								feeds, retryErr := l.recallObjectFacts(r.Context(), cached, stepBack, object, 3)
								if retryErr == nil && len(feeds) > 0 {
									newFeeds = append(newFeeds, objectFeed{Object: object, Count: len(feeds), Feed: feeds})
									all = append(all, feeds...)
								}
							}
							if len(all) > 0 {
								evidence = all
								objectFeeds = newFeeds
								item.ObjectFeeds = newFeeds
								item.StepBackQuery = stepBack
								item.FallbackNote = "step-back：" + stepBack
							}
						}
					}
				}
			}
			item.Feed = evidence
			item.FeedCount = len(evidence)
			for _, candidateItem := range evidence {
				item.Rerank = append(item.Rerank, candidateItem)
				item.Recall = append(item.Recall, candidateItem)
			}
			sort.SliceStable(item.Recall, func(i, j int) bool { return item.Recall[i].Cos > item.Recall[j].Cos })
			for index := range item.Recall {
				item.Recall[index].Rank = index + 1
				item.Recall[index].CosRank = index + 1
			}
			// 去重合并：同一 fact 只保留一条，分数取最高，记录命中 ask。
			for _, candidateItem := range item.Rerank {
				score := candidateItem.Cos
				if candidateItem.RerankScore > score {
					score = candidateItem.RerankScore
				}
				state, exists := mergedByID[candidateItem.ID]
				if !exists {
					state = &mergedState{
						item: mergedEvidence{
							ID: candidateItem.ID, Topic: candidateItem.Topic, Text: candidateItem.Text,
							Subject: candidateItem.Subject, Scope: candidateItem.Scope,
							Conditions: candidateItem.Conditions, Exceptions: candidateItem.Exceptions,
							BestScore: score, CosineMax: candidateItem.Cos, RerankMax: candidateItem.RerankScore,
							Conditional: candidateItem.Conditional,
						},
						seen: map[string]bool{},
					}
					mergedByID[candidateItem.ID] = state
				}
				if score > state.item.BestScore {
					state.item.BestScore = score
				}
				if candidateItem.Cos > state.item.CosineMax {
					state.item.CosineMax = candidateItem.Cos
				}
				if candidateItem.RerankScore > state.item.RerankMax {
					state.item.RerankMax = candidateItem.RerankScore
				}
				askID := strings.TrimSpace(ask.ID)
				if askID == "" {
					askID = "ask"
				}
				if !state.seen[askID] {
					state.seen[askID] = true
					state.item.HitAsks = append(state.item.HitAsks, askID)
				}
			}
		}
		response.Asks = append(response.Asks, item)
		response.DedupeBefore += len(item.Rerank)
	}
	for _, state := range mergedByID {
		response.Merged = append(response.Merged, state.item)
	}
	sort.SliceStable(response.Merged, func(i, j int) bool {
		if response.Merged[i].BestScore == response.Merged[j].BestScore {
			return response.Merged[i].CosineMax > response.Merged[j].CosineMax
		}
		return response.Merged[i].BestScore > response.Merged[j].BestScore
	})
	response.DedupeAfter = len(response.Merged)
	// 对比组：没有点名属性时，公共（全店通用）事实给一份；
	// 已经点名属性时，每个 ask 的召回里已包含对应属性下的公共事实，不再额外给全量公共。
	for _, ask := range request.Asks {
		if strings.TrimSpace(ask.CompareGroup) == "" || len(ask.Subjects) != 1 {
			continue
		}
		if len(cleanStrings(ask.Attributes)) > 0 {
			break
		}
		query := strings.TrimSpace(ask.Query)
		if query == "" {
			query = strings.TrimSpace(request.Question)
		}
		if shared, sharedErr := l.globalFacts(r.Context(), cached, query); sharedErr == nil {
			response.Shared = shared
		}
		break
	}
	writeJSON(w, http.StatusOK, response)
}

type answerFact struct {
	ID         int64    `json:"id"`
	Topic      string   `json:"topic"`
	Text       string   `json:"text"`
	Subject    string   `json:"subject,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Exceptions []string `json:"exceptions,omitempty"`
}

// handleAnswerFacts 最后一步：只把「原问题 + 直播间简介 + 去重后的事实清单」交给回答模型。
func (l *lab) handleAnswerFacts(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID     int64        `json:"plan_id"`
		Question   string       `json:"question"`
		Attributes []string     `json:"attributes"`
		Facts      []answerFact `json:"facts"`
		ChatModel  string       `json:"chat_model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少原问题"})
		return
	}
	if len(request.Facts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有事实证据"})
		return
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cached, err := l.cachedFacts(r.Context(), target, "sentence", "stored")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	model := strings.TrimSpace(request.ChatModel)
	if model == "" {
		model = l.chatMod
	}
	answerPrompt, err := interactionpolicy.Read("danmaku-test-answer.md")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取回答提示词失败: " + err.Error()})
		return
	}
	answerSystem := string(answerPrompt) + "\n\n" + planprompt.InteractionOutputContract
	// 和线上回答输入一致：事实按对象分组，每组 [{ids, text}]，条件/例外拼进 text。
	order := []string{}
	grouped := map[string][]map[string]any{}
	totalFacts := 0
	for _, item := range request.Facts {
		scope := strings.TrimSpace(item.Scope)
		if scope == "" {
			scope = "全局"
		}
		totalFacts++
		text := strings.TrimSpace(item.Text)
		conditions := cleanStrings(item.Conditions)
		exceptions := cleanStrings(item.Exceptions)
		if len(conditions) > 0 {
			text += "（条件：" + strings.Join(conditions, "；") + "）"
		}
		if len(exceptions) > 0 {
			text += "（例外：" + strings.Join(exceptions, "；") + "）"
		}
		if _, exists := grouped[scope]; !exists {
			order = append(order, scope)
		}
		grouped[scope] = append(grouped[scope], map[string]any{"ids": []int64{item.ID}, "text": text})
	}
	facts := make([]map[string]any, 0, len(order))
	for _, scope := range order {
		facts = append(facts, map[string]any{"object": scope, "facts": grouped[scope]})
	}
	// 对象级差异检测：同一主题下不同对象给出不同结论时，单独列出来给回答模型。
	allowedAttributes := map[string]bool{}
	for _, attribute := range request.Attributes {
		if attribute = strings.TrimSpace(attribute); attribute != "" {
			allowedAttributes[attribute] = true
		}
	}
	byTopic := map[string][]answerFact{}
	for _, item := range request.Facts {
		if len(allowedAttributes) > 0 && !allowedAttributes[strings.TrimSpace(item.Topic)] {
			continue
		}
		byTopic[item.Topic] = append(byTopic[item.Topic], item)
	}
	differences := []map[string]any{}
	for topic, items := range byTopic {
		values := []map[string]string{}
		seenValue := map[string]bool{}
		distinct := map[string]bool{}
		for _, item := range items {
			text := strings.TrimSpace(item.Text)
			key := strings.TrimSpace(item.Scope) + "|" + text
			if text == "" || seenValue[key] {
				continue
			}
			seenValue[key] = true
			distinct[text] = true
			values = append(values, map[string]string{"object": strings.TrimSpace(item.Scope), "value": text})
		}
		if len(distinct) >= 2 {
			differences = append(differences, map[string]any{"topic": topic, "objects": values})
		}
	}
	sort.SliceStable(differences, func(i, j int) bool {
		return fmt.Sprint(differences[i]["topic"]) < fmt.Sprint(differences[j]["topic"])
	})
	answerPolicy := []string{
		"先检查事实里是否存在对象级差异：如果不同对象对同一问题的结论不同（有的可以、有的不可以），必须逐对象说明，不能只按多数或全局结论回答。",
		"如果事实之间互相矛盾，以对象/条件更具体的那条为准，并在回答里说明例外。",
	}
	if len(differences) > 0 {
		answerPolicy = append(answerPolicy, "differences 里列出的主题存在对象级不同结论，回答必须逐个对象说明（哪些可以、哪些不可以），禁止只按多数回答。")
	}
	answerInputMap := map[string]any{
		"room_intro": cached.roomIntro,
		"customer_question": map[string]any{
			"speaker": "顾客", "source": "直播间弹幕", "original": request.Question,
		},
		"facts":         facts,
		"differences":   differences,
		"answer_policy": answerPolicy,
	}
	answerInput, _ := json.Marshal(answerInputMap)
	started := time.Now()
	raw, err := l.chat(r.Context(), model, answerSystem, string(answerInput), 2400)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "回答模型调用失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model":         model,
		"question":      request.Question,
		"room_intro":    cached.roomIntro,
		"fact_count":    totalFacts,
		"answer_input":  string(answerInput),
		"answer_raw":    raw,
		"answer_speech": speechText(raw),
		"elapsed_ms":    float64(time.Since(started).Microseconds()) / 1000,
	})
}

// cleanStrings 去掉空字符串，避免输入里出现 "" 条件。
func cleanStrings(items []string) []string {
	out := []string{}
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

// objectShortName 把“1号链接”转成“1号”这类简称，用于校验 query 是否混入其它对象。
func objectShortName(name string) string {
	if index := strings.Index(name, "号"); index > 0 {
		return name[:index+len("号")]
	}
	return ""
}

// mentionedObjects 找原句里点名的对象（目录名称或“N号”简称）。
func mentionedObjects(question string, objects []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, name := range objects {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		hit := strings.Contains(question, name)
		if !hit {
			if short := objectShortName(name); short != "" && strings.Contains(question, short) {
				hit = true
			}
		}
		// “3和5”这类省略写法：目录对象名里的数字出现在“和/与/、/或/的”旁边也算点名。
		if !hit {
			if number := objectLeadingNumber(name); number != "" && numberInQuestion(question, number) {
				hit = true
			}
		}
		if hit {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// objectLeadingNumber 取对象名开头的数字，例如“3号链接”→“3”。
func objectLeadingNumber(name string) string {
	name = strings.TrimSpace(name)
	index := strings.Index(name, "号")
	if index <= 0 {
		return ""
	}
	number := name[:index]
	for _, r := range number {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return number
}

// numberInQuestion 判断数字是否以“对象编号”的方式出现（“3和5”“1号、2号”“3的”），
// 避免把“9.9 元”“25 天”这类数字误认为对象。
func numberInQuestion(question, number string) bool {
	for start := 0; start < len(question); start++ {
		index := strings.Index(question[start:], number)
		if index < 0 {
			return false
		}
		position := start + index
		before := ""
		if position > 0 {
			before = string([]rune(question[:position])[len([]rune(question[:position]))-1])
		}
		after := ""
		if rest := []rune(question[position+len(number):]); len(rest) > 0 {
			after = string(rest[0])
		}
		// 后面紧跟“号/链接”直接算；否则后面是连接词/标点/结尾，且前面不是数字/小数点。
		if strings.HasPrefix(question[position+len(number):], "号") || strings.HasPrefix(question[position+len(number):], "链接") {
			return true
		}
		if after == "" || strings.ContainsRune("和与、或的了吗呢 ，。？?！!：:", []rune(after)[0]) {
			if before == "" || !strings.ContainsRune("0123456789.", []rune(before)[0]) {
				return true
			}
		}
		start = position + len(number)
	}
	return false
}

func (l *lab) handleRecall(w http.ResponseWriter, r *http.Request) {
	var request recallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入问题"})
		return
	}
	if request.TopK <= 0 {
		request.TopK = defaultTopK
	}
	if request.TopK > maxTopK {
		request.TopK = maxTopK
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if request.Refresh {
		l.mu.Lock()
		l.dropCache(request.PlanID)
		l.mu.Unlock()
	}
	timings := map[string]float64{}
	readyAt := time.Now()
	mode := strings.TrimSpace(request.DocMode)
	if mode == "" {
		mode = "lines"
	}
	cached, err := l.cachedFacts(r.Context(), target, mode, request.VectorSource)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	timings["facts_ms"] = float64(time.Since(readyAt).Microseconds()) / 1000

	embedStart := time.Now()
	recallQuery := expandDeliveryQuery(request.Question)
	queryVectors, err := l.embed(r.Context(), []string{recallQuery})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "问题向量化失败: " + err.Error()})
		return
	}
	timings["query_embed_ms"] = float64(time.Since(embedStart).Microseconds()) / 1000

	cosineStart := time.Now()
	scored := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	for index, item := range cached.entries {
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(queryVectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], queryVectors[0])
		}
		f := item.fact
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: item.owner, Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		// 同一条事实按多个对象展开时，只保留分数最高的那个归属副本。
		if at, exists := position[f.ID]; exists {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	if len(scored) > request.TopK {
		scored = scored[:request.TopK]
	}
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	timings["cosine_ms"] = float64(time.Since(cosineStart).Microseconds()) / 1000

	response := recallResponse{
		Plan:           target,
		Question:       request.Question,
		EmbedModel:     l.embedMod,
		FactCount:      len(cached.facts),
		EntryCount:     len(cached.entries),
		TopK:           request.TopK,
		DocMode:        mode,
		VectorSource:   cached.vectorSource,
		VectorModel:    cached.vectorModel,
		MissingVectors: cached.missingVectors,
		Recall:         scored,
		Rerank:         []candidate{},
		RerankModel:    l.reranker.Model(),
		Timings:        timings,
	}
	// 归属分布按真实对象名统计（全局 / 门店 / 1号链接…），与资料页左侧对象树一致；
	// 一条事实适用多个对象时各计一次。
	scopeCounts := map[string]int{}
	for _, item := range cached.facts {
		for _, owner := range item.Owners {
			scopeCounts[owner]++
		}
	}
	response.Plan.Scopes = scopeCounts
	if l.reranker.Enabled() && len(scored) > 1 {
		docs := make([]string, len(scored))
		for index := range scored {
			docs[index] = scored[index].Doc
		}
		rerankStart := time.Now()
		scores, rerankErr := l.reranker.Rerank(r.Context(), recallQuery, docs)
		timings["rerank_ms"] = float64(time.Since(rerankStart).Microseconds()) / 1000
		if rerankErr != nil {
			response.RerankError = rerankErr.Error()
		} else {
			reranked := make([]candidate, len(scored))
			copy(reranked, scored)
			for index := range reranked {
				reranked[index].RerankScore = math.Round(scores[index]*10000) / 10000
			}
			sort.SliceStable(reranked, func(i, j int) bool {
				if reranked[i].RerankScore == reranked[j].RerankScore {
					return reranked[i].Cos > reranked[j].Cos
				}
				return reranked[i].RerankScore > reranked[j].RerankScore
			})
			for index := range reranked {
				reranked[index].Rank = index + 1
				reranked[index].Delta = reranked[index].CosRank - reranked[index].Rank
			}
			response.Rerank = reranked
		}
	}
	// 喂给模型的证据：同一事实只保留一条（按原 fact id 去重），全局事实在没有点名
	// 对象的问题里统一标回“全局”，避免展开副本把 1号/2号/3号 混进上下文。
	pinned := objectMention(request.Question, cached.objectNames)
	normalize := func(items []candidate) {
		for index := range items {
			if items[index].OriginGlobal && items[index].Scope != pinned {
				items[index].Scope = "全局"
			}
		}
	}
	normalize(response.Recall)
	normalize(response.Rerank)
	ordered := response.Recall
	if len(response.Rerank) > 1 {
		ordered = response.Rerank
	}
	feed := make([]candidate, 0, defaultFeedSize)
	seen := map[int64]bool{}
	for _, item := range ordered {
		if seen[item.ID] {
			continue
		}
		// 问题点名了对象时，只喂“该对象 + 全局”的事实，其他链接的事实不进上下文。
		if pinned != "" && !item.OriginGlobal && item.Scope != pinned {
			continue
		}
		seen[item.ID] = true
		feed = append(feed, item)
		if len(feed) >= defaultFeedSize {
			break
		}
	}
	response.Feed = feed
	response.FeedSize = len(feed)
	writeJSON(w, http.StatusOK, response)
}

// objectMention 判断问题里是否点名了某个对象（“1号链接”或简称“1号”“门店”）。
func objectMention(question string, names []string) string {
	question = strings.TrimSpace(question)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(question, name) {
			return name
		}
		if index := strings.Index(name, "号"); index > 0 {
			if strings.Contains(question, name[:index+len("号")]) {
				return name
			}
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (l *lab) handlePage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}

// ---------- 意图挖掘 A/B（提示词 v1 vs v2） ----------

type intentCondition struct {
	Field  string   `json:"field"`
	Value  string   `json:"value"`
	Scope  string   `json:"scope"`
	AskIDs []string `json:"ask_ids,omitempty"`
}

type intentAskV2 struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Query        string   `json:"query"`
	Type         string   `json:"type"`
	Operation    string   `json:"operation"`
	AttrScope    string   `json:"attribute_scope"`
	Subjects     []string `json:"subjects"`
	Attributes   []string `json:"attributes"`
	Attribute    string   `json:"attribute"`
	CompareGroup string   `json:"compare_group"`
	DependsOn    []string `json:"depends_on"`
	ResolvedFrom string   `json:"resolved_from"`
	AltGroup     string   `json:"alternative_group"`
	Confidence   float64  `json:"confidence"`
}

type intentV2 struct {
	Meaning       string            `json:"meaning"`
	Category      string            `json:"category"`
	SpeechAct     string            `json:"speech_act"`
	Conditions    []intentCondition `json:"conditions"`
	Asks          []intentAskV2     `json:"asks"`
	Social        []string          `json:"social"`
	Uncertainties []string          `json:"uncertainties"`
}

type intentVariant struct {
	Raw    string   `json:"raw"`
	Parsed any      `json:"parsed"`
	Issues []string `json:"issues"`
}

type attrCandidate struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

type askVerification struct {
	AskID            string `json:"ask_id"`
	Query            string `json:"query"`
	Attribute        string `json:"attribute"`
	TopFactID        int64  `json:"top_fact_id,omitempty"`
	TopFactTopic     string `json:"top_fact_topic,omitempty"`
	TopFactText      string `json:"top_fact_text,omitempty"`
	TopicMatch       bool   `json:"topic_match"`
	ConditionCovered bool   `json:"condition_covered"`
	Note             string `json:"note,omitempty"`
}

type guidedResult struct {
	Raw           string            `json:"raw"`
	Parsed        intentV2          `json:"parsed"`
	Issues        []string          `json:"issues"`
	Candidates    []attrCandidate   `json:"candidates"`
	Verification  []askVerification `json:"verification"`
	FallbackUsed  bool              `json:"fallback_used"`
	FallbackAsks  []intentAskV2     `json:"fallback_asks"`
	ShouldClarify bool              `json:"should_clarify"`
}

type alignedSummary struct {
	Objects     int `json:"objects"`
	Stores      int `json:"stores"`
	Attributes  int `json:"attributes"`
	Catalog     int `json:"catalog"`
	ContextHits int `json:"context_hits"`
}

type intentCompareResponse struct {
	Model     string         `json:"model"`
	Question  string         `json:"question"`
	RoomIntro string         `json:"room_intro"`
	Payload   string         `json:"payload"`
	Aligned   alignedSummary `json:"aligned"`
	V1        intentVariant  `json:"v1"`
	V2        intentVariant  `json:"v2"`
	Guided    guidedResult   `json:"guided"`
}

// historyEntries 对应线上 conversation() 里的 recent（有历史时才带）。
func historyEntries(history []string) []map[string]string {
	out := make([]map[string]string, 0, len(history))
	for _, item := range history {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, map[string]string{"role": "customer", "text": item})
		}
	}
	return out
}

func objectNamesOf(aligned map[string]any) []string {
	links, _ := aligned["objects"].([]danmakutest.Link)
	out := make([]string, 0, len(links))
	for _, link := range links {
		if name := strings.TrimSpace(link.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

type danmakuSample struct {
	RoomID int64  `json:"room_id"`
	Text   string `json:"text"`
	At     string `json:"at"`
}

func (l *lab) handleDanmaku(w http.ResponseWriter, r *http.Request) {
	limit := 40
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 && value <= 200 {
			limit = value
		}
	}
	rows, err := l.db.QueryContext(r.Context(), `SELECT room_id, content, occurred_at
		FROM live_room_event_archive
		WHERE event_type='chat' AND content IS NOT NULL AND content<>''
		ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []danmakuSample{}
	seen := map[string]bool{}
	for rows.Next() {
		var roomID int64
		var text string
		var at time.Time
		if err := rows.Scan(&roomID, &text, &at); err != nil {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		items = append(items, danmakuSample{RoomID: roomID, Text: text, At: at.Format("2006-01-02 15:04")})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (l *lab) handleIntent(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID     int64    `json:"plan_id"`
		Question   string   `json:"question"`
		ChatModel  string   `json:"chat_model"`
		Objects    []string `json:"objects"`
		Attributes []string `json:"attributes"`
		History    []string `json:"history"`
		RoomIntro  *string  `json:"room_intro"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入弹幕"})
		return
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cached, err := l.cachedFacts(r.Context(), target, "sentence", "stored")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	model := strings.TrimSpace(request.ChatModel)
	if model == "" {
		model = l.chatMod
	}
	// 对齐线上：objects=链接+对象目录，stores=门店，attributes=AttributeName(topic)，
	// attribute_catalog=claim_key→attribute，context_words=语境词命中，room_intro=方案简介。
	aligned := l.intentPayloadAligned(cached, request.Question, request.Objects, request.Attributes)
	if request.RoomIntro != nil {
		aligned["room_intro"] = strings.TrimSpace(*request.RoomIntro)
	}
	if len(request.History) > 0 {
		aligned["recent"] = historyEntries(request.History)
		aligned["history_policy"] = "历史只属于当前观众，主线只作为现场背景，用于指代、省略和连续追问；当前问题优先，历史回答和主线不是当前商品事实依据。"
	}
	payloadRaw, _ := json.Marshal(aligned)
	roomIntro, _ := aligned["room_intro"].(string)
	attributes, _ := aligned["attributes"].([]string)
	objects, _ := aligned["objects"].([]danmakutest.Link)
	contextHits := 0
	if words, ok := aligned["context_words"].(*contextword.Context); ok && words != nil {
		contextHits = len(words.Hits)
	}
	stores, _ := aligned["stores"].([]string)
	catalog, _ := aligned["attribute_catalog"].([]map[string]string)

	run := func(promptName string) (string, error) {
		prompt, err := interactionpolicy.Read(promptName)
		if err != nil {
			return "", fmt.Errorf("读取提示词失败: %w", err)
		}
		return l.chatWithOptions(r.Context(), model, string(prompt), string(payloadRaw), 4000, chatOptions{JSON: true})
	}

	response := intentCompareResponse{
		Model: model, Question: request.Question, RoomIntro: roomIntro, Payload: string(payloadRaw),
		Aligned: alignedSummary{
			Objects: len(objects), Stores: len(stores), Attributes: len(attributes),
			Catalog: len(catalog), ContextHits: contextHits,
		},
	}
	// v1：线上现有提示词，原样保留结构。
	rawV1, errV1 := run("danmaku-test-intent.md")
	if errV1 != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "v1 意图调用失败: " + errV1.Error()})
		return
	}
	var parsedV1 map[string]any
	response.V1 = intentVariant{Raw: rawV1, Parsed: parsedV1}
	if err := looseJSON(rawV1, &parsedV1); err != nil {
		response.V1.Issues = []string{"v1 JSON 无法解析"}
	}
	response.V1.Parsed = parsedV1

	// v2：结构化提示词。
	rawV2, errV2 := run("danmaku-test-intent-v2.md")
	if errV2 != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "v2 意图调用失败: " + errV2.Error()})
		return
	}
	var parsedV2 intentV2
	response.V2 = intentVariant{Raw: rawV2}
	if err := looseJSON(rawV2, &parsedV2); err != nil {
		response.V2.Issues = []string{"v2 JSON 无法解析"}
	} else {
		response.V2.Parsed = parsedV2
		normalizeIntentV2(&parsedV2)
		response.V2.Parsed = parsedV2
		response.V2.Issues = validateIntentV2(parsedV2, request.Question, objectNamesOf(aligned), attributes)
		// 校验不通过时带着问题让模型重试一次（通用修复，不写业务规则）。
		if len(response.V2.Issues) > 0 {
			if repaired := l.repairIntentV2(r.Context(), model, string(payloadRaw), response.V2.Issues, request.Question, objectNamesOf(aligned), attributes); repaired != nil {
				response.V2 = *repaired
			}
		}
	}

	// v3 guided：程序先选候选属性，模型只写 query / 拆 ask。
	response.Guided = l.guidedIntent(r.Context(), cached, request.Question, attributes, aligned, model)
	writeJSON(w, http.StatusOK, response)
}

// pickAttributeCandidates 用“问题本身的向量”去比每个属性下的事实向量，
// 取该属性事实的最高 cosine 作为分数（不把属性名拼进 query，避免属性名自匹配）。
func (l *lab) pickAttributeCandidates(ctx context.Context, cached *cachedPlan, question string, pool []string, limit int) ([]attrCandidate, error) {
	names := []string{}
	for _, name := range pool {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	vectors, err := l.embed(ctx, []string{question})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("问题向量数量不符")
	}
	candidates := make([]attrCandidate, 0, len(names))
	for _, name := range names {
		best := 0.0
		for entryIndex, entry := range cached.entries {
			if strings.TrimSpace(entry.fact.Topic) != name || entryIndex >= len(cached.vectors) {
				continue
			}
			if len(cached.vectors[entryIndex]) != len(vectors[0]) {
				continue
			}
			if score := semantic.CosineSimilarity(cached.vectors[entryIndex], vectors[0]); score > best {
				best = score
			}
		}
		candidates = append(candidates, attrCandidate{Name: name, Score: math.Round(best*10000) / 10000})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

// guidedIntent 运行 v3：候选属性由程序给，模型只负责写 query / 拆 ask；
// 模型跑偏（属性不在候选里 / 没产出）时用程序兜底生成 ask，并做证据校验。
func (l *lab) guidedIntent(ctx context.Context, cached *cachedPlan, question string, attributePool []string, aligned map[string]any, model string) guidedResult {
	result := guidedResult{}
	candidates, err := l.pickAttributeCandidates(ctx, cached, question, attributePool, 3)
	if err != nil || len(candidates) == 0 {
		result.Issues = append(result.Issues, "属性候选生成失败: "+errString(err))
		return result
	}
	result.Candidates = candidates
	candidateNames := make([]string, len(candidates))
	for index, item := range candidates {
		candidateNames[index] = item.Name
	}
	payload := map[string]any{}
	for key, value := range aligned {
		payload[key] = value
	}
	payload["candidate_attributes"] = candidateNames
	raw, _ := json.Marshal(payload)
	prompt, err := interactionpolicy.Read("danmaku-test-intent-v3.md")
	if err != nil {
		result.Issues = append(result.Issues, "读取 v3 提示词失败: "+err.Error())
		return result
	}
	rawGuided, err := l.chatWithOptions(ctx, model, string(prompt), string(raw), 4000, chatOptions{JSON: true})
	result.Raw = rawGuided
	if err != nil {
		result.Issues = append(result.Issues, "v3 模型调用失败: "+err.Error())
	} else if err := looseJSON(rawGuided, &result.Parsed); err != nil {
		result.Issues = append(result.Issues, "v3 JSON 无法解析")
	} else {
		result.Issues = validateIntentV2(result.Parsed, question, objectNamesOf(aligned), candidateNames)
	}

	// 兜底：模型没产出或挑了候选外的属性 → 程序按候选属性直接生成 ask。
	if len(result.Parsed.Asks) == 0 || containsAttributeIssue(result.Issues) {
		result.FallbackUsed = true
		for index, name := range candidateNames {
			result.FallbackAsks = append(result.FallbackAsks, intentAskV2{
				ID:         fmt.Sprintf("f%d", index+1),
				Text:       question,
				Query:      question + " " + name,
				Type:       "提问",
				Attribute:  name,
				AltGroup:   "g1",
				Confidence: 0.2,
			})
		}
	}

	// 证据校验：每个 ask 拿去召回，看 top1 事实的主题是否等于属性、地区条件是否适用。
	location := l.geo.provincesIn(question)
	locationSet := map[string]bool{}
	for _, province := range location {
		locationSet[province] = true
	}
	topicProvinces := l.regionTopicProvinces(cached)
	verify := func(asks []intentAskV2) (int, int) {
		result.Verification = nil
		failed := 0
		for _, ask := range asks {
			bad := false
			verification := askVerification{AskID: ask.ID, Query: ask.Query, Attribute: ask.Attribute}
			evidence, recallErr := l.recallAsk(ctx, cached, ask.Query, 8, nil, false)
			if recallErr != nil || len(evidence) == 0 {
				verification.Note = "召回为空"
				result.Verification = append(result.Verification, verification)
				failed++
				continue
			}
			top := evidence[0]
			verification.TopFactID = top.ID
			verification.TopFactTopic = top.Topic
			verification.TopFactText = top.Text
			verification.TopicMatch = strings.TrimSpace(top.Topic) == strings.TrimSpace(ask.Attribute)
			// 有地区条件时，证据必须“带地区条件且适用于观众地区”才算覆盖。
			if len(locationSet) > 0 {
				fact := factByID(cached, top.ID)
				regions := regionPredicatesOf(fact, l.geo)
				if len(regions) == 0 {
					verification.ConditionCovered = false
					verification.Note = "证据没有地区条件，不能覆盖问题的地区条件"
				} else {
					applicable, _ := l.regionApplicable(fact, locationSet, topicProvinces)
					verification.ConditionCovered = applicable
					if !verification.ConditionCovered {
						verification.Note = "证据的地区条件不适用于该观众地区，可能属性挑错或缺少对应事实"
					}
				}
			} else {
				verification.ConditionCovered = true
			}
			if !verification.TopicMatch {
				verification.Note = "召回 top1 主题与属性不一致: " + top.Topic
				bad = true
			}
			if !verification.ConditionCovered {
				bad = true
			}
			if bad {
				failed++
			}
			result.Verification = append(result.Verification, verification)
		}
		return failed, len(asks)
	}
	asks := result.Parsed.Asks
	if result.FallbackUsed && len(result.FallbackAsks) > 0 {
		asks = result.FallbackAsks
	}
	failed, totalAsks := verify(asks)
	if failed > 0 && !result.FallbackUsed {
		// 模型挑的属性与召回证据对不上：不再相信模型这次的选择，
		// 退回到“程序按候选属性生成 ask”，并重新做证据校验。
		result.Issues = append(result.Issues, "属性与召回证据不一致，已按候选属性生成兜底 ask")
		result.FallbackUsed = true
		result.FallbackAsks = nil
		for index, candidate := range candidates {
			result.FallbackAsks = append(result.FallbackAsks, intentAskV2{
				ID:         fmt.Sprintf("f%d", index+1),
				Text:       question,
				Query:      question + " " + candidate.Name,
				Type:       "提问",
				Attribute:  candidate.Name,
				AltGroup:   "g1",
				Confidence: 0.2,
			})
		}
		failed, totalAsks = verify(result.FallbackAsks)
	}
	// 所有候选都过不了证据校验：不硬答，交给澄清。
	if totalAsks > 0 && failed >= totalAsks {
		result.ShouldClarify = true
		result.FallbackAsks = nil
		result.Issues = append(result.Issues, "候选属性都与问题条件不匹配，应澄清而不是硬答")
	}
	return result
}

func containsAttributeIssue(issues []string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, "属性不在目录中") || strings.Contains(issue, "ask 缺少属性") {
			return true
		}
	}
	return false
}

func factByID(cached *cachedPlan, id int64) fact {
	for _, item := range cached.facts {
		if item.ID == id {
			return item
		}
	}
	return fact{}
}

func errString(err error) string {
	if err == nil {
		return "无候选属性"
	}
	return err.Error()
}

// normalizeIntentV2 兼容 attribute / attributes 两种写法，并补齐 scope。
func normalizeIntentV2(intent *intentV2) {
	for index := range intent.Asks {
		ask := &intent.Asks[index]
		attributes := cleanStrings(ask.Attributes)
		if len(attributes) == 0 && strings.TrimSpace(ask.Attribute) != "" {
			attributes = []string{strings.TrimSpace(ask.Attribute)}
		}
		ask.Attributes = attributes
		if strings.TrimSpace(ask.Attribute) == "" && len(attributes) > 0 {
			ask.Attribute = attributes[0]
		}
		if strings.TrimSpace(ask.AttrScope) == "" {
			if len(attributes) == 0 {
				ask.AttrScope = "全部"
			} else {
				ask.AttrScope = "明确"
			}
		}
	}
}

// repairIntentV2 把校验问题回给模型，让它重出一次；修复后问题更少才采用。
func (l *lab) repairIntentV2(ctx context.Context, model, payload string, issues []string, question string, objects, attributes []string) *intentVariant {
	prompt, err := interactionpolicy.Read("danmaku-test-intent-v2.md")
	if err != nil {
		return nil
	}
	user := payload + "\n\n上一次输出存在以下校验问题：\n- " + strings.Join(issues, "\n- ") +
		"\n请修正后重新输出完整 JSON，只输出 JSON。"
	raw, err := l.chatWithOptions(ctx, model, string(prompt), user, 4000, chatOptions{JSON: true})
	if err != nil {
		return nil
	}
	var parsed intentV2
	if err := looseJSON(raw, &parsed); err != nil {
		return nil
	}
	normalizeIntentV2(&parsed)
	repairedIssues := validateIntentV2(parsed, question, objects, attributes)
	if len(repairedIssues) >= len(issues) {
		return nil
	}
	return &intentVariant{Raw: raw, Parsed: parsed, Issues: repairedIssues}
}

func attributeDirectory(cached *cachedPlan) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, item := range cached.facts {
		attribute := item.attributeName()
		if attribute != "" && !seen[attribute] {
			seen[attribute] = true
			out = append(out, attribute)
		}
	}
	return out
}

func validateIntentV2(intent intentV2, question string, objects, attributes []string) []string {
	issues := []string{}
	objectSet := map[string]bool{}
	for _, name := range objects {
		objectSet[strings.TrimSpace(name)] = true
	}
	attributeSet := map[string]bool{"其他": true}
	for _, name := range attributes {
		attributeSet[strings.TrimSpace(name)] = true
	}
	ids := map[string]int{}
	altGroups := map[string][]intentAskV2{}
	compareGroups := map[string][]intentAskV2{}
	mentioned := mentionedObjects(question, objects)
	for index, ask := range intent.Asks {
		if ask.ID == "" {
			issues = append(issues, fmt.Sprintf("asks[%d] 缺少 id", index))
		} else if _, exists := ids[ask.ID]; exists {
			issues = append(issues, "ask id 重复: "+ask.ID)
		} else {
			ids[ask.ID] = index
		}
		if strings.TrimSpace(ask.Text) == "" || !strings.Contains(question, strings.TrimSpace(strings.TrimRight(ask.Text, "，。？?！! "))) {
			issues = append(issues, "ask 文本不是原话子句: "+ask.Text)
		}
		// 检索友好：query 必须能直接拿去召回。
		if strings.TrimSpace(ask.Query) == "" {
			issues = append(issues, "ask 缺少检索问句 query: "+ask.Text)
		}
		// 单对象规则：一个 ask 最多一个对象。
		subjects := []string{}
		for _, subject := range ask.Subjects {
			if subject = strings.TrimSpace(subject); subject != "" {
				subjects = append(subjects, subject)
			}
		}
		if len(subjects) > 1 {
			issues = append(issues, "一个 ask 只能有一个对象: "+strings.Join(subjects, "、"))
		}
		for _, subject := range subjects {
			if len(objectSet) > 0 && !objectSet[subject] {
				issues = append(issues, "对象不在目录中: "+subject)
			}
		}
		// query 里不能出现别的对象名（对象简称也算）。
		for _, name := range objects {
			name = strings.TrimSpace(name)
			if name == "" || containsString(subjects, name) {
				continue
			}
			short := objectShortName(name)
			if strings.Contains(ask.Query, name) || (short != "" && strings.Contains(ask.Query, short)) {
				issues = append(issues, "query 里出现了别的对象: "+name)
			}
		}
		// attributes 为空 = 该对象全部属性（允许）；非空时逐个校验目录。
		for _, attribute := range ask.Attributes {
			attribute = strings.TrimSpace(attribute)
			if attribute != "" && !attributeSet[attribute] {
				issues = append(issues, "属性不在目录中: "+attribute)
			}
		}
		if len(ask.Attributes) == 0 && strings.TrimSpace(ask.Attribute) != "" && !attributeSet[strings.TrimSpace(ask.Attribute)] {
			issues = append(issues, "属性不在目录中: "+ask.Attribute)
		}
		for _, dependency := range ask.DependsOn {
			position, exists := ids[strings.TrimSpace(dependency)]
			if !exists {
				issues = append(issues, "depends_on 指向不存在或更晚的 ask: "+dependency)
				continue
			}
			if position >= index {
				issues = append(issues, "depends_on 必须指向更早的 ask: "+dependency)
			}
		}
		if group := strings.TrimSpace(ask.AltGroup); group != "" {
			altGroups[group] = append(altGroups[group], ask)
		}
		if group := strings.TrimSpace(ask.CompareGroup); group != "" {
			compareGroups[group] = append(compareGroups[group], ask)
		}
		// 原句没点名对象时，候选 ask 不能凭空补对象（历史承接除外）。
		if len(mentioned) == 0 && len(ask.Subjects) > 0 &&
			!strings.Contains(ask.ResolvedFrom, "历史") && !strings.Contains(ask.ResolvedFrom, "上下文") {
			issues = append(issues, "原句没有点名对象，ask 不应补对象: "+strings.Join(ask.Subjects, "、"))
		}
	}
	// 歧义拆分成多个候选时，每个 alternative_group 至少两个 ask，且属性不能重复。
	for group, asks := range altGroups {
		if len(asks) < 2 {
			issues = append(issues, "alternative_group 只拆出一个 ask（应拆多个候选）: "+group)
			continue
		}
		seenAttributes := map[string]bool{}
		for _, ask := range asks {
			attribute := strings.TrimSpace(ask.Attribute)
			if seenAttributes[attribute] {
				issues = append(issues, "alternative_group 候选属性重复: "+attribute)
			}
			seenAttributes[attribute] = true
		}
	}
	// 对比组：至少两个 ask、每个一个对象、属性一致、对象不重复。
	for group, asks := range compareGroups {
		if len(asks) < 2 {
			issues = append(issues, "compare_group 至少需要两个 ask: "+group)
			continue
		}
		seenSubjects := map[string]bool{}
		for _, ask := range asks {
			if len(ask.Subjects) != 1 {
				issues = append(issues, "对比组每个 ask 必须恰好一个对象: "+group)
			}
			for _, subject := range ask.Subjects {
				subject = strings.TrimSpace(subject)
				if seenSubjects[subject] {
					issues = append(issues, "对比组对象重复: "+subject)
				}
				seenSubjects[subject] = true
			}
			if ask.Attribute != asks[0].Attribute {
				issues = append(issues, "对比组属性不一致: "+group)
			}
			if ask.AttrScope != asks[0].AttrScope {
				issues = append(issues, "对比组 attribute_scope 不一致: "+group)
			}
		}
	}
	// 原句点名的对象必须全部被 asks 覆盖。
	covered := map[string]bool{}
	for _, ask := range intent.Asks {
		for _, subject := range ask.Subjects {
			covered[strings.TrimSpace(subject)] = true
		}
	}
	for _, name := range mentioned {
		if !covered[name] {
			issues = append(issues, "原句点名的对象没有被 ask 覆盖: "+name)
		}
	}
	for index, condition := range intent.Conditions {
		if strings.TrimSpace(condition.Field) == "" || strings.TrimSpace(condition.Value) == "" {
			issues = append(issues, fmt.Sprintf("conditions[%d] 缺少 field 或 value", index))
		}
	}
	if len(intent.Asks) == 0 && len(intent.Social) == 0 && intent.Category != "看不懂" {
		issues = append(issues, "没有解析出 ask，也没有社交内容")
	}
	return issues
}

// ---------- 完整链路：意图 → 每个 ask 召回 → 精排 → 回答 ----------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Temperature    *float64          `json:"temperature,omitempty"`
	EnableThinking *bool             `json:"enable_thinking,omitempty"`
	Thinking       map[string]string `json:"thinking,omitempty"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

// chatOptions 对齐线上 agentgateway 的请求参数：意图/回答用 JSON 模式、
// 不显式设置温度（线上 Request.Temperature 为空就不发）。
type chatOptions struct {
	Temperature *float64
	JSON        bool
}

type chatResponse struct {
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (l *lab) chat(ctx context.Context, model, system, user string, maxTokens int) (string, error) {
	zero := 0.0
	return l.chatWithOptions(ctx, model, system, user, maxTokens, chatOptions{Temperature: &zero})
}

func (l *lab) chatWithOptions(ctx context.Context, model, system, user string, maxTokens int, opts chatOptions) (string, error) {
	// 弹幕测试不启用思考，和线上互动一致，也避免意图/回答拖到几十秒。
	thinkingOff := false
	if opts.JSON {
		system += "\n只返回合法 JSON，不要代码围栏或解释。"
	}
	if strings.TrimSpace(model) == "" {
		model = l.chatMod
	}
	// enable_thinking 是 DashScope 的参数；DeepSeek 走 thinking={"type":"disabled"}，
	// 和线上 agentgateway 对 api.deepseek.com 的处理保持一致。
	var enableThinking *bool
	var thinking map[string]string
	if base := strings.ToLower(l.chatURL()); strings.Contains(base, "dashscope") || strings.Contains(base, "aliyuncs") {
		enableThinking = &thinkingOff
	} else if strings.Contains(base, "deepseek.com") {
		thinking = map[string]string{"type": "disabled"}
	}
	var responseFormat map[string]string
	if opts.JSON {
		responseFormat = map[string]string{"type": "json_object"}
	}
	payload, err := json.Marshal(chatRequest{
		Model:          model,
		Messages:       []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		MaxTokens:      maxTokens,
		Temperature:    opts.Temperature,
		EnableThinking: enableThinking,
		Thinking:       thinking,
		ResponseFormat: responseFormat,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, l.chatURL()+"/chat/completions", strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+l.chatAPIKey())
	request.Header.Set("Content-Type", "application/json")
	response, err := l.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var decoded chatResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode chat: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := http.StatusText(response.StatusCode)
		if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
			message = decoded.Error.Message
		}
		return "", fmt.Errorf("chat http %d: %s", response.StatusCode, message)
	}
	log.Printf("chat usage model=%s prompt=%d completion=%d total=%d", model,
		decoded.Usage.PromptTokens, decoded.Usage.CompletionTokens, decoded.Usage.TotalTokens)
	if len(decoded.Choices) == 0 {
		return "", errors.New("chat 返回为空")
	}
	choice := decoded.Choices[0]
	if choice.FinishReason == "length" || strings.TrimSpace(choice.Message.Content) == "" {
		log.Printf("chat truncated or empty model=%s finish=%s content_len=%d reasoning_len=%d",
			model, choice.FinishReason, len(choice.Message.Content), len(choice.Message.ReasoningContent))
	}
	return choice.Message.Content, nil
}

// looseJSON 兼容模型偶尔包 ```json 代码块或前后说明的情况。
func looseJSON(raw string, out any) error {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "```") {
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[index+1:]
		}
		if index := strings.LastIndex(text, "```"); index >= 0 {
			text = text[:index]
		}
	}
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start >= 0 && end > start {
		text = text[start : end+1]
	}
	return json.Unmarshal([]byte(text), out)
}

type intentAsk struct {
	Text       string         `json:"text"`
	Subjects   []string       `json:"subjects"`
	SpeechAct  string         `json:"speech_act"`
	Conditions map[string]any `json:"conditions,omitempty"`
}

type intentResult struct {
	Meaning       string      `json:"meaning"`
	Category      string      `json:"category"`
	SpeechAct     string      `json:"speech_act"`
	Operation     string      `json:"operation"`
	Corrected     string      `json:"corrected"`
	Uncertainties []string    `json:"uncertainties"`
	Asks          []intentAsk `json:"asks"`
}

type pipelineAskResult struct {
	Ask            string      `json:"ask"`
	Query          string      `json:"query,omitempty"`
	Subjects       []string    `json:"subjects,omitempty"`
	Attributes     []string    `json:"attributes,omitempty"`
	AttributeScope string      `json:"attribute_scope,omitempty"`
	CompareGroup   string      `json:"compare_group,omitempty"`
	Type           string      `json:"type,omitempty"`
	Notes          []string    `json:"notes,omitempty"`
	Evidence       []candidate `json:"evidence"`
	Error          string      `json:"error,omitempty"`
}

type pipelineResponse struct {
	Plan              plan                `json:"plan"`
	Question          string              `json:"question"`
	CorrectedQuestion string              `json:"corrected_question,omitempty"`
	Correction        *correctionResult   `json:"correction,omitempty"`
	ChatModel         string              `json:"chat_model"`
	Intent            intentResult        `json:"intent"`
	IntentRaw         string              `json:"intent_raw"`
	Asks              []pipelineAskResult `json:"asks"`
	Feed              []candidate         `json:"feed"`
	Location          []string            `json:"location,omitempty"`
	RegionBranch      string              `json:"region_branch,omitempty"`
	AnswerRaw         string              `json:"answer_raw"`
	AnswerInput       string              `json:"answer_input"`
	AnswerSpeech      string              `json:"answer_speech"`
	Timings           map[string]float64  `json:"timings_ms"`
}

func (l *lab) handlePipeline(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanID       int64  `json:"plan_id"`
		Question     string `json:"question"`
		TopK         int    `json:"top_k"`
		DocMode      string `json:"doc_mode"`
		MaxAsks      int    `json:"max_asks"`
		ChatModel    string `json:"chat_model"`
		VectorSource string `json:"vector_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式不正确"})
		return
	}
	request.Question = strings.TrimSpace(request.Question)
	if request.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请输入弹幕"})
		return
	}
	if request.TopK <= 0 {
		request.TopK = 12
	}
	if request.TopK > 30 {
		request.TopK = 30
	}
	if request.MaxAsks <= 0 || request.MaxAsks > 5 {
		request.MaxAsks = 3
	}
	if request.DocMode == "" {
		request.DocMode = "sentence"
	}
	target, err := l.planByID(r.Context(), request.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cached, err := l.cachedFacts(r.Context(), target, request.DocMode, request.VectorSource)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	// 先做文本纠错：只改错别字/全半角，不改语义；纠错结果用于意图和召回。
	originalQuestion := request.Question
	correction := l.correctDanmaku(r.Context(), target, request.Question)
	if correction.Corrected != "" {
		request.Question = correction.Corrected
	}
	timings := map[string]float64{}

	// 支持用请求参数临时换模型（默认 DASHSCOPE_CHAT_MODEL）。
	chatModel := strings.TrimSpace(request.ChatModel)
	if chatModel == "" {
		chatModel = l.chatMod
	}

	// 1) 意图：结构化 v2，和「解析意图」页同款提示词/输入。
	intentStart := time.Now()
	aligned := l.intentPayloadAligned(cached, request.Question, nil, nil)
	payloadRaw, _ := json.Marshal(aligned)
	intentPrompt, err := interactionpolicy.Read("danmaku-test-intent-v2.md")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取意图提示词失败: " + err.Error()})
		return
	}
	rawIntent, err := l.chatWithOptions(r.Context(), chatModel, string(intentPrompt), string(payloadRaw), 4000, chatOptions{JSON: true})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "意图模型调用失败: " + err.Error()})
		return
	}
	timings["intent_ms"] = float64(time.Since(intentStart).Microseconds()) / 1000
	var parsedIntent intentV2
	if err := looseJSON(rawIntent, &parsedIntent); err != nil {
		rawIntent, err = l.chatWithOptions(r.Context(), chatModel, string(intentPrompt), string(payloadRaw), 4000, chatOptions{JSON: true})
		if err == nil {
			err = looseJSON(rawIntent, &parsedIntent)
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "意图结果无法解析", "intent_raw": rawIntent})
			return
		}
	}
	normalizeIntentV2(&parsedIntent)
	// 对象名归一：模型可能写“门店/全店/所有门店”，统一映射到目录里的“全门店”；目录里没有的对象清空。
	for index := range parsedIntent.Asks {
		parsedIntent.Asks[index].Subjects = normalizeSubjectsForPlan(parsedIntent.Asks[index].Subjects, cached.objectNames, cached.globalObjectName)
	}
	// 泛问（“1号怎么样”）按对象全量事实召回：清掉模型自行推断的属性，避免被具体属性过滤。
	if looksOpenQuestion(originalQuestion) {
		for index := range parsedIntent.Asks {
			ask := &parsedIntent.Asks[index]
			if !attributeNamedInText(originalQuestion, ask.Attributes) {
				ask.Attributes = nil
				ask.Attribute = ""
				ask.AttrScope = "全部"
			}
		}
	}
	asksV2 := parsedIntent.Asks
	if len(asksV2) == 0 {
		asksV2 = []intentAskV2{{ID: "a1", Text: request.Question, Query: request.Question}}
	}
	if len(asksV2) > request.MaxAsks {
		asksV2 = asksV2[:request.MaxAsks]
	}
	intent := intentResult{
		Meaning: parsedIntent.Meaning, Category: parsedIntent.Category,
		SpeechAct: parsedIntent.SpeechAct, Uncertainties: parsedIntent.Uncertainties,
	}
	for _, ask := range asksV2 {
		intent.Asks = append(intent.Asks, intentAsk{Text: ask.Text, Subjects: ask.Subjects})
	}
	asks := asksV2

	// 2) 每个 ask 独立召回 + 精排。追问（“2号的呢”）带上上一句，避免只按“呢”召回。
	recallStart := time.Now()
	askResults := make([]pipelineAskResult, 0, len(asks))
	merged := map[int64]candidate{}
	order := []int64{}
	previousText := ""
	for _, ask := range asks {
		text := strings.TrimSpace(ask.Text)
		if text == "" {
			text = request.Question
		}
		query := strings.TrimSpace(ask.Query)
		if query == "" {
			query = text
		}
		if previousText != "" && isFollowUp(text) && query == text {
			query = previousText + "，" + text
		}
		query = expandDeliveryQuery(query)
		allowed := askOwnerFilter(intentAsk{Text: text, Subjects: ask.Subjects}, cached.objectNames)
		result := pipelineAskResult{Ask: text, Subjects: ask.Subjects, Evidence: []candidate{}}
		askAttributes := cleanStrings(ask.Attributes)
		if len(askAttributes) == 0 && strings.TrimSpace(ask.Attribute) != "" && strings.TrimSpace(ask.Attribute) != "其他" {
			askAttributes = []string{strings.TrimSpace(ask.Attribute)}
		}
		var attrNotes []string
		if len(askAttributes) > 0 {
			if expanded, notes := l.normalizeAttributes(r.Context(), cached, askAttributes); len(notes) > 0 {
				askAttributes = expanded
				attrNotes = append(attrNotes, "属性扩展："+strings.Join(notes, "、"))
			}
			if expanded, notes := l.expandAttributesFromQuery(r.Context(), cached, query, askAttributes); len(notes) > 0 {
				askAttributes = expanded
				attrNotes = append(attrNotes, "问题扩展："+strings.Join(notes, "、"))
			}
		}
		result.Notes = attrNotes
		result.Query = query
		result.Attributes = askAttributes
		result.AttributeScope = ask.AttrScope
		result.CompareGroup = ask.CompareGroup
		result.Type = ask.Type
		subject := ""
		if len(ask.Subjects) == 1 {
			subject = strings.TrimSpace(ask.Subjects[0])
		}
		var evidence []candidate
		var recallErr error
		compareAsk := strings.TrimSpace(ask.CompareGroup) != ""
		// 对比类：原话没点名属性时覆盖全部属性；只有原话明确提到某属性（如“价格区别”）才按属性过滤。
		compareFull := compareAsk && (strings.TrimSpace(ask.AttrScope) == "全部" || len(askAttributes) == 0 || !attributeNamedInText(text, askAttributes))
		switch {
		case subject != "" && compareFull:
			// 对比但没点名属性：覆盖该对象全部事实，公共事实给一份。
			evidence, recallErr = l.allObjectFacts(r.Context(), cached, query, subject, false)
			if shared, err := l.globalFacts(r.Context(), cached, query); err == nil {
				evidence = append(evidence, shared...)
			}
		case subject == "" && len(askAttributes) > 0:
			evidence, recallErr = l.recallAcrossObjects(r.Context(), cached, query, askAttributes, request.TopK)
		case len(askAttributes) > 0:
			evidence, recallErr = l.recallByAttributes(r.Context(), cached, query, subject, askAttributes, request.TopK, true)
		case subject != "" && subject == cached.globalObjectName:
			// 全门店/全局：取全部公共事实。
			evidence, recallErr = l.globalFacts(r.Context(), cached, query)
		case subject != "":
			// 只有对象、没有属性：该对象全量事实 + 公共事实。
			evidence, recallErr = l.allObjectFacts(r.Context(), cached, query, subject, true)
		default:
			evidence, recallErr = l.recallAsk(r.Context(), cached, query, request.TopK, allowed, allowed == nil)
		}
		// 空召回兜底：对比类先补对象全量，其余走 step-back 再召回一次。
		if recallErr == nil && len(evidence) == 0 {
			if compareAsk && subject != "" {
				if full, err := l.allObjectFacts(r.Context(), cached, query, subject, false); err == nil {
					evidence = full
				}
			}
			if len(evidence) == 0 {
				if stepBack, err := l.stepBackQuery(r.Context(), query); err == nil && stepBack != "" {
					if subject != "" {
						if retry, err := l.recallObjectFacts(r.Context(), cached, stepBack, subject, 5); err == nil {
							evidence = retry
						}
					} else if retry, err := l.recallAsk(r.Context(), cached, stepBack, request.TopK, allowed, allowed == nil); err == nil {
						evidence = retry
					}
				}
			}
		}
		// 陈述类（"我在上海"）没有明确问句时，额外做一次宽召回，避免只被属性过滤限制。
		if recallErr == nil && strings.TrimSpace(intent.Category) == "陈述" {
			if broad, err := l.recallAsk(r.Context(), cached, query, request.TopK, nil, true); err == nil {
				evidence = append(evidence, broad...)
			}
		}
		evidence = dedupeCandidates(evidence)
		if recallErr != nil {
			result.Error = recallErr.Error()
		} else {
			// 每个 ask 保留自己的完整去重清单，不做 6 条/8 条截断。
			result.Evidence = evidence
			for _, item := range evidence {
				existing, ok := merged[item.ID]
				if !ok {
					order = append(order, item.ID)
					merged[item.ID] = item
					continue
				}
				if item.RerankScore > existing.RerankScore {
					merged[item.ID] = item
				}
			}
		}
		askResults = append(askResults, result)
		previousText = text
	}
	timings["recall_ms"] = float64(time.Since(recallStart).Microseconds()) / 1000
	allEvidence := make([]candidate, 0, len(order))
	for _, id := range order {
		allEvidence = append(allEvidence, merged[id])
	}
	// 观众地区：从原句和每个 ask 里解析省份，按地区条件过滤“不适用的分支”。
	location := []string{}
	seenLocation := map[string]bool{}
	addLocation := func(provinces []string) {
		for _, province := range provinces {
			if province != "" && !seenLocation[province] {
				seenLocation[province] = true
				location = append(location, province)
			}
		}
	}
	addLocation(l.geo.provincesIn(request.Question))
	for _, ask := range asks {
		addLocation(l.geo.provincesIn(ask.Text))
	}
	// 先在完整候选上去掉条件不适用的事实，再按预算截断，避免“不抽真空(川渝)”占掉
	// 名额把“其它地区采用真空包装”挤出上下文。
	allEvidence, regionBranch := l.applyRegionConditions(allEvidence, cached, location)
	// 最终证据 = 各 ask 去重清单之和，不做全局 topN 截断。
	feed := allEvidence

	// 3) 回答模型：线上弹幕测试回答提示词 + 五字段输出契约。
	answerStart := time.Now()
	answerPrompt, err := interactionpolicy.Read("danmaku-test-answer.md")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取回答提示词失败: " + err.Error()})
		return
	}
	answerSystem := string(answerPrompt) + "\n\n" + planprompt.InteractionOutputContract
	answerInput := buildAnswerPayload(request.Question, cached.roomIntro, intent, feed, location, regionBranch)
	answerRaw, err := l.chat(r.Context(), chatModel, answerSystem, answerInput, 2400)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "回答模型调用失败: " + err.Error()})
		return
	}
	timings["answer_ms"] = float64(time.Since(answerStart).Microseconds()) / 1000

	writeJSON(w, http.StatusOK, pipelineResponse{
		Plan:              target,
		Question:          originalQuestion,
		CorrectedQuestion: request.Question,
		Correction:        &correction,
		ChatModel:         chatModel,
		Intent:            intent,
		IntentRaw:         rawIntent,
		Asks:              askResults,
		Feed:              feed,
		Location:          location,
		RegionBranch:      regionBranch,
		AnswerRaw:         answerRaw,
		AnswerInput:       answerInput,
		AnswerSpeech:      speechText(answerRaw),
		Timings:           timings,
	})
}

// dedupeCandidates 按 fact id 去重，保留分数更高的一条。
func dedupeCandidates(items []candidate) []candidate {
	out := make([]candidate, 0, len(items))
	index := map[int64]int{}
	for _, item := range items {
		position, ok := index[item.ID]
		if !ok {
			index[item.ID] = len(out)
			out = append(out, item)
			continue
		}
		if item.RerankScore > out[position].RerankScore || (item.RerankScore == out[position].RerankScore && item.Cos > out[position].Cos) {
			out[position] = item
		}
	}
	return out
}

// normalizeSubjectsForPlan 把模型写的对象名归一到目录：精确匹配保留；和全局对象真实名字互相包含的归到全局；其余清空。
func normalizeSubjectsForPlan(subjects []string, names []string, globalName string) []string {
	available := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			available[name] = true
		}
	}
	if globalName != "" {
		available[globalName] = true
	}
	out := []string{}
	seen := map[string]bool{}
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if available[subject] {
			if !seen[subject] {
				seen[subject] = true
				out = append(out, subject)
			}
			continue
		}
		// 不写死业务词：只要和全局对象的真实名字互相包含（例如“门店”⊂“全门店”），就归到全局对象。
		if globalName != "" && (strings.Contains(globalName, subject) || strings.Contains(subject, globalName)) {
			if !seen[globalName] {
				seen[globalName] = true
				out = append(out, globalName)
			}
		}
	}
	return out
}

// looksOpenQuestion 判断是不是“怎么样/好不好”这类泛问。
func looksOpenQuestion(text string) bool {
	for _, keyword := range []string{"怎么样", "咋样", "好不好", "如何", "介绍", "有什么", "值不值", "推荐"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// attributeNamedInText 判断原话是否明确点到了某个属性名（用于区分“区别”和“价格区别”）。
func attributeNamedInText(text string, attributes []string) bool {
	for _, attribute := range attributes {
		attribute = strings.TrimSpace(attribute)
		if attribute != "" && strings.Contains(text, attribute) {
			return true
		}
	}
	return false
}

// balanceEvidence 通用证据预算：单作用域取前 budget；多作用域按作用域轮转，保证每个对象都有机会。
func (l *lab) balanceEvidence(items []candidate, budget int) []candidate {
	if len(items) <= budget {
		return items
	}
	buckets := map[string][]candidate{}
	order := []string{}
	for _, item := range items {
		scope := item.Scope
		if scope == "" {
			scope = "全局"
		}
		if _, ok := buckets[scope]; !ok {
			order = append(order, scope)
		}
		buckets[scope] = append(buckets[scope], item)
	}
	if len(order) <= 1 {
		return items[:budget]
	}
	out := []candidate{}
	for round := 0; len(out) < budget; round++ {
		picked := false
		for _, scope := range order {
			if round < len(buckets[scope]) {
				out = append(out, buckets[scope][round])
				picked = true
				if len(out) >= budget {
					break
				}
			}
		}
		if !picked {
			break
		}
	}
	return out
}

// rerankEvidence 对一批候选统一做一次 rerank，按精排分排序。
func (l *lab) rerankEvidence(ctx context.Context, query string, items []candidate) []candidate {
	if !l.reranker.Enabled() || len(items) <= 1 {
		return items
	}
	docs := make([]string, len(items))
	for index := range items {
		docs[index] = items[index].Doc
	}
	scores, err := l.reranker.Rerank(ctx, query, docs)
	if err != nil || len(scores) != len(items) {
		return items
	}
	out := append([]candidate{}, items...)
	for index := range out {
		out[index].RerankScore = math.Round(scores[index]*10000) / 10000
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RerankScore > out[j].RerankScore })
	return out
}

// complementByTopic 在 rerank 头部同主题上补齐其它条件分支：
// 例如“抽真空吗”同时给“川渝地区不抽真空”和“其它地区采用真空包装”，
// 避免回答模型只看到条件分支之一就下无条件结论。
func complementByTopic(ranked []candidate, head, limit int) []candidate {
	if len(ranked) <= head {
		return ranked
	}
	selected := []candidate{}
	included := map[int64]bool{}
	topics := map[string]bool{}
	for _, item := range ranked {
		if len(selected) >= head {
			break
		}
		selected = append(selected, item)
		included[item.ID] = true
		topics[item.Topic] = true
	}
	for _, item := range ranked {
		if len(selected) >= limit {
			break
		}
		if included[item.ID] || !topics[item.Topic] {
			continue
		}
		selected = append(selected, item)
		included[item.ID] = true
	}
	return selected
}

func (l *lab) intentPayload(cached *cachedPlan, question string) map[string]any {
	return l.intentPayloadWith(question, cached.objectNames, attributeDirectory(cached), nil, cached.roomIntro)
}

// intentPayloadWith 允许评测时覆盖对象/属性目录并带 history（只用于指代与省略消解）。
// room_intro 是方案直播间简介，意图理解必须带上。
func (l *lab) intentPayloadWith(question string, objectNames, attributes, history []string, roomIntro string) map[string]any {
	objects := []map[string]any{}
	for _, name := range objectNames {
		if strings.TrimSpace(name) != "" {
			objects = append(objects, map[string]any{"id": 0, "name": strings.TrimSpace(name)})
		}
	}
	historyPayload := []map[string]string{}
	for index, item := range history {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		historyPayload = append(historyPayload, map[string]string{"role": "customer", "text": item, "turn": strconv.Itoa(index + 1)})
	}
	return map[string]any{
		"question":          question,
		"room_intro":        strings.TrimSpace(roomIntro),
		"context_words":     map[string]any{"hits": []any{}},
		"objects":           objects,
		"stores":            []string{},
		"attributes":        attributes,
		"attribute_catalog": []any{},
		"history":           historyPayload,
	}
}

// ownerAllowed 把 ask 里点名的对象（“1号链接”“1号”）映射到对象目录；没点名返回 nil（不过滤）。
func ownerAllowed(subjects []string, names []string) map[string]bool {
	allowed := map[string]bool{}
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if name == subject || strings.Contains(name, subject) || strings.Contains(subject, name) {
				allowed[name] = true
			}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

// askOwnerFilter 在 ask 明确点名对象时返回该对象集合，否则 nil（不过滤）。
// 除了 subjects，还会从原文里识别“1号”“2号”这类简称。
func askOwnerFilter(ask intentAsk, names []string) map[string]bool {
	subjects := append([]string{}, ask.Subjects...)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.Contains(ask.Text, name) {
			subjects = append(subjects, name)
			continue
		}
		if index := strings.Index(name, "号"); index > 0 && strings.Contains(ask.Text, name[:index+len("号")]) {
			subjects = append(subjects, name)
		}
	}
	return ownerAllowed(subjects, names)
}

// isFollowUp 识别“2号的呢”“那个呢”这类省略式追问，召回时带上前一句。
func isFollowUp(text string) bool {
	trimmed := strings.TrimSpace(text)
	if len([]rune(trimmed)) > 10 {
		return false
	}
	return strings.HasSuffix(trimmed, "呢") || strings.Contains(trimmed, "那个") ||
		strings.Contains(trimmed, "这个") || strings.HasPrefix(trimmed, "那") || strings.HasPrefix(trimmed, "这")
}

// shippingTopic 是“与对象无关的配送类事实”：即使观众没点名商品，
// 这类事实也要参与召回（例如“仅限南充到店自提，不邮寄”）。
func shippingTopic(topic string) bool {
	for _, key := range []string{"配送", "发货", "邮寄", "快递", "核销", "提货", "包邮", "运费", "使用方式", "门店", "自取", "自提"} {
		if strings.Contains(topic, key) {
			return true
		}
	}
	return false
}

// expandDeliveryQuery 给“能卖吗/能发吗/能寄吗”这类笼统问法补上配送词，
// 否则 embedding 只看到“上海能卖吗”，召不回“不邮寄/到店自提”。
func expandDeliveryQuery(text string) string {
	trimmed := strings.TrimSpace(text)
	for _, explicit := range []string{"邮寄", "包邮", "发货", "快递", "配送", "自提", "自取", "到店"} {
		if strings.Contains(trimmed, explicit) {
			return trimmed
		}
	}
	trigger := false
	for _, marker := range []string{"能卖", "能买", "能发", "能寄", "能到", "能送", "可以卖", "可以买", "可以发", "可以寄", "卖吗", "买吗", "发吗", "寄吗", "到吗", "送吗", "有卖", "能拿到", "能收到"} {
		if strings.Contains(trimmed, marker) {
			trigger = true
			break
		}
	}
	if !trigger && strings.HasSuffix(trimmed, "吗") && (strings.Contains(trimmed, "能") || strings.Contains(trimmed, "可以")) {
		trigger = true
	}
	if !trigger {
		return trimmed
	}
	return trimmed + " 邮寄 发货 配送 快递 到店自提"
}

// recallAsk 对单个 ask 做 cosine 召回 + rerank，返回按相关性排序、按事实去重的候选。
// globalOnly 表示问题没点名对象：只取“全店通用”主索引 + 配送类事实，
// 避免把 2号/3号链接的口味、送礼对象等对象事实混进来。
// globalByAttributes 只取公共（全店通用）事实里命中所选属性的那些，给一份。
func (l *lab) globalByAttributes(ctx context.Context, cached *cachedPlan, query string, attributes []string) ([]candidate, error) {
	allowed := map[string]bool{}
	for _, attribute := range attributes {
		if attribute = strings.TrimSpace(attribute); attribute != "" {
			allowed[attribute] = true
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("属性为空")
	}
	vectors, err := l.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	scored := make([]candidate, 0, len(cached.facts))
	position := map[int64]int{}
	for index, item := range cached.entries {
		if !(item.global || item.owner == "全店通用") {
			continue
		}
		attribute := item.fact.attributeName()
		if !allowed[attribute] {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: "全局", Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: true,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// recallByAttributes 用属性（topic）过滤候选，再在过滤后的池子里做 cosine + rerank。
// 对象可为空：无对象时只按属性过滤（例如“上海可以买吗”只取购买/门店/配送类事实）。
func (l *lab) recallByAttributes(ctx context.Context, cached *cachedPlan, query, subject string, attributes []string, topK int, includeGlobal bool) ([]candidate, error) {
	allowed := map[string]bool{}
	for _, attribute := range attributes {
		if attribute = strings.TrimSpace(attribute); attribute != "" {
			allowed[attribute] = true
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("属性为空")
	}
	vectors, err := l.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	scored := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	for index, item := range cached.entries {
		attribute := item.fact.attributeName()
		if !allowed[attribute] {
			continue
		}
		if subject != "" && item.owner != subject && item.owner != "全店通用" {
			continue
		}
		if !includeGlobal && (item.global || item.owner == "全店通用") {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		scope := item.owner
		if item.global || scope == "全店通用" {
			scope = "全局"
		}
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: scope, Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	if l.reranker.Enabled() && len(scored) > 1 {
		docs := make([]string, len(scored))
		for index := range scored {
			docs[index] = scored[index].Doc
		}
		scores, err := l.reranker.Rerank(ctx, query, docs)
		if err == nil && len(scores) == len(scored) {
			for index := range scored {
				scored[index].RerankScore = math.Round(scores[index]*10000) / 10000
			}
			sort.SliceStable(scored, func(i, j int) bool {
				if scored[i].RerankScore == scored[j].RerankScore {
					return scored[i].Cos > scored[j].Cos
				}
				return scored[i].RerankScore > scored[j].RerankScore
			})
		}
	}
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// recallAcrossObjects 不按对象循环，直接在所有对象条目里按属性过滤、算一次余弦、只 rerank 一次。
func (l *lab) recallAcrossObjects(ctx context.Context, cached *cachedPlan, query string, attributes []string, topK int) ([]candidate, error) {
	allowed := map[string]bool{}
	for _, attribute := range attributes {
		if attribute = strings.TrimSpace(attribute); attribute != "" {
			allowed[attribute] = true
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("属性为空")
	}
	vectors, err := l.embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 {
		return nil, errors.New("问题向量化失败")
	}
	scored := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	for index, item := range cached.entries {
		if !allowed[item.fact.attributeName()] {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		scope := item.owner
		if item.global || scope == "全店通用" {
			scope = "全局"
		}
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(), Subject: f.Subject, Scope: scope, Conditions: f.Conditions, Exceptions: f.Exceptions, Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global, Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	if l.reranker.Enabled() && len(scored) > 1 {
		docs := make([]string, len(scored))
		for index := range scored {
			docs[index] = scored[index].Doc
		}
		if scores, err := l.reranker.Rerank(ctx, query, docs); err == nil && len(scores) == len(scored) {
			for index := range scored {
				scored[index].RerankScore = math.Round(scores[index]*10000) / 10000
			}
			sort.SliceStable(scored, func(i, j int) bool { return scored[i].RerankScore > scored[j].RerankScore })
		}
	}
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// normalizeAttributes maps unsupported model attributes to the active plan directory.
func (l *lab) normalizeAttributes(ctx context.Context, cached *cachedPlan, attributes []string) ([]string, []string) {
	directory := cached.attributeDirectory
	log.Printf("lab intent attributes tenant=%d plan=%d outside_catalog=%d",
		cached.plan.TenantID, cached.plan.ID, directory.CountUnknown(attributes))
	out := []string{}
	notes := []string{}
	unknown := []string{}
	for _, raw := range cleanStrings(attributes) {
		name := cached.attributeResolver.CanonicalName(raw)
		switch {
		case name == "":
			notes = append(notes, raw+"（已忽略）")
		case slices.Contains(directory.Names, name):
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
			if name != raw {
				notes = append(notes, raw+"→"+name)
			}
		default:
			unknown = append(unknown, raw)
		}
	}
	if len(unknown) == 0 {
		return out, notes
	}
	texts := append(append([]string{}, unknown...), directory.Names...)
	vectors, err := l.embed(ctx, texts)
	for index, raw := range unknown {
		best := ""
		score := .55
		if err == nil && len(vectors) == len(texts) {
			for offset, name := range directory.Names {
				value := semantic.CosineSimilarity(vectors[index], vectors[len(unknown)+offset])
				if value >= score {
					best, score = name, value
				}
			}
		}
		if best == "" {
			notes = append(notes, raw+"（已忽略）")
			continue
		}
		if !slices.Contains(out, best) {
			out = append(out, best)
		}
		notes = append(notes, raw+"→"+best)
	}
	return out, notes
}

// stepBackQuery 把具体问题抽象成更通用的检索问句，用于召回为空时的兜底。
func (l *lab) stepBackQuery(ctx context.Context, query string) (string, error) {
	system := "你是一个检索问题改写器。只输出一句更通用、更抽象的中文检索问句，保留对象、地区、数量等条件，去掉过于具体的属性措辞；不要解释，不要标点以外内容。"
	user := "原问题：" + query + "\n请输出向上抽象后的检索问句："
	raw, err := l.chat(ctx, l.chatMod, system, user, 200)
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"'“”")
	raw = strings.Split(raw, "\n")[0]
	return strings.TrimSpace(raw), nil
}

// policyExceptionFacts 对每个对象做“政策例外扫描”：把公共政策事实与该对象自己的
// 事实比相似度，取相似但可能结论不同的对象事实（例如公共“不邮寄”、5号“可邮寄”）。
func (l *lab) policyExceptionFacts(ctx context.Context, cached *cachedPlan, policyFacts []candidate, object string, minCos float64, limit int) []candidate {
	if len(policyFacts) == 0 || strings.TrimSpace(object) == "" {
		return nil
	}
	texts := make([]string, 0, len(policyFacts))
	for _, fact := range policyFacts {
		if text := strings.TrimSpace(fact.Doc); text != "" {
			texts = append(texts, text)
		} else {
			texts = append(texts, fact.Text)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	vectors, err := l.embed(ctx, texts)
	if err != nil || len(vectors) != len(texts) {
		return nil
	}
	type scoredFact struct {
		item  candidate
		score float64
	}
	scored := []scoredFact{}
	seen := map[int64]bool{}
	for index, entry := range cached.entries {
		if entry.owner != object || entry.global {
			continue
		}
		if index >= len(cached.vectors) || seen[entry.fact.ID] {
			continue
		}
		best := 0.0
		for _, vector := range vectors {
			if len(cached.vectors[index]) != len(vector) {
				continue
			}
			if score := semantic.CosineSimilarity(cached.vectors[index], vector); score > best {
				best = score
			}
		}
		if best < minCos {
			continue
		}
		f := entry.fact
		seen[f.ID] = true
		scored = append(scored, scoredFact{
			item: candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
				Subject: f.Subject, Scope: object, Conditions: f.Conditions, Exceptions: f.Exceptions,
				Cos: math.Round(best*10000) / 10000, Doc: entry.doc,
				Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0},
			score: best,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	out := []candidate{}
	for index, item := range scored {
		if index >= limit {
			break
		}
		out = append(out, item.item)
	}
	return out
}

// allObjectFacts 无属性约束时用：给该对象（含全局投影）的全部事实，不截断、
// 不再让另一对象抢名额。同一事实在本 ask 内只留一条。
func (l *lab) allObjectFacts(ctx context.Context, cached *cachedPlan, query, subject string, includeGlobal bool) ([]candidate, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("对象为空")
	}
	vectors, err := l.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	scored := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	for index, item := range cached.entries {
		if includeGlobal {
			if item.owner != subject && item.owner != "全店通用" {
				continue
			}
		} else {
			// 对比组：只取对象自己的事实，公共事实单独给一份。
			if item.owner != subject || item.global {
				continue
			}
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		scope := item.owner
		if item.global || scope == "全店通用" {
			scope = "全局"
		}
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: scope, Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// globalFacts 取方案里的公共事实（全店通用），对比组时只给一份。
func (l *lab) globalFacts(ctx context.Context, cached *cachedPlan, query string) ([]candidate, error) {
	vectors, err := l.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	scored := make([]candidate, 0, len(cached.facts))
	position := map[int64]int{}
	for index, item := range cached.entries {
		if !(item.global || item.owner == "全店通用") {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: "全局", Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: true,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// recallObjectFacts 对比组专用：只在该对象（含全局投影）的事实里召回，
// 避免一条 query 被同组另一个对象带偏；两边各取 topK，保证对比对称。
func (l *lab) recallObjectFacts(ctx context.Context, cached *cachedPlan, query, subject string, topK int) ([]candidate, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return l.recallAsk(ctx, cached, query, topK, nil, false)
	}
	vectors, err := l.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	// 对象自己的事实和全局共享事实分开收：对比时先保证对象属性覆盖，
	// 再补少量全局事实，避免全局副本把 20 个名额全占掉。
	own := make([]candidate, 0, len(cached.entries))
	global := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	globalPosition := map[int64]int{}
	for index, item := range cached.entries {
		if item.owner != subject && item.owner != "全店通用" {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		scope := item.owner
		if item.global || scope == "全店通用" {
			scope = "全局"
		}
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: scope, Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if scope == "全局" {
			if at, ok := globalPosition[f.ID]; ok {
				if current.Cos > global[at].Cos {
					global[at] = current
				}
				continue
			}
			globalPosition[f.ID] = len(global)
			global = append(global, current)
			continue
		}
		if at, ok := position[f.ID]; ok {
			if current.Cos > own[at].Cos {
				own[at] = current
			}
			continue
		}
		position[f.ID] = len(own)
		own = append(own, current)
	}
	sort.SliceStable(own, func(i, j int) bool { return own[i].Cos > own[j].Cos })
	sort.SliceStable(global, func(i, j int) bool { return global[i].Cos > global[j].Cos })
	// 同主题最多 2 条，避免一个主题（比如同一商品的多个卖点）占满对象名额。
	ownDiverse := make([]candidate, 0, len(own))
	topicCount := map[string]int{}
	for _, item := range own {
		if topicCount[item.Topic] >= 2 {
			continue
		}
		topicCount[item.Topic]++
		ownDiverse = append(ownDiverse, item)
	}
	own = ownDiverse
	ownLimit := topK - 2
	if ownLimit < 1 {
		ownLimit = topK
	}
	if len(own) > ownLimit {
		own = own[:ownLimit]
	}
	if len(global) > 2 {
		global = global[:2]
	}
	scored := append(append([]candidate{}, own...), global...)
	if len(scored) > topK {
		scored = scored[:topK]
	}
	if l.reranker.Enabled() && len(scored) > 1 {
		docs := make([]string, len(scored))
		for index := range scored {
			docs[index] = scored[index].Doc
		}
		scores, err := l.reranker.Rerank(ctx, query, docs)
		if err == nil && len(scores) == len(scored) {
			for index := range scored {
				scored[index].RerankScore = math.Round(scores[index]*10000) / 10000
			}
			sort.SliceStable(scored, func(i, j int) bool {
				if scored[i].RerankScore == scored[j].RerankScore {
					return scored[i].Cos > scored[j].Cos
				}
				return scored[i].RerankScore > scored[j].RerankScore
			})
		}
	}
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

func (l *lab) recallAsk(ctx context.Context, cached *cachedPlan, ask string, topK int, allowed map[string]bool, globalOnly bool) ([]candidate, error) {
	vectors, err := l.embed(ctx, []string{ask})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, errors.New("问题向量化返回不完整")
	}
	scored := make([]candidate, 0, len(cached.entries))
	position := map[int64]int{}
	for index, item := range cached.entries {
		if globalOnly && item.owner != "全店通用" && !shippingTopic(item.fact.Topic) {
			continue
		}
		if allowed != nil && !allowed[item.owner] {
			continue
		}
		score := 0.0
		if index < len(cached.vectors) && len(cached.vectors[index]) == len(vectors[0]) {
			score = semantic.CosineSimilarity(cached.vectors[index], vectors[0])
		}
		f := item.fact
		scope := item.owner
		if globalOnly && item.owner == "全店通用" {
			scope = "全局"
		}
		current := candidate{ID: f.ID, Topic: f.attributeName(), Text: f.displayText(),
			Subject: f.Subject, Scope: scope, Conditions: f.Conditions, Exceptions: f.Exceptions,
			Cos: math.Round(score*10000) / 10000, Doc: item.doc, OriginGlobal: item.global,
			Conditional: len(f.Conditions) > 0 || len(f.Exceptions) > 0}
		if at, ok := position[f.ID]; ok {
			if current.Cos > scored[at].Cos {
				scored[at] = current
			}
			continue
		}
		position[f.ID] = len(scored)
		scored = append(scored, current)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Cos > scored[j].Cos })
	if len(scored) > topK {
		scored = scored[:topK]
	}
	if l.reranker.Enabled() && len(scored) > 1 {
		docs := make([]string, len(scored))
		for index := range scored {
			docs[index] = scored[index].Doc
		}
		scores, err := l.reranker.Rerank(ctx, ask, docs)
		if err == nil && len(scores) == len(scored) {
			for index := range scored {
				scored[index].RerankScore = math.Round(scores[index]*10000) / 10000
			}
			sort.SliceStable(scored, func(i, j int) bool {
				if scored[i].RerankScore == scored[j].RerankScore {
					return scored[i].Cos > scored[j].Cos
				}
				return scored[i].RerankScore > scored[j].RerankScore
			})
		}
	}
	for index := range scored {
		scored[index].Rank = index + 1
		scored[index].CosRank = index + 1
	}
	return scored, nil
}

// naturalFactSentence 把入库用的自然语言 doc 直接交给回答模型，
// 只是去掉开头的归属前缀（归属已经在 facts 分组的 object 字段里）。
// 例如：“如果收货地区是川渝地区包装方式不抽真空”。
func naturalFactSentence(item candidate) string {
	text := strings.TrimSpace(item.Doc)
	if text == "" {
		return item.Text
	}
	for _, prefix := range []string{item.Subject, item.Scope, "全店通用"} {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	if text == "" {
		return item.Text
	}
	return text
}

func buildAnswerPayload(question, roomIntro string, intent intentResult, feed []candidate, location []string, regionBranch string) string {
	groups := map[string][]map[string]any{}
	order := []string{}
	for _, item := range feed {
		scope := item.Scope
		if scope == "" {
			scope = "全局"
		}
		text := naturalFactSentence(item)
		if _, ok := groups[scope]; !ok {
			order = append(order, scope)
		}
		groups[scope] = append(groups[scope], map[string]any{"ids": []int64{item.ID}, "text": text})
	}
	facts := []map[string]any{}
	// 程序确认的顾客条件放最前面，回答模型不用自己猜地区归属。
	if len(location) > 0 {
		text := "顾客收货地区：" + strings.Join(location, "、")
		if regionBranch != "" {
			text += "，命中地区分支：" + regionBranch
		}
		facts = append(facts, map[string]any{"object": "顾客条件", "facts": []map[string]any{{"ids": []int64{}, "text": text}}})
	}
	for _, scope := range order {
		facts = append(facts, map[string]any{"object": scope, "facts": groups[scope]})
	}
	customer := map[string]any{
		"speaker": "顾客", "source": "直播间弹幕", "original": question,
		"meaning": intent.Meaning, "uncertainties": intent.Uncertainties,
	}
	// 程序确认过的地区条件，直接告诉回答模型，避免它自己猜“上海属于川渝/其它地区”。
	if len(location) > 0 {
		conditions := map[string]any{"收货地区": strings.Join(location, "、")}
		if regionBranch != "" {
			conditions["地区分支"] = regionBranch
		}
		customer["customer_conditions"] = conditions
	}
	if len(intent.Asks) > 1 {
		asks := []map[string]string{}
		for _, ask := range intent.Asks {
			asks = append(asks, map[string]string{"text": ask.Text})
		}
		customer["asks"] = asks
	}
	payload := map[string]any{
		"facts":             facts,
		"qa_pairs":          []any{},
		"room_intro":        strings.TrimSpace(roomIntro),
		"context_words":     map[string]any{"hits": []any{}},
		"customer_question": customer,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

// speechText 从五字段回答里取出可播正文（话术，其次澄清句）。
func speechText(raw string) string {
	speech, clarify := "", ""
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		for _, key := range []string{"话术：", "话术:"} {
			if strings.HasPrefix(line, key) {
				speech = strings.TrimSpace(strings.TrimPrefix(line, key))
			}
		}
		for _, key := range []string{"澄清句：", "澄清句:"} {
			if strings.HasPrefix(line, key) {
				clarify = strings.TrimSpace(strings.TrimPrefix(line, key))
			}
		}
	}
	if speech != "" {
		return speech
	}
	return clarify
}
