package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"livecompanion/management/internal/audiocuts"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type scriptFirstCachedBlock struct {
	AssetID     int64                                `json:"asset_id"`
	StartSample int64                                `json:"start_sample"`
	EndSample   int64                                `json:"end_sample"`
	DurationMS  int64                                `json:"duration_ms"`
	Text        string                               `json:"text"`
	Timeline    []model.LiveAgentPlanTimelineSegment `json:"timeline"`
}
type scriptFirstBlockIndex struct {
	Version           int                      `json:"version"`
	Key               string                   `json:"key"`
	ParentAssetID     int64                    `json:"parent_asset_id"`
	SourceSHA         string                   `json:"source_sha256"`
	UntimedTranscript bool                     `json:"untimed_transcript,omitempty"`
	Blocks            []scriptFirstCachedBlock `json:"blocks"`
}

func validScriptFirstBlockIndex(index scriptFirstBlockIndex, key string, audio volcengineCachedAudio) bool {
	if index.Version != 1 || index.Key != key || index.ParentAssetID != audio.AssetID || index.UntimedTranscript != audio.UntimedTranscript || len(index.SourceSHA) != 64 || len(index.Blocks) == 0 {
		return false
	}
	if _, err := hex.DecodeString(index.SourceSHA); err != nil {
		return false
	}
	var end int64
	for _, b := range index.Blocks {
		if b.AssetID <= 0 || b.StartSample != end || b.EndSample <= b.StartSample || b.DurationMS != (b.EndSample-b.StartSample)/24 || b.DurationMS <= 0 {
			return false
		}
		if audio.UntimedTranscript && len(b.Timeline) > 0 {
			return false
		}
		if !audio.UntimedTranscript && len(b.Timeline) == 0 {
			return false
		}
		end = b.EndSample
	}
	return end/24 == audio.DurationMS
}

func scriptFirstPlayableSpans(pcm []int16, timeline []model.LiveAgentPlanTimelineSegment) ([]scriptFirstSpan, error) {
	spans, err := scriptFirstSpans(pcm, timeline)
	if err != nil {
		return nil, err
	}
	if len(timeline) > 0 && len(spans) > 1 {
		for _, span := range spans {
			cues, e := scriptFirstClipTimeline(timeline, span)
			if e != nil || len(volcengineSemanticSafePoints(cues)) == 0 {
				// The existing timed-asset contract needs a complete sentence.
				// Preserve the whole paid paragraph rather than retrying archive
				// forever or manufacturing a semantic cut from an acoustic pause.
				return []scriptFirstSpan{{0, int64(len(pcm))}}, nil
			}
		}
	}
	return spans, nil
}

type scriptFirstSpan struct{ Start, End int64 }

// Aim for 60s, but never cut speech to meet a duration target. Cut only at
// acoustic quiet points outside subtitle cues. Use 20ms frame boundaries to
// avoid padding a child with silence at every Core frame boundary.
func scriptFirstSpans(pcm []int16, timeline []model.LiveAgentPlanTimelineSegment) ([]scriptFirstSpan, error) {
	points, err := audiocuts.Scan(pcm, 24000)
	if err != nil {
		return nil, err
	}
	var spans []scriptFirstSpan
	start := int64(0)
	total := int64(len(pcm))
	mean := audiocuts.MeanRMS(pcm)
	for total-start > 90*24000 {
		best := int64(-1)
		bestScore := math.MaxFloat64
		for _, p := range points {
			// The scanner may select the first zero in a long pause. Search
			// nearby frame boundaries so the whole 20ms neighborhood is quiet.
			for delta := int64(-12); delta <= 12; delta++ {
				x := ((p.SampleOffset+240)/480 + delta) * 480
				if x-start < 45*24000 || x-start > 75*24000 || total-x < 15*24000 || x < 240 || x+240 >= total {
					continue
				}
				ms := x / 24
				inside := false
				for _, cue := range timeline {
					if cue.StartMS < ms && ms < cue.EndMS {
						inside = true
						break
					}
				}
				if inside {
					continue
				}
				// Require an actual quiet 20ms neighborhood, not a single phoneme
				// zero crossing that happens to have a low 1ms envelope.
				var sum float64
				for _, v := range pcm[x-240 : x+240] {
					sum += float64(v) * float64(v)
				}
				if math.Sqrt(sum/480) > mean*audiocuts.DefaultGradeBPercent {
					continue
				}
				score := math.Abs(float64(x-start-60*24000)) / 24000
				if p.Grade == audiocuts.GradeB {
					score += 30
				}
				if score < bestScore {
					best, bestScore = x, score
				}
			}
		}
		if best < 0 {
			break
		}
		spans = append(spans, scriptFirstSpan{start, best})
		start = best
	}
	spans = append(spans, scriptFirstSpan{start, total})
	return spans, nil
}

func scriptFirstClipTimeline(source []model.LiveAgentPlanTimelineSegment, span scriptFirstSpan) ([]model.LiveAgentPlanTimelineSegment, error) {
	if len(source) == 0 {
		return nil, nil
	}
	start, end := span.Start/24, span.End/24
	var out []model.LiveAgentPlanTimelineSegment
	for _, cue := range source {
		if cue.EndMS <= start || cue.StartMS >= end {
			continue
		}
		if cue.StartMS < start || cue.EndMS > end {
			return nil, errors.New("播放子块不能拆开字幕句段")
		}
		cue.StartMS -= start
		cue.EndMS -= start
		cue.Index = len(out) + 1
		cue.SegmentID = fmt.Sprintf("block-%03d", cue.Index)
		out = append(out, cue)
	}
	if len(out) == 0 {
		return nil, errors.New("播放子块没有对应字幕")
	}
	return out, nil
}

