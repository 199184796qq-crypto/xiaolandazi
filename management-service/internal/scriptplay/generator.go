package scriptplay

// 话术生成器接口：现在只返回原文，改写后续单独实现。
//
// 外层（版本池、缓存、播放）只依赖这个接口，不关心内部是否改写过，
// 所以以后接改写时只换实现，不动播放代码。

// Generated 是一次生成的产物。
type Generated struct {
	Text string
	Kind Kind
}

// Generator 把片段原文变成"这次要合成的文本"。
type Generator interface {
	Generate(pieceText string) (Generated, error)
}

// OriginalGenerator 原样返回原文。当前生效的实现。
type OriginalGenerator struct{}

// Generate 返回原文，类型为原始。
func (OriginalGenerator) Generate(pieceText string) (Generated, error) {
	return Generated{Text: pieceText, Kind: KindOriginal}, nil
}

// GenerationRequest 描述"这次该生成什么"。
type GenerationRequest struct {
	Kind Kind
	Seq  int
}

// PlanGeneration 决定本次生成应该出原始还是改写。
//
// 规则来自设计：没有原始 → 出原始；已有原始 → 出改写。
// 已经到版本上限时返回 ok=false，调用方停止生成。
func PlanGeneration(pool Pool, voiceID string, cap int) (GenerationRequest, bool) {
	if !pool.CanGenerate(voiceID, cap) {
		return GenerationRequest{}, false
	}
	return GenerationRequest{Kind: pool.NextKind(voiceID), Seq: pool.NextSeq(voiceID)}, true
}

// PlanFallbackGeneration 是原始生成失败时的兜底：改生成改写，不留空。
//
// 若该音色下已有原始版本，直接返回 nil（本来就该出改写，无需兜底）。
func PlanFallbackGeneration(pool Pool, voiceID string, cap int) *GenerationRequest {
	if pool.HasOriginal(voiceID) || !pool.CanGenerate(voiceID, cap) {
		return nil
	}
	req := GenerationRequest{Kind: KindRewrite, Seq: pool.NextSeq(voiceID)}
	return &req
}

// NewVersion 按生成结果和请求组装一条版本记录。
func NewVersion(pieceID, fingerprint, voiceID, model string, volume int, req GenerationRequest, audioRef string) Version {
	return Version{
		PieceID:     pieceID,
		Fingerprint: fingerprint,
		VoiceID:     voiceID,
		Model:       model,
		Volume:      volume,
		Kind:        req.Kind,
		Seq:         req.Seq,
		AudioRef:    audioRef,
	}
}

// VoiceReady 判断某个音色是否已经"每个片段至少有一个版本"，也就是可以切过去。
func VoiceReady(pools map[string]Pool, voiceID string) (ready, total int) {
	total = len(pools)
	for _, pool := range pools {
		if len(pool.VoiceVersions(voiceID)) > 0 {
			ready++
		}
	}
	return ready, total
}
