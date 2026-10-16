// Package anchorcycle schedules opening/body/closing runs. Only adopted
// speech advances the cursor or changes either memory or the macro style window.
package anchorcycle

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/contentstrategy"
	"livecompanion/management/internal/model"
)

func ID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

type Task struct {
	Common           bool           `json:"common,omitempty"`
	LectureTopic     int64          `json:"lecture_topic,omitempty"`
	LectureAngle     int            `json:"lecture_angle,omitempty"`
	LecturePurpose   string         `json:"lecture_purpose,omitempty"`
	LectureFacts     string         `json:"lecture_facts,omitempty"`
	LecturePlanJSON  string         `json:"lecture_plan_json,omitempty"`
	LecturePlanSHA   string         `json:"lecture_plan_sha,omitempty"`
	FactScope        *TaskFactScope `json:"fact_scope,omitempty"`
	PointWeight      int            `json:"point_weight,omitempty"`
	AngleWeight      int            `json:"angle_weight,omitempty"`
	ID               string         `json:"id,omitempty"`
	LinkKey          string         `json:"link_key,omitempty"`
	FocusLink        string         `json:"focus_link,omitempty"`
	AllowedLinksJSON string         `json:"allowed_links_json,omitempty"`
	ProductMode      string         `json:"product_mode,omitempty"`
	ProductMaxChars  int            `json:"product_max_chars,omitempty"`
	SellingPoint     string         `json:"selling_point"`
	Angle            string         `json:"angle"`
	Key              string         `json:"key"`
	SlotKey          string         `json:"slot_key,omitempty"` // 跨场复用稳定键：LinkKey×卖点ID×角度序号，不受措辞变化影响
	Tier             string         `json:"tier,omitempty"`
	TargetAudience   string         `json:"target_audience,omitempty"`
	PainPoint        string         `json:"pain_point,omitempty"`
}

// schedulingKey is owned by the program. Expansion and run initialization must
// use the same identity; SlotKey has a separate cache lifetime and is unchanged.
func (t Task) schedulingKey() string {
	if t.ID != "" {
		return t.ID
	}
	if t.LinkKey != "" {
		return t.LinkKey + "×" + t.SellingPoint + "×" + t.Angle
	}
	return t.SellingPoint + "×" + t.Angle
}

