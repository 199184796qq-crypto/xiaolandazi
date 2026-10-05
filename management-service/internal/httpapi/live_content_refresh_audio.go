package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
	"livecompanion/management/internal/ttsgateway"
)

// Select an approximate duration fraction at sentence boundaries. A single
// formal track must never turn a 25% refresh into a 100% track replacement.
func selectContentRefreshWindow(timeline []model.LiveAgentPlanTimelineSegment, percent, generation int) (int, int, error) {
	if percent < 1 || percent > 100 || len(timeline) == 0 {
		return 0, 0, errors.New("invalid content replacement percentage or timeline")
	}
	if percent < 100 && len(timeline) < (100+percent-1)/percent {
		return 0, 0, errors.New("正式声音的语义分段不足，不能安全执行局部更新")
	}
	var total int64
	for _, segment := range timeline {
		if segment.EndMS <= segment.StartMS {
			return 0, 0, errors.New("invalid sentence duration")
		}
		total += segment.EndMS - segment.StartMS
	}
	target := total * int64(percent) / 100
	if generation < 1 {
		generation = 1
	}
	offset := total * int64(((generation-1)*percent)%100) / 100
	start, cursor := 0, int64(0)
	for start < len(timeline)-1 && cursor+timeline[start].EndMS-timeline[start].StartMS <= offset {
		cursor += timeline[start].EndMS - timeline[start].StartMS
		start++
	}
	// Move a final short tail backwards to retain a full-sized replacement.
	for start > 0 && total-cursor < target {
		start--
		cursor -= timeline[start].EndMS - timeline[start].StartMS
	}
	end, duration := start, int64(0)
	for end < len(timeline) {
		next := duration + timeline[end].EndMS - timeline[end].StartMS
		if end > start && math.Abs(float64(next-target)) > math.Abs(float64(duration-target)) {
			break
		}
		duration = next
		end++
		if duration >= target {
			break
		}
	}
	if percent < 100 && end-start == len(timeline) {
		return 0, 0, errors.New("partial refresh would replace the entire recording")
	}
	return start, end, nil
}

// Recover unchanged PCM ranges from the registered asset, not an expiring URL.
// Rounded millisecond boundaries are mapped to complete samples exactly once.
func existingContentVoicePieces(raw []byte, timeline []model.LiveAgentPlanTimelineSegment) ([]generatedVoiceWAVPiece, error) {
	fmtData, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
	if err != nil {
		return nil, err
	}
	if len(fmtData) < 16 || binary.LittleEndian.Uint16(fmtData[:2]) != 1 {
		return nil, errors.New("局部更新只支持已归档PCM正式声音")
	}
	align := int64(binary.LittleEndian.Uint16(fmtData[12:14]))
	if align <= 0 || int64(len(pcm))%align != 0 {
		return nil, errors.New("invalid PCM alignment")
	}
	if len(timeline) == 0 || timeline[0].StartMS != 0 {
		return nil, errors.New("正式声音缺少完整时间轴")
	}
	pieces := make([]generatedVoiceWAVPiece, 0, len(timeline))
	previous := int64(0)
	for i, segment := range timeline {
		if i > 0 && segment.StartMS != timeline[i-1].EndMS {
			return nil, errors.New("正式声音时间轴不连续")
		}
		end := ((segment.EndMS*int64(byteRate) + 500) / 1000 / align) * align
		if i == len(timeline)-1 {
			actual := int64(len(pcm)) * 1000 / int64(byteRate)
			if math.Abs(float64(actual-segment.EndMS)) > 5 {
				return nil, errors.New("音频与时间轴长度不一致")
			}
			end = int64(len(pcm))
		}
		if end <= previous || end > int64(len(pcm)) {
			return nil, errors.New("invalid PCM timeline boundary")
		}
		pieces = append(pieces, generatedVoiceWAVPiece{text: segment.Text, fmtData: fmtData, pcmData: pcm[previous:end], byteRate: byteRate})
		previous = end
	}
	return pieces, nil
}

func generateContentRefreshText(ctx context.Context, generation model.LiveAgentFullShowGenerationContext, policyPrompt, before, oldText, after string, gateways ...anchorStyleCompleter) (string, error) {
	targetChars := utf8.RuneCountInString(oldText)
	if targetChars < 10 {
		targetChars = 10
	}
	refreshContext := generation
	refreshContext.VariantCount = 1
	refreshContext.RoundMinutes = max(1, (targetChars+249)/250)
	refreshContext.ExpansionPlans = speechexpander.BuildFixedPlans(speechexpander.Input{
		DurationMinutes: refreshContext.RoundMinutes,
		TargetChars:     targetChars,
		VariantCount:    1,
		FactKeys:        fullShowFactKeys(refreshContext),
		BenefitKeys:     fullShowBenefitKeys(refreshContext),
		LinkKeys:        fullShowLinkKeys(refreshContext),
	})
	topic := fmt.Sprintf(`这是正在运行的直播口播局部更新，只替换旧片段并与前后文自然衔接。
前文：%s
旧片段：%s
后文：%s
旧片段只用于理解原来的表达目的，不是事实来源；必须换角度和句式，不能提“更新”“上一版”等内部词。`, before, oldText, after)
	text, _, _, _, _, err := generateAnchorStyleTest(ctx, speechGenerationGateway(gateways), refreshContext, policyPrompt, topic, targetChars)
	if err != nil {
		return "", err
	}
	if err := auditContentRefreshText(generation, oldText, text); err != nil {
		return "", err
	}
	return text, nil
}

