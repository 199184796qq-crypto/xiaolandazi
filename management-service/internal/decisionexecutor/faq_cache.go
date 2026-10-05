package decisionexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const maxFAQCacheBuckets = 512

type faqPolicyStore interface {
	GetLiveContentPolicy(context.Context, int64, int64) (model.LiveContentPolicyRecord, error)
}

// This cache is deliberately disposable: restart/leader failover regenerates answers.
// Neither a cache read nor an audio refresh extends a text variant's generation TTL.
type faqAnswerCache struct {
	mu      sync.Mutex
	buckets map[string][]faqAnswerVariant
	recent  map[string][]faqRecentUse
}

type faqRecentUse struct {
	Text  string
	Until time.Time
}

type faqAnswerVariant struct {
	Text        string
	GeneratedAt time.Time
	ExpiresAt   time.Time
	LastUsedAt  time.Time
	AudioKey    string
	AudioURL    string
	AudioUntil  time.Time
}

type faqCacheTicket struct {
	Key         string
	Policy      model.LiveContentPolicy
	GeneratedAt time.Time
	ExpiresAt   time.Time
	Text        string
	Hit         bool
	Recent      []string
}

func faqCacheEligible(item decisionItem) bool {
	if item.ManualOrigin != "" || item.PreviewInstruction != "" || item.FixedText != "" ||
		strings.EqualFold(item.ExecutionMode, "verbatim") || item.faqPersonalized ||
		(len(item.Nicknames) != 0 && !item.faqAddressingResolved) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "reply_question", "reply_chat", "":
	default:
		return false
	}
	return len(item.SampleQuestions) == 1 && strings.TrimSpace(item.SampleQuestions[0]) != ""
}

// Only exact, context-free paraphrases are folded. Product numbers, quantities,
// audience qualifiers and broad semantic topic buckets must never be collapsed.
func faqPromptItem(item decisionItem) decisionItem {
	if !faqCacheEligible(item) {
		return item
	}
	raw := strings.TrimSpace(item.SampleQuestions[0])
	plain := strings.Trim(raw, " ?？。！!")
	canonical := ""
	switch plain {
	case "多少钱", "价格多少", "价格是多少":
		canonical = "多少钱"
	case "发什么快递", "用什么快递", "发的是哪个快递":
		canonical = "发什么快递"
	case "什么时候发货", "啥时候发货":
		canonical = "什么时候发货"
	}
	if canonical == "" {
		return item
	}
	item.SampleQuestions = []string{canonical}
	if strings.TrimSpace(item.Summary) == raw {
		item.Summary = canonical
	}
	if strings.TrimSpace(item.Title) == raw {
		item.Title = canonical
	}
	return item
}