type Config struct {
	AngleRefreshAfterPlays int          `json:"angle_refresh_after_plays,omitempty"`
	AllowConsecutiveRepeat bool         `json:"allow_consecutive_repeat,omitempty"`
	TextProfileID          string       `json:"text_profile_id,omitempty"`
	WorkspaceID            string       `json:"workspace_id"`
	Region                 string       `json:"region"`
	AgentID                string       `json:"agent_id"`
	EnvironmentID          string       `json:"environment_id"`
	CredentialID           string       `json:"credential_profile_id"`
	FeedbackID             string       `json:"feedback_profile_id"`
	Threshold              int          `json:"threshold"`
	MaxRetries             int          `json:"max_retries"`
	MaxChars               int          `json:"max_chars"`
	PointGap               int          `json:"point_gap"`
	RepairPunctuation      bool         `json:"repair_punctuation"`
	Rolling                bool         `json:"rolling"`
	BodySegments           int          `json:"body_segments"`
	BodyMin                int          `json:"body_min,omitempty"`
	BodyMax                int          `json:"body_max,omitempty"`
	CombinationGap         int          `json:"combination_gap"`
	MinReappearCycles      int          `json:"min_reappear_cycles"`  // 同一卖点×角度至少隔 N 轮再出现；0 表示不启用轮级冷却
	MaxCycles              int          `json:"max_cycles"`           // Zero means until explicitly stopped.
	FreshWindowMinutes     int          `json:"fresh_window_minutes"` // 开播前 N 分钟全实时；0 表示不启用话术复用
	ReuseEveryN            int          `json:"reuse_every_n"`        // 混合期每 N 段 1 段实时，其余复用旧话术；0 或 1 表示每段都实时
	MaxReuse               int          `json:"max_reuse"`            // 单段话术最多复用次数，达到后淘汰
	TTS                    *VoiceConfig `json:"tts,omitempty"`
	ComplianceScope        string       `json:"compliance_scope,omitempty"`
}
type VoiceConfig struct {
	Source          string  `json:"source"`
	VoiceID         string  `json:"voice_id"`
	ProfileID       int64   `json:"profile_id,omitempty"`
	Model           string  `json:"model,omitempty"`
	Rate            float64 `json:"rate"`
	BufferSeconds   int     `json:"buffer_seconds"`
	CacheTTLSeconds int     `json:"cache_ttl_seconds,omitempty"`
}
type VoiceAsset struct {
	PreviewReservedMS int64                             `json:"preview_reserved_ms,omitempty"`
	PreviewTruncated  bool                              `json:"preview_truncated,omitempty"`
	JobID             string                            `json:"job_id,omitempty"`
	Status            string                            `json:"status"`
	Error             string                            `json:"error,omitempty"`
	LatencyMS         int64                             `json:"latency_ms"`
	CacheID           string                            `json:"cache_id,omitempty"`
	CacheHit          bool                              `json:"cache_hit,omitempty"`
	CacheExpiresAt    string                            `json:"cache_expires_at,omitempty"`
	Bundle            model.LiveAgentPlanVersionVariant `json:"bundle"`
}
type P0Allowance struct {
	RuleID      string `json:"rule_id"`
	MatchedText string `json:"matched_text"`
	MatchKey    string `json:"match_key"`
	Decision    string `json:"decision"`
}
type Candidate struct {
	LibraryEntryID          string                                `json:"library_entry_id,omitempty"`
	LibraryDecision         string                                `json:"library_decision,omitempty"`
	LibraryTextSHA          string                                `json:"library_text_sha,omitempty"`
	FinalContentAudit       *FinalContentAudit                    `json:"final_content_audit,omitempty"`
	LectureAudit            *LectureAudit                         `json:"lecture_audit,omitempty"`
	ExpressionAudit         *LectureAudit                         `json:"expression_audit,omitempty"`
	ExactDuplicateRemovals  int                                   `json:"exact_duplicate_removals,omitempty"`
	AngleHook               *AngleHook                            `json:"selected_angle_hook,omitempty"`
	HookCalculations        []ComputedHookCalculation             `json:"hook_calculations,omitempty"`
	AnglePlan               *AnglePlan                            `json:"angle_plan,omitempty"`
	FissionSide             string                                `json:"fission_side,omitempty"`
	OpeningInstructionEdits []contentguard.OpeningInstructionEdit `json:"opening_instruction_edits,omitempty"`
	P0CheckedInputSHA       string                                `json:"p0_checked_input_sha,omitempty"`
	P0ApprovedTextSHA       string                                `json:"p0_approved_text_sha,omitempty"`
	ContentWarnings         []string                              `json:"content_warnings,omitempty"`
	ProductAudit            *ProductAudit                         `json:"product_audit,omitempty"`
	ID                      string                                `json:"id"`
	Attempt                 int                                   `json:"attempt"`
	Text                    string                                `json:"text"`
	RawText                 string                                `json:"raw_text,omitempty"`
	OutputText              string                                `json:"output_text,omitempty"`
	PunctuationEdits        []PunctuationEdit                     `json:"punctuation_edits,omitempty"`
	ScoreScope              string                                `json:"score_scope,omitempty"`
	PostprocessMS           int64                                 `json:"postprocess_ms,omitempty"`
	Prompt                  string                                `json:"prompt"`
	Score                   anchorstylescore.Result               `json:"score"`
	GenerationMS            int64                                 `json:"generation_ms"`
	EventIDs                []string                              `json:"event_ids"`
	RetryFeedback           string                                `json:"retry_feedback,omitempty"`
	CreatedAt               string                                `json:"created_at"`
	PriceCheck              PriceCheck                            `json:"price_check,omitempty"`
	ContentIssue            string                                `json:"content_issue,omitempty"`
	CacheID                 string                                `json:"cache_id,omitempty"`
	P0Report                *contentguard.Report                  `json:"p0_report,omitempty"`
	P0Patch                 *contentguard.Repair                  `json:"p0_patch,omitempty"`
	P0PatchError            string                                `json:"p0_patch_error,omitempty"`
	P0RewriteNumber         int                                   `json:"p0_rewrite_number,omitempty"`
}
type Segment struct {
	LectureSkipped bool                           `json:"lecture_skipped,omitempty"`
	Phase          string                         `json:"phase"`
	CycleNumber    int                            `json:"cycle_number,omitempty"`
	OpeningVersion string                         `json:"opening_version,omitempty"`
	OpeningHook    *OpeningHook                   `json:"opening_hook,omitempty"`
	Task           Task                           `json:"task"`
	Selected       Candidate                      `json:"selected"`
	Attempts       []Candidate                    `json:"attempts"`
	Reason         string                         `json:"reason"`
	Summary        string                         `json:"summary"`
	MacroReport    anchorstylescore.RollingReport `json:"macro_report"`
	Feedback       json.RawMessage                `json:"feedback,omitempty"`
	Voice          *VoiceAsset                    `json:"voice,omitempty"`
}
type Use struct {
	Count       int    `json:"count"`
	LastIndex   int    `json:"last_index"`
	LastCycle   int    `json:"last_cycle,omitempty"` // 上次出现的轮号，用于轮级冷却
	LastSummary string `json:"last_summary,omitempty"`
}
type SpeechPoolEntry struct {
	Text       string `json:"text"`
	Key        string `json:"key"`
	ReuseCount int    `json:"reuse_count"`
	LastIndex  int    `json:"last_index"`
}
type State struct {
	LibrarySourceSHA             string                         `json:"library_source_sha,omitempty"`
	FinalContentReviewRequired   bool                           `json:"final_content_review_required,omitempty"`
	FinalContentReviewContextSHA string                         `json:"final_content_review_context_sha256,omitempty"`
	LatestExpressionConstraints  []map[string]any               `json:"latest_expression_constraints,omitempty"`
	Lectures                     *LectureRuntime                `json:"lectures,omitempty"`
	AnglePendingUses             []AnglePlayedUse               `json:"angle_pending_uses,omitempty"`
	AngleProgress                map[string]*AngleUseProgress   `json:"angle_use_progress,omitempty"`
	AngleRegisteredTracks        map[string]bool                `json:"angle_registered_tracks,omitempty"`
	AngleTrackBindings           map[string]AngleTrackBinding   `json:"angle_track_bindings,omitempty"`
	AngleReceiptSequence         uint64                         `json:"angle_receipt_sequence,omitempty"`
	AnglePlans                   map[string]*AnglePlan          `json:"angle_plans,omitempty"`
	AnglePlanMotherSHA           string                         `json:"angle_plan_mother_sha,omitempty"`
	TextModel                    *agentgateway.SpeechModel      `json:"text_model_snapshot,omitempty"`
	ReviewModel                  *agentgateway.SpeechModel      `json:"review_model_snapshot,omitempty"`
	TextPromptSHA                string                         `json:"text_prompt_sha256,omitempty"`
	GenerationSkips              []GenerationSkip               `json:"generation_skips,omitempty"`
	ContentStrategy              *contentstrategy.Snapshot      `json:"content_strategy,omitempty"`
	ProductFocus                 *ProductFocus                  `json:"product_focus,omitempty"`
	AcceptedOffset               int                            `json:"accepted_offset,omitempty"`
	Delivery                     *MainlineDelivery              `json:"mainline_delivery,omitempty"`
	Surface                      string                         `json:"surface,omitempty"`
	PreviewLimitMS               int64                          `json:"preview_limit_ms,omitempty"`
	StreamControlled             bool                           `json:"stream_controlled,omitempty"`
	DerivedSafetyAssetID         int64                          `json:"derived_safety_asset_id,omitempty"`
	Version                      int                            `json:"version"`
	RunID                        string                         `json:"run_id"`
	Revision                     int64                          `json:"revision"`
	TenantID                     int64                          `json:"tenant_id"`
	PlanID                       int64                          `json:"plan_id"`
	RoomID                       int64                          `json:"room_id"`
	Phase                        string                         `json:"phase"`
	Status                       string                         `json:"status"`
	Cursor                       int                            `json:"cursor"`
	TaskIndex                    int                            `json:"task_index"`
	OpeningCount                 int                            `json:"opening_count"`
	PendingOpeningHook           *OpeningHook                   `json:"pending_opening_hook,omitempty"`
	RecentOpeningHookIDs         []string                       `json:"recent_opening_hook_ids,omitempty"`
	OpeningHookLibraryMD         string                         `json:"opening_hook_library_md,omitempty"`
	OpeningHookLibrarySource     string                         `json:"opening_hook_library_source,omitempty"`
	OpeningHookLibraryAssetID    int64                          `json:"opening_hook_library_asset_id,omitempty"`
	CycleNumber                  int                            `json:"cycle_number"`
	BodyCount                    int                            `json:"body_count"`
	BodyTarget                   int                            `json:"body_target,omitempty"`
	PriceHook                    *PriceHook                     `json:"pending_price_hook,omitempty"`
	Queue                        []Task                         `json:"queue"`
	Config                       Config                         `json:"config"`
	Session                      json.RawMessage                `json:"session"`
	SessionID                    string                         `json:"session_id"`
	BootstrapSent                bool                           `json:"bootstrap_sent,omitempty"`
	StyleID                      int64                          `json:"style_asset_id"`
	ScorecardID                  int64                          `json:"scorecard_asset_id"`
	WheelID                      int64                          `json:"wheel_asset_id"`
	StyleMD                      string                         `json:"style_md"`
	Facts                        string                         `json:"facts"`
	FissionSides                 []string                       `json:"fission_sides,omitempty"`
	PointSchedules               map[string]*WeightedSchedule   `json:"point_schedules,omitempty"`
	ContentSchedule              *WeightedSchedule              `json:"content_schedule,omitempty"`
	ContentAdopted               map[string]int                 `json:"content_adopted,omitempty"`
	PendingContentKind           string                         `json:"pending_content_kind,omitempty"`
	PointUses                    map[string]int                 `json:"point_uses,omitempty"`
	ScheduledAngleUses           map[string]int                 `json:"scheduled_angle_uses,omitempty"`
	SideUses                     map[string]map[string]int      `json:"side_uses,omitempty"`
	FactSideMap                  map[string][]string            `json:"fact_side_map,omitempty"`
	BaselineJSON                 string                         `json:"baseline_json"`
	Attempts                     []Candidate                    `json:"attempts"`
	Accepted                     []Segment                      `json:"accepted"`
	Used                         map[string]Use                 `json:"used_combinations"`
	SpeechPool                   []SpeechPoolEntry              `json:"speech_pool,omitempty"`
	DuplicateSkippedKeys         map[string]bool                `json:"duplicate_skipped_keys,omitempty"`
	RecentSummaries              []string                       `json:"recent_summaries"`
	StyleWindow                  string                         `json:"style_window"`
	PendingFeedback              string                         `json:"pending_feedback"`
	MacroReport                  anchorstylescore.RollingReport `json:"macro_report"`
	FeedbackPending              bool                           `json:"feedback_pending"`
	LastFeedbackFingerprint      string                         `json:"last_feedback_fingerprint"`
	LastFeedbackIndex            int                            `json:"last_feedback_index"`
	Busy                         bool                           `json:"busy"`
	Uncertain                    bool                           `json:"uncertain"`
	Warnings                     []string                       `json:"warnings"`
	Error                        string                         `json:"error,omitempty"`
	CreatedAt                    string                         `json:"created_at"`
	P0PolicyMD                   string                         `json:"p0_policy_md,omitempty"`
	P0PolicySource               string                         `json:"p0_policy_source,omitempty"`
	P0MaxRewrites                int                            `json:"p0_max_rewrites,omitempty"`
	P0RewritesUsed               int                            `json:"p0_rewrites_used,omitempty"`
	P0PendingRepair              *contentguard.Repair           `json:"p0_pending_repair,omitempty"`
	P0Skipped                    []P0Skip                       `json:"p0_skipped,omitempty"`
	P0SkippedKeys                map[string]bool                `json:"p0_skipped_keys,omitempty"`
	P0ConsecutiveSkips           int                            `json:"p0_consecutive_skips,omitempty"`
	P0AuditPending               *Candidate                     `json:"p0_audit_pending,omitempty"`
	P0Allowances                 []P0Allowance                  `json:"p0_allowances,omitempty"`
}