func auditContentRefreshText(generation model.LiveAgentFullShowGenerationContext, oldText, text string) error {
	oldN, newN := utf8.RuneCountInString(oldText), utf8.RuneCountInString(text)
	if newN < 10 || newN > 4000 || newN*100 < oldN*65 || newN*100 > oldN*135 {
		return errors.New("局部更新稿长度偏差过大，保留原成品")
	}
	if similarity := fullShowSimilarityPercent(oldText, text); similarity >= 72 {
		return fmt.Errorf("局部更新稿与原片段相似度%d%%，没有形成有效更新", similarity)
	}
	generation.AvoidRecent = true
	result := auditFullShowVariants(generation, []model.LiveAgentFullShowVariant{{Text: text}}, []string{oldText})
	if !result[0].Audit.Passed {
		return fmt.Errorf("局部更新稿未通过事实/重复检查: %v", result[0].Audit.Issues)
	}
	return nil
}

func contentTimelineText(timeline []model.LiveAgentPlanTimelineSegment) string {
	var b strings.Builder
	for _, s := range timeline {
		b.WriteString(s.Text)
	}
	return b.String()
}

func (s *Server) refreshContentVariant(ctx context.Context, tenantID, actorID, planID, roomID int64, generation model.LiveAgentFullShowGenerationContext, policyPrompt string, voice model.LiveAgentVoiceIdentity, original model.LiveAgentPlanVersionVariant, percent int, stillAllowed func() error) (model.LiveAgentPlanVersionVariant, error) {
	if err := stillAllowed(); err != nil {
		return original, err
	}
	start, end, err := selectContentRefreshWindow(original.Timeline, percent, original.GenerationNo)
	if err != nil {
		return original, err
	}
	asset, err := s.store.GetMediaAsset(ctx, tenantID, original.AudioAssetID)
	if err != nil {
		return original, err
	}
	if fmt.Sprint(asset.Metadata["purpose"]) != "live_agent_formal_voice_timeline" {
		return original, errors.New("仅AI正式成品允许自动更新；原始录音不会改写")
	}
	if err := validateContentRefreshVoiceIdentity(asset, voice); err != nil {
		return original, err
	}
	assetStore, err := s.assetStorage.For(asset.StorageDriver)
	if err != nil {
		return original, err
	}
	reader, err := assetStore.Open(ctx, asset.ObjectKey)
	if err != nil {
		return original, err
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxGeneratedVoiceBytes+1))
	reader.Close()
	if err != nil || len(raw) > maxGeneratedVoiceBytes {
		return original, errors.New("正式音频读取失败或超出大小限制")
	}
	pieces, err := existingContentVoicePieces(raw, original.Timeline)
	if err != nil {
		return original, err
	}
	oldText := contentTimelineText(original.Timeline[start:end])
	before := contentTimelineText(original.Timeline[max(0, start-2):start])
	after := contentTimelineText(original.Timeline[end:min(len(original.Timeline), end+2)])
	newText, err := generateContentRefreshText(ctx, generation, policyPrompt, before, oldText, after, s.speechGateway())
	if err != nil {
		return original, err
	}
	if err := stillAllowed(); err != nil {
		return original, err
	}
	voiceID, modelName, err := s.resolveFormalVoice(ctx, tenantID, fullShowVariantVoiceInput{Source: voice.Source, VoiceID: voice.VoiceID, ProfileID: voice.ProfileID})
	if err != nil {
		return original, err
	}
	// Never silently replace the published clone with a new profile revision.
	if voiceID != voice.VoiceID || modelName != voice.Model {
		return original, errors.New("已发布音色已变化，请重新确认并发布声音")
	}
	gateway := ttsgateway.NewFromEnv()
	newPieces := make([]generatedVoiceWAVPiece, 0)
	for _, text := range splitFullShowVoiceText(newText) {
		if err := stillAllowed(); err != nil {
			return original, err
		}
		generated, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Model: modelName, VoiceID: voiceID, Text: text, Rate: voice.Rate})
		if err != nil {
			return original, err
		}
		data, err := downloadGeneratedVoiceWAV(ctx, generated.AudioURL)
		if err != nil {
			return original, err
		}
		format, pcm, rate, err := parseGeneratedVoiceWAV(data)
		if err != nil {
			return original, err
		}
		if rate != pieces[0].byteRate || !bytes.Equal(format, pieces[0].fmtData) {
			return original, errors.New("新旧声音格式不一致，保留原成品")
		}
		newPieces = append(newPieces, generatedVoiceWAVPiece{text: text, fmtData: format, pcmData: pcm, byteRate: rate})
	}
	merged := append(append(append([]generatedVoiceWAVPiece{}, pieces[:start]...), newPieces...), pieces[end:]...)
	combined, timeline, duration, err := combineGeneratedVoiceWAV(original.VariantKey, merged)
	if err != nil {
		return original, err
	}
	if err := stillAllowed(); err != nil {
		return original, err
	}
	archive, err := s.archiveFullShowVoice(ctx, tenantID, actorID, planID, roomID, original.VariantKey, voiceID, modelName, voice.Source, combined, duration, timeline, voice.Rate)
	if err != nil {
		return original, err
	}
	result := original
	result.Text, result.Timeline, result.AudioDurationMS = contentTimelineText(timeline), timeline, duration
	result.AudioURL, result.AudioAssetID = archive.PreviewURL, archive.AssetID
	result.SRT, result.SafePoints, result.AssetManifest = archive.SRT, archive.SafePoints, archive.Manifest
	result.GenerationNo = max(1, original.GenerationNo) + 1
	result.EstimatedMinutes = int((duration + 59999) / 60000)
	result.Audit = model.LiveAgentFullShowAudit{Passed: true}
	return result, nil
}