func faqDigest(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func (w *Worker) prepareFAQCache(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, systemPrompt, prompt string) *faqCacheTicket {
	if w.faqCache == nil || !faqCacheEligible(item) {
		return nil
	}
	source, ok := w.store.(faqPolicyStore)
	if !ok {
		return nil
	}
	record, err := source.GetLiveContentPolicy(ctx, session.TenantID, session.RoomID)
	if err == nil {
		err = record.Policy.Validate()
	}
	if err != nil {
		// Fail closed for reuse, not for fresh answers. A policy outage cannot
		// keep serving a formerly valid cached response indefinitely.
		log.Printf("faq cache policy unavailable tenant=%d room=%d: %v", session.TenantID, session.RoomID, err)
		return nil
	}
	industry, l1, l2, l3, err := w.store.LoadLivePolicyLayers(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return nil
	}
	key := faqDigest([]any{session.TenantID, session.RoomID, session.ID, session.ExecutionRealm,
		record.Policy, record.Revision, record.SystemRevision, industry, l1, l2, l3,
		systemPrompt, prompt, w.store.AgentPromptValue(ctx, "policy.runtime.execution", ""),
		w.store.AgentPromptValue(ctx, "live.final_review.system", "")})
	now := w.now()
	_, expiry := record.Policy.ContentExpiry("faq", now)
	ticket := &faqCacheTicket{Key: key, Policy: record.Policy, GeneratedAt: now, ExpiresAt: expiry}
	w.faqCache.mu.Lock()
	defer w.faqCache.mu.Unlock()
	w.faqCache.prune(now)
	variants := w.faqCache.buckets[key]
	for _, use := range w.faqCache.recent[key] {
		ticket.Recent = append(ticket.Recent, use.Text)
	}
	selected := -1
	for i, variant := range variants {
		if now.Sub(variant.LastUsedAt) < time.Duration(record.Policy.MinRepeatSeconds)*time.Second {
			continue
		}
		if selected < 0 || variant.LastUsedAt.Before(variants[selected].LastUsedAt) {
			selected = i
		}
	}
	// Fill the server-configured variant pool lazily; no speculative paid requests.
	if len(variants) >= record.Policy.FAQVariantCount && selected >= 0 {
		variant := &variants[selected]
		variant.LastUsedAt = now
		ticket.GeneratedAt, ticket.ExpiresAt = variant.GeneratedAt, variant.ExpiresAt
		ticket.Text, ticket.Hit = variant.Text, true
	}
	return ticket
}

func (c *faqAnswerCache) prune(now time.Time) {
	for key, uses := range c.recent {
		valid := uses[:0]
		for _, use := range uses {
			if now.Before(use.Until) {
				valid = append(valid, use)
			}
		}
		if len(valid) == 0 {
			delete(c.recent, key)
		} else {
			c.recent[key] = valid
		}
	}
	for key, variants := range c.buckets {
		valid := variants[:0]
		for _, variant := range variants {
			if now.Before(variant.ExpiresAt) {
				valid = append(valid, variant)
			}
		}
		if len(valid) == 0 {
			delete(c.buckets, key)
			delete(c.recent, key)
		} else {
			c.buckets[key] = valid
		}
	}
}

func (w *Worker) rememberFAQText(ticket *faqCacheTicket, text string) error {
	if ticket == nil {
		return nil
	}
	for _, previous := range ticket.Recent {
		if strings.TrimSpace(previous) == strings.TrimSpace(text) {
			return fmt.Errorf("常见问题回答仍与冷却期内的旧版本相同，暂不重复播出")
		}
	}
	now := w.now()
	if !now.Before(ticket.ExpiresAt) {
		return nil
	}
	w.faqCache.mu.Lock()
	defer w.faqCache.mu.Unlock()
	w.faqCache.prune(now)
	if w.faqCache.recent == nil {
		w.faqCache.recent = make(map[string][]faqRecentUse)
	}
	// Retain cooldowns for evicted variants too, otherwise a small pool could
	// alternate endlessly between two identical answers within the cooldown.
	if ticket.Policy.MinRepeatSeconds > 0 {
		uses := w.faqCache.recent[ticket.Key]
		until := now.Add(time.Duration(ticket.Policy.MinRepeatSeconds) * time.Second)
		found := false
		for i := range uses {
			if uses[i].Text == text {
				uses[i].Until, found = until, true
				break
			}
		}
		if !found {
			if len(uses) >= 128 {
				return fmt.Errorf("常见问题生成过于频繁，等待已有版本冷却后再播出")
			}
			uses = append(uses, faqRecentUse{Text: text, Until: until})
		}
		w.faqCache.recent[ticket.Key] = uses
	}
	variants := w.faqCache.buckets[ticket.Key]
	for i := range variants {
		if variants[i].Text == text {
			// A reviewer returning the same text is not a new generation.
			variants[i].LastUsedAt = now
			ticket.Text = text
			ticket.GeneratedAt, ticket.ExpiresAt = variants[i].GeneratedAt, variants[i].ExpiresAt
			return nil
		}
	}
	if len(variants) >= ticket.Policy.FAQVariantCount {
		oldest := 0
		for i := range variants {
			if variants[i].GeneratedAt.Before(variants[oldest].GeneratedAt) {
				oldest = i
			}
		}
		variants = append(variants[:oldest], variants[oldest+1:]...)
	}
	if len(w.faqCache.buckets) >= maxFAQCacheBuckets && len(variants) == 0 {
		oldestKey := ""
		var oldest time.Time
		for key, items := range w.faqCache.buckets {
			if oldestKey == "" || items[0].GeneratedAt.Before(oldest) {
				oldestKey, oldest = key, items[0].GeneratedAt
			}
		}
		delete(w.faqCache.buckets, oldestKey)
		delete(w.faqCache.recent, oldestKey)
	}
	w.faqCache.buckets[ticket.Key] = append(variants, faqAnswerVariant{
		Text: text, GeneratedAt: ticket.GeneratedAt, ExpiresAt: ticket.ExpiresAt, LastUsedAt: now,
	})
	ticket.Text = text
	return nil
}

func (w *Worker) synthesizeFAQAudio(ctx context.Context, item *decisionItem, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	ticket := item.faqCacheTicket
	key := faqDigest(request) // Includes exact text, provider/model, voice, speed and emotion.
	if ticket != nil {
		w.faqCache.mu.Lock()
		for _, variant := range w.faqCache.buckets[ticket.Key] {
			if variant.Text == request.Text && variant.AudioKey == key && w.now().Before(variant.AudioUntil) && w.now().Before(variant.ExpiresAt) {
				w.faqCache.mu.Unlock()
				return ttsgateway.SynthesizeResponse{AudioURL: variant.AudioURL, Provider: request.Provider, Model: request.Model, VoiceID: request.VoiceID, Rate: request.Rate}, nil
			}
		}
		w.faqCache.mu.Unlock()
	}
	response, err := w.tts.SynthesizeURL(ctx, request)
	if err != nil || ticket == nil || strings.TrimSpace(response.AudioURL) == "" {
		return response, err
	}
	until := faqAudioExpiry(response.AudioURL, ticket.ExpiresAt)
	if !w.now().Before(until) {
		return response, nil
	}
	w.faqCache.mu.Lock()
	defer w.faqCache.mu.Unlock()
	variants := w.faqCache.buckets[ticket.Key]
	for i := range variants {
		if variants[i].Text == request.Text {
			variants[i].AudioKey, variants[i].AudioURL, variants[i].AudioUntil = key, response.AudioURL, until
			break
		}
	}
	return response, nil
}

// Signed URL lifetimes may be shorter than the FAQ lifetime. Unknown query-token
// formats are deliberately not cached; known signatures retain a 60s safety margin.
func faqAudioExpiry(rawURL string, contentExpiry time.Time) time.Time {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return time.Time{}
	}
	if u.RawQuery == "" {
		return contentExpiry
	}
	query := u.Query()
	var expiry time.Time
	for _, field := range []string{"Expires", "expires"} {
		if value := query.Get(field); value != "" {
			seconds, err := strconv.ParseInt(value, 10, 64)
			if err != nil || seconds <= 0 {
				return time.Time{}
			}
			expiry = time.Unix(seconds, 0)
		}
	}
	for _, prefix := range []string{"X-Amz-", "x-oss-"} {
		dateKey, expiresKey := prefix+"Date", prefix+"Expires"
		if prefix == "x-oss-" {
			dateKey, expiresKey = "x-oss-date", "x-oss-expires"
		}
		if date := query.Get(dateKey); date != "" {
			start, err := time.Parse("20060102T150405Z", date)
			seconds, secondsErr := strconv.ParseInt(query.Get(expiresKey), 10, 64)
			if err != nil || secondsErr != nil || seconds <= 0 || seconds > 604800 {
				return time.Time{}
			}
			expiry = start.Add(time.Duration(seconds) * time.Second)
		}
	}
	if expiry.IsZero() {
		return time.Time{}
	}
	expiry = expiry.Add(-time.Minute)
	if expiry.Before(contentExpiry) {
		return expiry
	}
	return contentExpiry
}