// Ephemeral handoff receipts are stored with this run in Redis only.
type MainlineDelivery struct {
	SourceFactsSHA        string   `json:"source_facts_sha256"`
	SourceWheelSHA        string   `json:"source_wheel_sha256"`
	CurrentTrackDurations []int    `json:"current_track_durations,omitempty"`
	VersionID             int64    `json:"version_id"`
	RuntimeSessionID      int64    `json:"runtime_session_id"`
	ProgramID             string   `json:"program_id"`
	Generation            uint64   `json:"generation"`
	PendingJobID          string   `json:"pending_job_id,omitempty"`
	PendingCandidates     []string `json:"pending_candidates,omitempty"`
	DeliveredCandidates   []string `json:"delivered_candidates,omitempty"`
}

func New(tasks []Task, cfg Config) (State, error) {
	if cfg.AngleRefreshAfterPlays < 0 || cfg.AngleRefreshAfterPlays > 100 {
		return State{}, errors.New("角度推演更新次数须为1到100，留空默认3次")
	}
	if len(tasks) > 10000 || cfg.Threshold < 0 || cfg.Threshold > 100 || cfg.MaxRetries < 0 || cfg.MaxRetries > 5 || cfg.MaxChars < 80 || cfg.MaxChars > 1000 || cfg.PointGap < 0 || cfg.PointGap > 100 || cfg.CombinationGap < 0 || cfg.CombinationGap > 500 || cfg.BodySegments < 0 || cfg.BodySegments > 100 || cfg.MaxCycles < 0 || cfg.MaxCycles > 1000 {
		return State{}, errors.New("大循环参数无效")
	}
	seen := map[string]bool{}
	for i := range tasks {
		if strings.TrimSpace(tasks[i].SellingPoint) == "" || strings.TrimSpace(tasks[i].Angle) == "" {
			return State{}, errors.New("卖点角度不能为空")
		}
		tasks[i].Key = tasks[i].schedulingKey()
		if seen[tasks[i].Key] {
			return State{}, errors.New("固定队列存在重复卖点角度")
		}
		seen[tasks[i].Key] = true
	}
	if cfg.Rolling {
		if cfg.BodyMin == 0 && cfg.BodyMax == 0 && cfg.BodySegments == 0 {
			cfg.BodyMin, cfg.BodyMax = 2, 6
		}
		if cfg.BodyMin != 0 || cfg.BodyMax != 0 {
			if cfg.BodyMin < 2 || cfg.BodyMax > 6 || cfg.BodyMin > cfg.BodyMax {
				return State{}, errors.New("中间讲解范围须在2到6段之间，最少不能超过最多")
			}
		}
		if cfg.BodyMin == 0 && (cfg.BodySegments < 2 || cfg.BodySegments > 6) {
			return State{}, errors.New("中间讲解段数须为2到6")
		}
		if len(tasks) == 0 || (cfg.BodySegments == 0 && cfg.BodyMin == 0) {
			return State{}, errors.New("滚动模式须有卖点池，每轮中间段数至少为1")
		}
	} else {
		var err error
		tasks, err = Arrange(tasks, cfg.PointGap)
		if err != nil {
			return State{}, err
		}
	}
	phase := "opening"
	if len(tasks) == 0 {
		phase = "closing"
	}
	s := State{Version: 3, RunID: ID(), Phase: phase, Status: "ready", CycleNumber: 1, Queue: tasks, Config: cfg, Attempts: []Candidate{}, Accepted: []Segment{}, Used: map[string]Use{}, RecentSummaries: []string{}, Warnings: []string{}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if cfg.Rolling {
		s.planBodyTarget()
	}
	return s, nil
}

// Freeze one target per cycle, not per request. A stable run/round hash spreads
// rounds across the configured range; retries and Redis reloads never reroll it.
func (s *State) planBodyTarget() {
	if s.Config.BodyMin == 0 {
		s.BodyTarget = s.Config.BodySegments // Existing fixed-length runs stay fixed.
		return
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(s.RunID + ":" + strconv.Itoa(s.CycleNumber)))
	width := s.Config.BodyMax - s.Config.BodyMin + 1
	target := s.Config.BodyMin + int(h.Sum32()%uint32(width))
	if width > 1 && target == s.BodyTarget {
		target = s.Config.BodyMin + (target-s.Config.BodyMin+1)%width
	}
	s.BodyTarget = target
}
func (s *State) BodyLimit() int {
	if s.BodyTarget > 0 {
		return s.BodyTarget
	}
	return s.Config.BodySegments
}

// Plan spacing once, then freeze the queue. Generation retries never consume
// a slot or reschedule the queue. The opening retains the first pool task.
func Arrange(tasks []Task, gap int) ([]Task, error) {
	if gap == 0 || len(tasks) < 2 {
		return append([]Task{}, tasks...), nil
	}
	groups := map[string][]Task{}
	order := []string{}
	for _, task := range tasks {
		if _, ok := groups[task.SellingPoint]; !ok {
			order = append(order, task.SellingPoint)
		}
		groups[task.SellingPoint] = append(groups[task.SellingPoint], task)
	}
	queue := []Task{tasks[0]}
	groups[tasks[0].SellingPoint] = groups[tasks[0].SellingPoint][1:]
	for len(queue) < len(tasks) {
		blocked := map[string]bool{}
		for _, task := range queue[max(0, len(queue)-gap):] {
			blocked[task.SellingPoint] = true
		}
		chosen := ""
		for _, point := range order {
			if blocked[point] || len(groups[point]) == 0 {
				continue
			}
			if chosen == "" || len(groups[point]) > len(groups[chosen]) {
				chosen = point
			}
		}
		if chosen == "" {
			return nil, errors.New("卖点池无法按当前间隔排成固定队列，请扩充卖点或降低同卖点间隔")
		}
		queue = append(queue, groups[chosen][0])
		groups[chosen] = groups[chosen][1:]
	}
	return queue, nil
}
func (s *State) Task() Task {
	if s.Lectures != nil && s.Lectures.Active != nil {
		return *s.Lectures.Active
	}
	if s.PriceHook != nil {
		return s.focusTask(Task{Common: s.PriceHook.Task.Common, FactScope: s.PriceHook.Task.FactScope, SellingPoint: "塑价兑现", Angle: s.PriceHook.Task.SellingPoint + " × " + s.PriceHook.Task.Angle, Key: "塑价兑现×" + s.PriceHook.Task.Key})
	}
	if s.Phase == "closing" || (!s.Config.Rolling && s.Cursor >= len(s.Queue)) {
		task := s.focusTask(Task{SellingPoint: "本轮收口", Angle: "重报核心信息并自然承接", Key: "本轮收口"})
		for i := len(s.Accepted) - 1; i >= 0; i-- {
			prior := s.Accepted[i].Task
			if prior.FocusLink == task.FocusLink {
				task.FactScope = prior.FactScope
				break
			}
		}
		return task
	}
	if s.Config.Rolling {
		return s.focusTask(s.Queue[s.TaskIndex])
	}
	return s.focusTask(s.Queue[s.Cursor])
}

func (s *State) OpeningVersion() string {
	if s.OpeningCount == 0 {
		return "完整版"
	}
	return "接新版"
}

// pointUsedCounts 统计每个卖点下所有角度的累计使用次数，供卖点级加权调度使用。
// 商品级卖点池展开的 task 带 LinkKey，按「商品链接×卖点名」隔离统计，同名卖点跨商品互不干扰。
func (s *State) pointUsedCounts() map[string]int {
	counts := map[string]int{}
	for _, task := range s.Queue {
		key := task.SellingPoint
		if task.LinkKey != "" {
			key = productSellingPointKey(task.LinkKey, task.SellingPoint)
		}
		counts[key] += s.Used[task.Key].Count
	}
	return counts
}

// tierWeight 返回卖点 tier 的默认权重：核心最高、次级中、信任福利最低。
// 供节奏规划按 tier 加权出镜，核心卖点讲得多、信任福利讲得少；同级平级。
func tierWeight(tier string) int {
	switch tier {
	case "core":
		return 3
	case "secondary":
		return 2
	case "trust":
		return 1
	}
	return 1
}

// pointWeight 返回卖点权重。优先用运营/母版配置的 PointWeights（手动覆盖）；
// 未配置时按卖点 tier 默认权重（core > secondary > trust），缺省 1。
// 配合 pointUsedCounts 让各卖点按权重占比分布，而不是"角度多的卖点霸屏"。
// 商品级卖点池展开的 task 带 LinkKey，权重键为「商品链接×卖点名」，同名卖点按商品隔离。
func (s *State) pointWeight(task Task) int {
	if s.ProductFocus != nil && s.ProductFocus.Policy.PointWeights != nil {
		if task.LinkKey != "" {
			if w, ok := s.ProductFocus.Policy.PointWeights[productSellingPointKey(task.LinkKey, task.SellingPoint)]; ok && w > 0 {
				return w
			}
		}
		if w, ok := s.ProductFocus.Policy.PointWeights[task.SellingPoint]; ok && w > 0 {
			return w
		}
	}
	return tierWeight(task.Tier)
}

// angleWeight 返回角度（卖点×角度）权重，缺省 1。让同一卖点下的角度也按权重分布，
// 而不是"角度最少使用优先"的均匀轮换。key 与 Used 一致，为「卖点名×角度名」，
// 商品级卖点池展开时带商品前缀「商品链接×卖点名×角度名」。
func (s *State) angleWeight(key string) int {
	if s.ProductFocus != nil && s.ProductFocus.Policy.AngleWeights != nil {
		if w, ok := s.ProductFocus.Policy.AngleWeights[key]; ok && w > 0 {
			return w
		}
	}
	return 1
}

// taskPointKey 返回任务在卖点级加权与避重里使用的键：商品级卖点池带 LinkKey 时
// 用「商品链接×卖点名」隔离，否则用卖点名。
func taskPointKey(task Task) string {
	if task.LinkKey != "" {
		return productSellingPointKey(task.LinkKey, task.SellingPoint)
	}
	return task.SellingPoint
}

// Called only after adoption. Candidates and retries keep a frozen TaskIndex.
// Gap N means at least N adopted paragraphs between occurrences; closings are
// paragraphs too. Reusing a task never clears its old count or last expression.
func (s *State) chooseNext() {
	s.EnsureProductFocus()
	if s.Config.Rolling {
		s.prepareContentSelection()
		if s.PriceHook != nil || s.chooseProductWeightedTask() {
			return
		}
		if s.Surface == "formal_live" && len(s.P0SkippedKeys) > 0 {
			s.P0SkippedKeys = map[string]bool{}
			if s.chooseProductWeightedTask() {
				return
			}
		}
		s.Status, s.Error = "spacing_blocked", "当前商品没有可用卖点×角度，已保留本场记录"
		if len(s.P0SkippedKeys) > 0 {
			s.Status, s.Error = "compliance_blocked", "本场可用卖点×角度已被P0全部跳过，已暂停；不无限生成"
		}
		s.FeedbackPending = false
		return
	}
	blockedPoints := map[string]bool{}
	for _, segment := range s.Accepted[max(0, len(s.Accepted)-s.Config.PointGap):] {
		if segment.Phase != "closing" {
			blockedPoints[taskPointKey(segment.Task)] = true
		}
	}
	chosen := -1
	pointCounts := s.pointUsedCounts()
	for i, task := range s.Queue {
		if !s.productTaskEligible(task) {
			continue
		}
		if s.P0SkippedKeys[task.Key] {
			continue
		}
		u := s.Used[task.Key]
		if blockedPoints[taskPointKey(task)] || (u.Count > 0 && s.TotalAccepted()-u.LastIndex < s.Config.CombinationGap) {
			continue
		}
		// 轮级冷却：同一卖点×角度至少隔 MinReappearCycles 轮才能再次出现。
		// 与 CombinationGap（段内防连讲）互补，控制跨轮的重复频率。
		if u.Count > 0 && s.Config.MinReappearCycles > 0 && s.CycleNumber-u.LastCycle < s.Config.MinReappearCycles {
			continue
		}
		if chosen < 0 {
			chosen = i
			continue
		}
		prior := s.Used[s.Queue[chosen].Key]
		// 三级加权调度：卖点权重 → 角度权重 → 最近使用。
		// 卖点按 PointWeights 占比（缺省等频），避免角度多的卖点霸屏；
		// 同卖点内角度按 AngleWeights 占比（缺省等频）；再按最近使用兜底。
		curPoint := float64(pointCounts[taskPointKey(task)]) / float64(s.pointWeight(task))
		priorPoint := float64(pointCounts[taskPointKey(s.Queue[chosen])]) / float64(s.pointWeight(s.Queue[chosen]))
		curAngle := float64(u.Count) / float64(s.angleWeight(task.Key))
		priorAngle := float64(prior.Count) / float64(s.angleWeight(s.Queue[chosen].Key))
		if curPoint < priorPoint ||
			(curPoint == priorPoint && curAngle < priorAngle) ||
			(curPoint == priorPoint && curAngle == priorAngle && u.LastIndex < prior.LastIndex) {
			chosen = i
		}
	}
	if chosen < 0 && s.Surface == "formal_live" {
		// Spacing and previously rejected prose cannot stop a live producer.
		// Pick the next eligible task and generate fresh text; never revive a rejected draft.
		for offset := 1; offset <= len(s.Queue); offset++ {
			i := (s.TaskIndex + offset) % len(s.Queue)
			if s.productTaskEligible(s.Queue[i]) && !s.P0SkippedKeys[s.Queue[i].Key] {
				chosen = i
				break
			}
		}
		if chosen < 0 {
			s.P0SkippedKeys = map[string]bool{}
			for offset := 1; offset <= len(s.Queue); offset++ {
				i := (s.TaskIndex + offset) % len(s.Queue)
				if s.productTaskEligible(s.Queue[i]) {
					chosen = i
					break
				}
			}
		}
		if chosen >= 0 {
			s.Status, s.Error, s.TaskIndex = "ready", "", chosen
			return
		}
	}
	if chosen < 0 {
		available := false
		for _, task := range s.Queue {
			if !s.productTaskEligible(task) {
				continue
			}
			if !s.P0SkippedKeys[task.Key] {
				available = true
				break
			}
		}
		if !available && len(s.P0SkippedKeys) > 0 {
			s.Status = "compliance_blocked"
			s.Error = "本场可用卖点×角度已被P0全部跳过，已暂停；不无限生成"
			s.FeedbackPending = false
			return
		}
		if s.Surface == "generation_preview" {
			// The customer preview is capped by actual audio duration. When a
			// small pool cannot satisfy the normal long-lived spacing window,
			// reuse the least-used safe task with the existing reuse prompt so
			// the voice queue can still reach two minutes without adding silence.
			for i, task := range s.Queue {
				if !s.productTaskEligible(task) {
					continue
				}
				if s.P0SkippedKeys[task.Key] {
					continue
				}
				if chosen < 0 || s.Used[task.Key].Count < s.Used[s.Queue[chosen].Key].Count || (s.Used[task.Key].Count == s.Used[s.Queue[chosen].Key].Count && s.Used[task.Key].LastIndex < s.Used[s.Queue[chosen].Key].LastIndex) {
					chosen = i
				}
			}
			if chosen >= 0 {
				s.Status, s.Error, s.TaskIndex = "ready", "", chosen
				s.Warn("试听卖点池不足，已使用不同表达继续补足2分钟声音")
				return
			}
		}
		s.Status = "spacing_blocked"
		s.Error = "卖点池无法满足当前避重间隔，已保留采纳稿；请扩充池或结束本场后降低间隔，不会自动短期复读"
		s.FeedbackPending = false
		return
	}
	s.TaskIndex = chosen
}

func excerpt(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
func Window(text string) string {
	return anchorstylescore.TailStyleChars(text, 1000)
}
func (s *State) Warn(message string) {
	s.Warnings = append(s.Warnings, message)
	if len(s.Warnings) > 30 {
		s.Warnings = s.Warnings[len(s.Warnings)-30:]
	}
}

// AddCandidate never adds rejected drafts to memory, nor evaluates a macro
// window before adoption. At the cap, select the best of all attempts, including
// the initial attempt; equal scores keep the earlier version.
func (s *State) AddCandidate(c Candidate, b anchorstylescore.Baseline) (bool, error) {
	if s.Phase == "ended" || s.Status == "spacing_blocked" || s.Status == "content_blocked" || s.Status == "compliance_blocked" || strings.TrimSpace(c.Text) == "" {
		return false, errors.New("当前环节不能采纳空稿或已结束")
	}
	c.Attempt = len(s.Attempts) + 1
	if err := s.CheckP0Candidate(&c); err != nil {
		return false, err
	}
	if c.P0Patch != nil {
		s.P0RewritesUsed++
	}
	if c.P0Report != nil && !c.P0Report.Passed {
		// 局部重写格式错误（模型未返回合法 replacements JSON）时，重写本身已失败，
		// 不消耗重写预算反复重试（会拖长主线断供），直接按「耗尽」处理：删词兜底，删不掉跳过本段。
		if c.P0PatchError != "" {
			s.P0RewritesUsed = s.P0MaxRewrites
		}
		// 局部重写已耗尽仍违规：先尝试程序删词兜底（替代直接跳过该段）。
		if s.P0RewritesUsed >= s.P0MaxRewrites {
			if repaired, ok := s.deleteRepair(c); ok {
				c.Text = repaired
				c.OutputText = repaired
				c.P0Patch = nil
				c.P0PatchError = ""
				c.ScoreScope = "p0_delete_repaired"
				if err := s.CheckP0Candidate(&c); err != nil {
					return false, err
				}
				if c.P0Report == nil || !c.P0Report.Passed {
					s.rejectP0Candidate(c)
					return false, nil
				}
				// 删词成功且再过 P0，落到下方正常采纳流程。
			} else {
				s.rejectP0Candidate(c)
				return false, nil
			}
		} else {
			s.rejectP0Candidate(c)
			return false, nil
		}
	}
	if (s.Task().ProductMode == "lecture" || len(s.EffectiveExpressionConstraints()) > 0) && !s.LectureCandidateApproved(c) {
		s.Attempts = append(s.Attempts, c)
		s.SkipGeneration("lecture_scope_review", "讲解正文事实复核未通过")
		return false, nil
	}
	if !s.FinalContentCandidateApproved(c) {
		audit := c.FinalContentAudit
		// 审核模型明确判违规（有问题）→ 先局部重写换说法，重写耗尽才跳过。
		// 回执失效/技术错误（Decision 为空、Error 非空）保持跳过，由审核链路单独处理。
		if audit != nil && audit.Error == "" && audit.Decision == "有问题" && s.P0RewritesUsed < s.P0MaxRewrites {
			s.rejectReviewCandidate(c, audit.Reason)
			return false, nil
		}
		s.Attempts = append(s.Attempts, c)
		s.SkipGeneration("final_content_review", "最终文字事实与要求审核未通过或回执失效")
		return false, nil
	}
	s.P0PendingRepair = nil
	// Non-compliance writing requirements are feedback, never fatal approval gates.
	c.PriceCheck = InspectPrice(c.Text, s.Task())
	if issue := s.openingHookProblem(c.Text); issue != "" {
		c.ContentWarnings = append(c.ContentWarnings, issue)
	}
	if issue := s.priceObligationProblem(c.PriceCheck); issue != "" {
		c.ContentWarnings = append(c.ContentWarnings, issue)
	}
	s.Attempts = append(s.Attempts, c)
	if s.Surface == "formal_live" && c.Score.OverallScore < s.Config.Threshold && len(s.Attempts) > s.Config.MaxRetries {
		s.SkipGeneration("style_generation", "生成重试后仍未达到本场风格标尺")
		return false, nil
	}
	if c.Score.OverallScore < s.Config.Threshold && len(s.Attempts) <= s.Config.MaxRetries {
		return false, nil
	}
	selected := c
	reason := "passed"
	if c.Score.OverallScore < s.Config.Threshold {
		reason = "retry_limit_best"
		selected = s.Attempts[0]
		for _, candidate := range s.Attempts[1:] {
			if candidate.ContentIssue == "" && (selected.ContentIssue != "" || candidate.Score.OverallScore > selected.Score.OverallScore) {
				selected = candidate
			}
		}
	}
	// A style retry may not revive a higher-scoring draft that failed an explicit
	// next-segment promise. Invalid candidates remain in the trace only.
	if selected.ContentIssue != "" || !s.FinalContentCandidateApproved(selected) {
		selected = c
	}
	task := s.Task()
	wasLecture := task.ProductMode == "lecture"
	wasHook := s.PriceHook != nil
	s.PriceHook = nil
	if selected.PriceCheck.Hook != "" && !wasLecture {
		s.PriceHook = &PriceHook{Task: task, Promise: selected.PriceCheck.Hook, SourceText: excerpt(selected.Text, 400), CreatedIndex: s.TotalAccepted() + 1, DueIndex: s.TotalAccepted() + 2}
	}
	edges := anchorstylescore.AnalyzeRolling([]string{selected.Text}, 1, b, s.StyleMD).Edges
	summary := task.Key
	if len(edges) > 0 {
		summary += "；开头：" + edges[0].Opening + "；结尾：" + edges[0].Ending
	}
	openingVersion := ""
	if s.Phase == "opening" {
		openingVersion = s.OpeningVersion()
	}
	s.Accepted = append(s.Accepted, Segment{Phase: s.Phase, CycleNumber: s.CycleNumber, OpeningVersion: openingVersion, Task: task, Selected: selected, Attempts: s.Attempts, Reason: reason, Summary: summary})
	if !wasLecture && selected.FissionSide != "" {
		s.recordFissionSide(task.Key, selected.FissionSide)
	}
	s.RegisterLectureTrack(selected, task, s.Phase)
	if !wasLecture {
		s.recordProductAdoption(task, spokenCandidate(selected))
		if s.Phase == "body" && !wasHook {
			s.recordScheduledAdoption(task)
		}
		if s.Phase == "body" && task.ProductMode != "auxiliary" {
			s.AddToSpeechPool(spokenCandidate(selected), task.Key)
		}
	}
	if wasLecture {
		s.Lectures.Active = nil
		s.Phase = "opening"
		s.Attempts = nil
		s.P0RewritesUsed = 0
		s.P0ConsecutiveSkips = 0
		s.PendingFeedback = ""
		s.FeedbackPending = false
		return true, nil
	}
	if s.Phase == "opening" && s.PendingOpeningHook != nil {
		s.Accepted[len(s.Accepted)-1].OpeningHook = s.PendingOpeningHook
		s.RecentOpeningHookIDs = append(s.RecentOpeningHookIDs, s.PendingOpeningHook.ID)
		s.RecentOpeningHookIDs = s.RecentOpeningHookIDs[max(0, len(s.RecentOpeningHookIDs)-s.PendingOpeningHook.RecentCount):]
		s.PendingOpeningHook = nil
	}
	if s.Phase != "closing" && !wasHook && !wasLecture {
		u := s.Used[task.Key]
		u.Count++
		u.LastIndex = s.TotalAccepted()
		u.LastCycle = s.CycleNumber
		u.LastSummary = excerpt(selected.Text, 180)
		s.Used[task.Key] = u
		s.Cursor++
	}
	if s.Phase != "closing" && wasHook && s.Config.Rolling {
		s.Cursor++ // A continuation is a paragraph, not another pool use.
	}
	if s.Phase == "opening" {
		s.OpeningCount++
	}
	if s.Phase == "body" && task.ProductMode != "auxiliary" {
		s.BodyCount++
	}
	s.RecentSummaries = append(s.RecentSummaries, summary)
	s.RecentSummaries = s.RecentSummaries[max(0, len(s.RecentSummaries)-3):]
	s.StyleWindow = Window(s.StyleWindow + "\n\n" + selected.Text)
	recent := []string{}
	start := max(0, len(s.Accepted)-5)
	for _, segment := range s.Accepted[start:] {
		recent = append(recent, segment.Selected.Text)
	}
	s.MacroReport = anchorstylescore.AnalyzeMacro1000(s.StyleWindow, recent, s.AcceptedOffset+start+1, b, s.StyleMD)
	s.Accepted[len(s.Accepted)-1].MacroReport = s.MacroReport
	s.Attempts = []Candidate{}
	s.P0RewritesUsed = 0
	s.P0ConsecutiveSkips = 0
	s.PendingFeedback = ""
	s.FeedbackPending = true
	if s.Config.Rolling {
		if task.ProductMode == "auxiliary" {
			s.ProductFocus.AuxiliaryActive = false
			s.Phase = "opening"
			s.chooseNext()
		} else if s.Phase == "closing" {
			if s.Config.MaxCycles > 0 && s.CycleNumber >= s.Config.MaxCycles {
				s.Phase = "ended"
				s.Status = "completed"
				s.FeedbackPending = false
			} else {
				s.CycleNumber++
				s.BodyCount = 0
				s.planBodyTarget()
				s.Phase = "opening"
				s.chooseNext()
			}
		} else if s.BodyCount >= s.BodyLimit() {
			// A hook in the final planned body paragraph may extend this round
			// once, but never past the user's hard maximum. At maximum, the
			// closing itself resolves it before taking any other task.
			if s.PriceHook != nil && s.Config.BodyMax > s.BodyCount {
				s.BodyTarget = s.BodyCount + 1
				s.Phase = "body"
			} else {
				s.Phase = "closing"
			}
		} else {
			s.Phase = "body"
			if s.PriceHook == nil {
				s.chooseNext()
			}
		}
	} else if s.Phase == "closing" {
		s.Phase = "ended"
		s.Status = "completed"
		s.FeedbackPending = false
	} else if s.Cursor >= len(s.Queue) {
		s.Phase = "closing"
	} else {
		s.Phase = "body"
	}
	return true, nil
}

// Decisions continue to be measured every adoption; repeated identical macro
// advice is spaced by two accepted segments, avoiding cumulative overcorrection.
func (s *State) ShouldFeedback() bool {
	if !s.FeedbackPending || s.Phase == "ended" || len(s.MacroReport.Issues) == 0 {
		return false
	}
	keys := []string{}
	for _, issue := range s.MacroReport.Issues {
		metric, _, _ := strings.Cut(issue.Actual, "：")
		keys = append(keys, issue.Key+":"+metric+":"+issue.Target)
	}
	fingerprint := strings.Join(keys, "|")
	if fingerprint == s.LastFeedbackFingerprint && s.TotalAccepted()-s.LastFeedbackIndex < 2 {
		return false
	}
	s.LastFeedbackFingerprint = fingerprint
	s.LastFeedbackIndex = s.TotalAccepted()
	return true
}
