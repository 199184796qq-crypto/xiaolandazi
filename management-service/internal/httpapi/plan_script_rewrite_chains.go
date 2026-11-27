package httpapi

import (
	"context"
	"hash/fnv"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/scriptchain"
)

// 每场直播内复用：分析结果和已用链记录随场次失效，这里给 12 小时兜底。
var scriptChainStore = scriptchain.NewStore(scriptchain.NewMemoryKV(), 12*time.Hour)
var scriptAnalysisInFlight sync.Map

// SetScriptChainRedis configures both rewrite caches at startup.
func (s *Server) SetScriptChainRedis(client *redis.Client) {
	if client == nil {
		return
	}
	scriptChainStore = scriptchain.NewStore(scriptchain.NewRedisKV(client), 12*time.Hour)
	s.liveScriptReasoning.client = client
	planRewriteFacts.redis = client
	log.Printf("script chain cache backend: redis")
}

func clearSessionScriptChains(ctx context.Context, tenant, session int64) {
	if err := scriptChainStore.ClearSession(ctx, tenant, session); err != nil {
		log.Printf("script chain session cleanup failed tenant=%d session=%d: %v", tenant, session, err)
	}
}

func scriptChainPieceKey(piece string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(piece))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}

func (s *Server) analyzeScriptSample(ctx context.Context, sample string) (scriptchain.Analysis, error) {
	work, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	result, err := s.completePlayedScriptModel(work, agentgateway.Request{
		Stage: "script_analysis",
		Messages: []agentgateway.Message{
			{Role: "system", Content: scriptchain.AnalysisPrompt},
			{Role: "user", Content: "【样板话术】\n" + sample},
		},
		ResponseFormat:  agentgateway.ResponseText,
		MaxTokens:       8000,
		ReasoningEffort: "low",
		Timeout:         180 * time.Second,
	})
	if err != nil {
		return scriptchain.Analysis{}, err
	}
	return scriptchain.ParseAnalysis(result.Text)
}

// scriptChainRequirement 取这条话术片的逻辑链，随机选一条，返回写作要求段落。
func (s *Server) scriptChainRequirement(ctx context.Context, tenant, plan, session int64, sample, piece string) string {
	sample = strings.TrimSpace(sample)
	if sample == "" || plan <= 0 || session <= 0 {
		return ""
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	target := scriptchain.TargetLength(r)
	key := scriptchain.Piece{Tenant: tenant, Plan: plan, Session: session, ID: scriptChainPieceKey(piece), ContentHash: scriptchain.ContentHash(sample)}

	analysis, ok, err := scriptChainStore.Load(ctx, key)
	if err != nil {
		log.Printf("script chain cache read failed plan=%d piece=%s: %v", plan, piece, err)
		return scriptchain.DefaultRequirement(target)
	}
	if !ok {
		if _, analyzing := scriptAnalysisInFlight.LoadOrStore(key, true); analyzing {
			return scriptchain.DefaultRequirement(target)
		}
		defer scriptAnalysisInFlight.Delete(key)
		// Another worker can complete the first analysis before this one takes the lock.
		analysis, ok, err = scriptChainStore.Load(ctx, key)
		if err != nil {
			log.Printf("script chain cache read failed plan=%d piece=%s: %v", plan, piece, err)
			return scriptchain.DefaultRequirement(target)
		}
	}
	if !ok {
		started := time.Now()
		parsed, err := s.analyzeScriptSample(ctx, sample)
		if err != nil {
			log.Printf("script analysis failed plan=%d piece=%s chains=%d: %v", plan, piece, len(parsed.Chains), err)
			return scriptchain.DefaultRequirement(target)
		}
		analysis = parsed
		if err := checkScriptGenerationGuard(ctx); err != nil {
			return ""
		}
		if saveErr := scriptChainStore.Save(ctx, key, analysis); saveErr != nil {
			log.Printf("script chain cache write failed plan=%d piece=%s: %v", plan, piece, saveErr)
		}
		log.Printf("script analysis done plan=%d piece=%s chains=%d cost=%dms", plan, piece, len(analysis.Chains), time.Since(started).Milliseconds())
	}

	recent := scriptChainStore.RecentUsed(ctx, key)
	idx := scriptchain.Pick(len(analysis.Chains), recent, r)
	if idx < 0 {
		return scriptchain.DefaultRequirement(target)
	}
	chain := analysis.Chains[idx]
	scriptChainStore.MarkUsed(ctx, key, recent, idx)
	log.Printf("script chain picked plan=%d piece=%s chain=%s nodes=%d", plan, piece, chain.Name, len(chain.Nodes))
	return scriptchain.RenderRequirement(analysis, chain, target)
}