func scriptFirstPCMToWAV(pcm []int16) []byte {
	raw := make([]byte, 44+len(pcm)*2)
	copy(raw, "RIFF")
	binary.LittleEndian.PutUint32(raw[4:], uint32(len(raw)-8))
	copy(raw[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(raw[16:], 16)
	binary.LittleEndian.PutUint16(raw[20:], 1)
	binary.LittleEndian.PutUint16(raw[22:], 1)
	binary.LittleEndian.PutUint32(raw[24:], 24000)
	binary.LittleEndian.PutUint32(raw[28:], 48000)
	binary.LittleEndian.PutUint16(raw[32:], 2)
	binary.LittleEndian.PutUint16(raw[34:], 16)
	copy(raw[36:], "data")
	binary.LittleEndian.PutUint32(raw[40:], uint32(len(pcm)*2))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(raw[44+i*2:], uint16(v))
	}
	return raw
}

func (s *Server) ensureScriptFirstBlocks(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, audio volcengineCachedAudio) (volcengineCachedAudio, error) {
	unlock, err := lockVolcengineParagraph(ctx, unit.Dir)
	if err != nil {
		return audio, err
	}
	defer unlock()
	path := filepath.Join(unit.Dir, "playback-blocks-v1.json")
	index := scriptFirstBlockIndex{Version: 1, Key: unit.Key, ParentAssetID: audio.AssetID, UntimedTranscript: audio.UntimedTranscript}
	if err = readVolcengineJSON(path, &index); err == nil {
		if !validScriptFirstBlockIndex(index, unit.Key, audio) {
			return audio, errors.New("播放子块与当前声音缓存不一致")
		}
		valid := len(index.Blocks) > 0
		for _, b := range index.Blocks {
			if _, e := s.recordingStreamAssetURL(ctx, tenant, b.AssetID); e != nil {
				valid = false
				break
			}
		}
		if valid {
			audio.Blocks = index.Blocks
			return audio, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return audio, err
	}
	// Recover the immutable paid source locally, or download its archived copy.
	source := filepath.Join(unit.Dir, "job.json.wav")
	if v.VoiceIdentity.Model != ttsgateway.VolcengineICL2 {
		source = filepath.Join(unit.Dir, "paragraph.wav")
	}
	if _, err = os.Stat(source); err != nil {
		url, e := s.recordingStreamAssetURL(ctx, tenant, audio.AssetID)
		if e != nil {
			return audio, e
		}
		source, e = downloadVolcengineCachedWAV(ctx, url, unit.Dir)
		if e != nil {
			return audio, e
		}
		defer os.Remove(source)
	}
	pcm, rate, err := readWAVPCM16Mono(source)
	if err != nil {
		return audio, err
	}
	if rate != 24000 {
		return audio, errors.New("播放子块须使用24kHz声音")
	}
	if int64(len(pcm))/24 != audio.DurationMS {
		return audio, errors.New("播放子块源与已归档时长不一致")
	}
	sum := sha256.Sum256(int16Bytes(pcm))
	index.SourceSHA = hex.EncodeToString(sum[:])
	spans, err := scriptFirstPlayableSpans(pcm, audio.Timeline)
	if err != nil {
		return audio, err
	}
	index.Blocks = nil
	for i, span := range spans {
		if err = ctx.Err(); err != nil {
			return audio, err
		}
		timeline, e := scriptFirstClipTimeline(audio.Timeline, span)
		if e != nil {
			return audio, e
		}
		b := scriptFirstCachedBlock{StartSample: span.Start, EndSample: span.End, DurationMS: (span.End - span.Start) / 24, Text: scriptFirstText(timeline), Timeline: timeline}
		if audio.UntimedTranscript {
			b.Text = unit.Text
		}
		if len(spans) == 1 {
			b.AssetID = audio.AssetID
		} else {
			raw := scriptFirstPCMToWAV(pcm[span.Start:span.End])
			if audio.UntimedTranscript {
				id, e := s.archiveUntimedScriptFirstAudio(ctx, tenant, room, v, fmt.Sprintf("%s-b%d", unit.Key, i), unit.Text, audio.VoiceID, raw, b.DurationMS)
				if e != nil {
					return audio, e
				}
				b.AssetID = id
			} else {
				archive, e := s.archiveFullShowVoice(withVolcengineArchiveInfo(ctx, 100), tenant, 0, v.PlanID, room, fmt.Sprintf("paragraph-%s-b%d", unit.Key[:16], i), audio.VoiceID, v.VoiceIdentity.Model, v.VoiceIdentity.Source, raw, b.DurationMS, timeline, v.VoiceIdentity.Rate)
				if e != nil {
					return audio, e
				}
				b.AssetID = archive.AssetID
			}
		}
		index.Blocks = append(index.Blocks, b)
	}
	if err = writeVolcengineJSON(path, index); err != nil {
		return audio, err
	}
	audio.Blocks = index.Blocks
	return audio, nil
}
