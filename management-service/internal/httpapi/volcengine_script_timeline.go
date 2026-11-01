package httpapi

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func splitVolcengineParagraphs(text string) []string {
	var out []string
	for _, part := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "====") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
func withoutSpeechWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
func speechTimeMS(value float64, duration int64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value*1000 > float64(duration)+100 {
		return 0, errors.New("供应商字幕时间戳越界")
	}
	return min(duration, int64(math.Round(value*1000))), nil
}

// Preserve the vendor's actual word timing. Punctuation chooses boundaries;
// text length never invents a timestamp or proportionally guesses alignment.
func volcengineParagraphTimeline(text string, subtitles []ttsgateway.SpeechSubtitle, duration int64) ([]model.LiveAgentPlanTimelineSegment, error) {
	if duration <= 0 || len(subtitles) == 0 {
		return nil, errors.New("已保留声音，供应商没有返回可用字幕时间戳")
	}
	var original strings.Builder
	for _, sub := range subtitles {
		original.WriteString(sub.Text)
	}
	if withoutSpeechWhitespace(original.String()) != withoutSpeechWhitespace(text) {
		return nil, errors.New("供应商字幕与输入原文不一致，已保留声音，未重新合成")
	}
	var out []model.LiveAgentPlanTimelineSegment
	var words strings.Builder
	var start, end, lastWord int64
	flush := func() {
		if strings.TrimSpace(words.String()) == "" {
			words.Reset()
			return
		}
		out = append(out, model.LiveAgentPlanTimelineSegment{SegmentID: fmt.Sprintf("long-%03d", len(out)+1), Index: len(out) + 1, StartMS: start, EndMS: end, Text: strings.TrimSpace(words.String()), SafeCut: true})
		words.Reset()
	}
	for _, sub := range subtitles {
		if len(sub.Words) == 0 {
			flush()
			a, err := speechTimeMS(sub.Start, duration)
			if err != nil {
				return nil, err
			}
			b, err := speechTimeMS(sub.End, duration)
			if err != nil {
				return nil, err
			}
			if b <= a || a < lastWord {
				return nil, errors.New("供应商分句时间戳次序无效")
			}
			start, end, lastWord = a, b, b
			words.WriteString(sub.Text)
			flush()
			continue
		}
		for _, word := range sub.Words {
			if strings.TrimSpace(word.Text) == "" {
				continue
			}
			a, err := speechTimeMS(word.Start, duration)
			if err != nil {
				return nil, err
			}
			b, err := speechTimeMS(word.End, duration)
			if err != nil {
				return nil, err
			}
			if b <= a || a < lastWord {
				return nil, errors.New("供应商逐字时间戳次序无效")
			}
			if words.Len() == 0 {
				start = a
			}
			end, lastWord = b, b
			words.WriteString(word.Text)
			if strings.ContainsAny(word.Text, "，。！？；：,.!?;:\n") {
				flush()
			}
		}
	}
	flush()
	if len(out) == 0 {
		return nil, errors.New("供应商字幕为空，已保留声音")
	}
	if len(out) > 1024 {
		return nil, errors.New("长段字幕超过播放窗口上限；请用====缩短单段，已保留声音未重新合成")
	}
	// ==== is an explicit complete paragraph boundary. If its raw text lacks
	// closing punctuation, mark that boundary in captions only; the original
	// TTS input and cached spoken text remain unchanged.
	if !volcengineSentenceEnding(out[len(out)-1].Text) {
		out[len(out)-1].Text += "。"
	}
	return out, nil
}

func volcengineSentenceEnding(text string) bool {
	runes := []rune(strings.TrimSpace(text))
	return len(runes) > 0 && strings.ContainsRune("。！？!?；;", runes[len(runes)-1])
}

// Core preserves timeline SafeCut as PauseCut for quick interaction mode, but
// its explicit semantic safe-point list accepts only full sentence endings.
func volcengineSemanticSafePoints(timeline []model.LiveAgentPlanTimelineSegment) []model.LiveAgentPlanSafePoint {
	semantic := append([]model.LiveAgentPlanTimelineSegment(nil), timeline...)
	for i := range semantic {
		semantic[i].SafeCut = semantic[i].SafeCut && volcengineSentenceEnding(semantic[i].Text)
	}
	return buildFullShowSafePoints(semantic)
}

// Use the measured rendered length: this also absorbs encoder/frame rounding
// instead of letting the final subtitle drift past the actual audio endpoint.
func stretchVolcengineTimeline(source []model.LiveAgentPlanTimelineSegment, oldMS, newMS int64) ([]model.LiveAgentPlanTimelineSegment, error) {
	if oldMS <= 0 || newMS <= 0 {
		return nil, errors.New("变速声音时长无效")
	}
	out := append([]model.LiveAgentPlanTimelineSegment(nil), source...)
	for i := range out {
		if out[i].StartMS < 0 || out[i].EndMS > oldMS || out[i].EndMS <= out[i].StartMS {
			return nil, errors.New("变速字幕源时间戳无效")
		}
		out[i].StartMS = int64(math.Round(float64(out[i].StartMS) * float64(newMS) / float64(oldMS)))
		out[i].EndMS = min(newMS, int64(math.Round(float64(out[i].EndMS)*float64(newMS)/float64(oldMS))))
		if out[i].EndMS <= out[i].StartMS {
			return nil, errors.New("变速后字幕时间段为空")
		}
	}
	return out, nil
}
