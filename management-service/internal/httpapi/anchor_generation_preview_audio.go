package httpapi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"strings"
)

func encodePreviewWAV(fmtData, pcm []byte) ([]byte, error) {
	if len(fmtData) < 16 || len(pcm) == 0 {
		return nil, errors.New("试听声音格式或内容为空")
	}
	var out bytes.Buffer
	fmtPadding, dataPadding := len(fmtData)%2, len(pcm)%2
	riffSize := 4 + 8 + len(fmtData) + fmtPadding + 8 + len(pcm) + dataPadding
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(riffSize))
	out.WriteString("WAVE")
	out.WriteString("fmt ")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(fmtData)))
	out.Write(fmtData)
	if fmtPadding != 0 {
		out.WriteByte(0)
	}
	out.WriteString("data")
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(pcm)))
	out.Write(pcm)
	if dataPadding != 0 {
		out.WriteByte(0)
	}
	return out.Bytes(), nil
}

func previewTimelinePrefix(timeline []model.LiveAgentPlanTimelineSegment, cutMS int64, text string) ([]model.LiveAgentPlanTimelineSegment, string, error) {
	result := make([]model.LiveAgentPlanTimelineSegment, 0, len(timeline))
	textEnd := 0
	searchAt := 0
	for _, original := range timeline {
		if original.EndMS <= original.StartMS || original.StartMS >= cutMS {
			break
		}
		segment := original
		at := strings.Index(text[searchAt:], original.Text)
		if at < 0 {
			return nil, "", errors.New("试听时间轴与正文不一致")
		}
		segmentTextAt := searchAt + at
		if segment.EndMS > cutMS {
			segment.EndMS = cutMS
			segment.SafeCut = false
			runes := []rune(original.Text)
			keep := int((cutMS - original.StartMS) * int64(len(runes)) / (original.EndMS - original.StartMS))
			keep = max(1, min(len(runes), keep))
			segment.Text = string(runes[:keep])
		}
		result = append(result, segment)
		textEnd = segmentTextAt + len(segment.Text)
		searchAt = segmentTextAt + len(original.Text)
		if original.EndMS >= cutMS {
			break
		}
	}
	if len(result) == 0 || result[len(result)-1].EndMS != cutMS || textEnd <= 0 {
		return nil, "", errors.New("试听声音不能截取到目标时长")
	}
	return result, strings.TrimSpace(text[:textEnd]), nil
}

// Generation may retain the next complete sentence. Browser projection and
// download separately enforce the hard playback deadline; this is not extra
// listening time.
func limitPreviewVoice(raw []byte, timeline []model.LiveAgentPlanTimelineSegment, duration, maxMS int64, text string) ([]byte, []model.LiveAgentPlanTimelineSegment, int64, string, bool, error) {
	if maxMS <= 0 || duration <= maxMS {
		return raw, timeline, duration, text, false, nil
	}
	if len(timeline) == 0 || timeline[0].StartMS != 0 {
		return nil, nil, 0, "", false, errors.New("试听声音缺少完整时间轴")
	}
	fmtData, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
	if err != nil {
		return nil, nil, 0, "", false, err
	}
	if len(fmtData) < 16 || binary.LittleEndian.Uint16(fmtData[:2]) != 1 {
		return nil, nil, 0, "", false, errors.New("限时试听只支持PCM声音")
	}
	align := int64(binary.LittleEndian.Uint16(fmtData[12:14]))
	if align <= 0 || int64(len(pcm))%align != 0 {
		return nil, nil, 0, "", false, errors.New("试听声音采样对齐无效")
	}
	cutMS := int64(0)
	for _, segment := range timeline {
		if segment.SafeCut && segment.EndMS >= maxMS {
			cutMS = segment.EndMS
			break
		}
	}
	if cutMS == 0 || cutMS-maxMS > anchorcycle.PreviewSafeOverrunMS {
		return nil, nil, 0, "", false, errors.New("试听声音在时长目标后没有就近完整语义结尾")
	}
	if cutMS == duration {
		return raw, timeline, duration, text, true, nil
	}
	cutBytes := ((cutMS*int64(byteRate) + 500) / 1000 / align) * align
	if cutBytes <= 0 || cutBytes > int64(len(pcm)) {
		return nil, nil, 0, "", false, errors.New("试听目标时长超出声音范围")
	}
	cutMS = (cutBytes*1000 + int64(byteRate)/2) / int64(byteRate)
	newTimeline, newText, err := previewTimelinePrefix(timeline, cutMS, text)
	if err != nil {
		return nil, nil, 0, "", false, err
	}
	clipped, err := encodePreviewWAV(fmtData, pcm[:cutBytes])
	if err != nil {
		return nil, nil, 0, "", false, err
	}
	if !newTimeline[len(newTimeline)-1].SafeCut {
		return nil, nil, 0, "", false, errors.New("试听声音没有在完整语义结尾收口")
	}
	return clipped, newTimeline, cutMS, newText, true, nil
}

// Cut browser output at a PCM sample boundary at or before the remaining budget,
// even when it lands in the middle of a word. Keep the generated asset intact.
func hardLimitPreviewWAV(raw []byte, maxMS int64) ([]byte, error) {
	fmtData, pcm, byteRate, err := parseGeneratedVoiceWAV(raw)
	if err != nil {
		return nil, err
	}
	if maxMS <= 0 || len(fmtData) < 16 || binary.LittleEndian.Uint16(fmtData[:2]) != 1 {
		return nil, errors.New("限时试听只支持有效时长的PCM声音")
	}
	align := int64(binary.LittleEndian.Uint16(fmtData[12:14]))
	if align <= 0 || byteRate <= 0 || int64(len(pcm))%align != 0 {
		return nil, errors.New("试听声音采样对齐无效")
	}
	cutBytes := maxMS * int64(byteRate) / 1000 / align * align
	if cutBytes <= 0 {
		return nil, errors.New("试听剩余时长不足一个声音采样")
	}
	if cutBytes >= int64(len(pcm)) {
		return raw, nil
	}
	return encodePreviewWAV(fmtData, pcm[:cutBytes])
}
