package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/speechasr"
	"livecompanion/management/internal/storage"
)

func (s *Server) queueHumanFissionSplit(w http.ResponseWriter, r *http.Request, tenant, plan, source int64) {
	var selection humanfission.SplitRequest
	index, err := strconv.Atoi(r.PathValue("part"))
	if err != nil || index < 1 || readJSONWithLimit(w, r, &selection, 4<<10) != nil || len(selection.Version) != 64 {
		writeError(w, http.StatusBadRequest, "语音片选区无效")
		return
	}
	selection.Number = index
	if err = s.store.QueueHumanFissionSplit(r.Context(), tenant, plan, source, selection); err != nil {
		switch {
		case errors.Is(err, humanfission.ErrSplitSelection):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, db.ErrFissionUpdated), errors.Is(err, db.ErrFissionBusy):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeScriptHumanVoiceError(w, err)
		}
		return
	}
	select {
	case humanFissionWake <- struct{}{}:
	default:
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"split_status": "queued"})
}

func fissionTimedWords(result speechasr.Result) []humanfission.TimedWord {
	var words []humanfission.TimedWord
	for _, sentence := range result.Sentences {
		for _, word := range sentence.Words {
			words = append(words, humanfission.TimedWord{Start: word.BeginTime, End: word.EndTime, Text: word.Text})
		}
	}
	return words
}

func (s *Server) splitHumanFissionPart(ctx context.Context, job *db.HumanFissionJob, oss *storage.OSS, report func(string) error) error {
	selection := job.State.Split
	if selection == nil || selection.Number < 1 || selection.Number > len(job.State.Parts) {
		return db.ErrFissionUpdated
	}
	part := job.State.Parts[selection.Number-1]
	if part.Version() != selection.Version {
		return db.ErrFissionUpdated
	}
	if err := report("正在定位选中的声音…"); err != nil {
		return err
	}
	pcm, err := humanFissionRead(ctx, oss, part.Object)
	if err != nil {
		return errors.New("语音片暂时无法读取，请重试")
	}
	pieces, err := humanfission.CutSelection(part, pcm, selection.Start, selection.End)
	if errors.Is(err, humanfission.ErrSplitAlignment) && len(part.Words) == 0 {
		if err = report("正在识别这一片的词级时间戳…"); err != nil {
			return err
		}
		link, err := oss.SignedURL(ctx, part.Object, 5*time.Minute)
		if err != nil {
			return errors.New("语音片暂时无法读取，请重试")
		}
		asr, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		result, err := speechasr.NewFromEnv().Transcribe(asr, link)
		if err != nil {
			return errors.New("词级时间戳暂时无法获取，请重试")
		}
		part.Words = fissionTimedWords(result)
		// Persist alignment for another selection, even when this boundary has no safe cut.
		job.State.Parts[selection.Number-1].Words = part.Words
		pieces, err = humanfission.CutSelection(part, pcm, selection.Start, selection.End)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	parts := make([]humanfission.Part, 0, len(pieces))
	for i, piece := range pieces {
		if err := report(fmt.Sprintf("正在保存第 %d / %d 片…", i+1, len(pieces))); err != nil {
			return err
		}
		name := "split-" + humanfission.Digest([]any{part.Object, piece.From, piece.To})
		key, err := s.putHumanFission(ctx, *job, oss, name, pcm[piece.From:piece.To])
		if err != nil {
			return err
		}
		piece.Part.Object = key
		parts = append(parts, piece.Part)
	}
	job.State.ReplacePart(selection.Number, parts)
	job.State.Split = nil
	return nil
}
