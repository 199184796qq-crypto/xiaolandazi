package decisionexecutor

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
	"sync"
	"time"
)

const welcomeBenefitPoolTTL = 7 * 24 * time.Hour

type welcomeBenefitPool struct {
	Revision string            `json:"revision"`
	Items    []welcomeMaterial `json:"items"`
}

var welcomeBenefitPoolMu sync.Mutex
var welcomeBenefitPoolMemory = map[welcomeMaterialKey]welcomeBenefitPool{}

func welcomeBenefitRevision(claims []model.PlanMaterialClaim) string {
	rows := append([]model.PlanMaterialClaim(nil), claims...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	h := sha1.New()
	for _, c := range rows {
		if !c.IsBenefit || c.Status != "" && c.Status != "active" {
			continue
		}
		fmt.Fprintf(h, "%d|%s|%s|%s|%s|%t\n", c.ID, c.Content, c.SourceQuote, c.Conditions, c.Exceptions, c.IsBenefit)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func welcomeBenefitEntries(claims []model.PlanMaterialClaim) []welcomeMaterial {
	out := []welcomeMaterial{}
	for _, c := range claims {
		if !c.IsBenefit || c.Status != "" && c.Status != "active" {
			continue
		}
		if c.Destination != "" && c.Destination != "fact" {
			continue
		}
		if c.ScopeKind != "plan" && c.ScopeKind != "product" {
			continue
		}
		text := strings.TrimSpace(c.Content)
		if text == "" {
			continue
		}
		source := strings.TrimSpace(c.SourceQuote)
		if source == "" {
			source = text
		}
		condition := strings.TrimSpace(string(c.Conditions))
		if condition == "" {
			condition = strings.TrimSpace(string(c.Exceptions))
		}
		if condition == "" && welcomeConditionalText(text) {
			condition = "原文含条件，需按原文表述"
		}
		safe := !c.NeedsConfirmation && !c.NeedsSplit && len(c.Risks) == 0 && len(c.RiskFlags) == 0 && len(c.P0Hits) == 0 && condition == ""
		out = append(out, welcomeMaterial{ClaimID: c.ID, Kind: "offer", Topic: strings.TrimSpace(c.Topic), Text: text, Source: source, Condition: condition, Safe: safe, LinkID: c.ProductLinkID})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClaimID < out[j].ClaimID })
	return out
}

func welcomeBenefitPoolRedisKey(tenant, plan int64, revision string) string {
	return fmt.Sprintf("livecompanion:welcome-benefits:v1:%d:%d:%s", tenant, plan, revision)
}

func welcomeBenefitPointerKey(tenant, plan int64) string {
	return fmt.Sprintf("livecompanion:welcome-benefits:current:%d:%d", tenant, plan)
}

func (w *Worker) refreshWelcomeBenefitPool(ctx context.Context, tenant, plan int64) error {
	store, ok := w.store.(welcomeMaterialStore)
	if !ok || w.store == nil || plan <= 0 {
		return fmt.Errorf("welcome benefit pool: semantic library unavailable")
	}
	claims, err := store.ListPlanMaterialClaims(ctx, tenant, plan)
	if err != nil {
		return err
	}
	revision := welcomeBenefitRevision(claims)
	pool := welcomeBenefitPool{Revision: revision, Items: welcomeBenefitEntries(claims)}
	key := welcomeMaterialKey{tenant: tenant, plan: plan}
	welcomeBenefitPoolMu.Lock()
	welcomeBenefitPoolMemory[key] = pool
	welcomeBenefitPoolMu.Unlock()
	if w.benefitRedis == nil {
		return nil
	}
	raw, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	if err := w.benefitRedis.Set(ctx, welcomeBenefitPoolRedisKey(tenant, plan, revision), raw, welcomeBenefitPoolTTL).Err(); err != nil {
		return err
	}
	return w.benefitRedis.Set(ctx, welcomeBenefitPointerKey(tenant, plan), revision, welcomeBenefitPoolTTL).Err()
}

func (w *Worker) loadWelcomeBenefitPool(ctx context.Context, tenant, plan int64) []welcomeMaterial {
	key := welcomeMaterialKey{tenant: tenant, plan: plan}
	currentRevision := ""
	if store, ok := w.store.(welcomeMaterialStore); ok {
		if claims, err := store.ListPlanMaterialClaims(ctx, tenant, plan); err == nil {
			currentRevision = welcomeBenefitRevision(claims)
		}
	}
	welcomeBenefitPoolMu.Lock()
	if pool, ok := welcomeBenefitPoolMemory[key]; ok {
		welcomeBenefitPoolMu.Unlock()
		if currentRevision == "" || pool.Revision == currentRevision {
			return append([]welcomeMaterial(nil), pool.Items...)
		}
		if err := w.refreshWelcomeBenefitPool(ctx, tenant, plan); err == nil {
			welcomeBenefitPoolMu.Lock()
			items := append([]welcomeMaterial(nil), welcomeBenefitPoolMemory[key].Items...)
			welcomeBenefitPoolMu.Unlock()
			return items
		}
		return append([]welcomeMaterial(nil), pool.Items...)
	}
	welcomeBenefitPoolMu.Unlock()

	if w.benefitRedis != nil {
		if revision, err := w.benefitRedis.Get(ctx, welcomeBenefitPointerKey(tenant, plan)).Result(); err == nil && strings.TrimSpace(revision) != "" && (currentRevision == "" || revision == currentRevision) {
			if raw, err := w.benefitRedis.Get(ctx, welcomeBenefitPoolRedisKey(tenant, plan, revision)).Bytes(); err == nil {
				var pool welcomeBenefitPool
				if json.Unmarshal(raw, &pool) == nil && pool.Revision == revision {
					welcomeBenefitPoolMu.Lock()
					welcomeBenefitPoolMemory[key] = pool
					welcomeBenefitPoolMu.Unlock()
					return append([]welcomeMaterial(nil), pool.Items...)
				}
			}
		}
	}
	if err := w.refreshWelcomeBenefitPool(ctx, tenant, plan); err != nil {
		return nil
	}
	welcomeBenefitPoolMu.Lock()
	defer welcomeBenefitPoolMu.Unlock()
	return append([]welcomeMaterial(nil), welcomeBenefitPoolMemory[key].Items...)
}

// WarmWelcomeBenefitPool is called from plan/fact/benefit save hooks. It is
// idempotent when the semantic revision has not changed.
func (w *Worker) WarmWelcomeBenefitPool(ctx context.Context, tenant, plan int64) {
	if w == nil || plan <= 0 {
		return
	}
	if err := w.refreshWelcomeBenefitPool(ctx, tenant, plan); err != nil {
		// Keep the old pool on failure; never clear a working benefit source.
		return
	}
}

// Keep the compiler honest when the interaction package changes its envelope.
var _ = interactionplan.Protocol
