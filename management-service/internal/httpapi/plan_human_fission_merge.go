package httpapi

import (
	"context"
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/storage"
)

func (s *Server) queueHumanFissionMerge(w http.ResponseWriter, r *http.Request, tenant, plan, source int64) {
	var selection humanfission.MergeRequest
	if readJSONWithLimit(w, r, &selection, 4<<10) != nil || !selection.Adjacent() ||
		len(selection.FirstVersion) != 64 || len(selection.SecondVersion) != 64 {
		writeError(w, http.StatusBadRequest, humanfission.ErrMergeSelection.Error())
		return
	}
	if err := s.store.QueueHumanFissionMerge(r.Context(), tenant, plan, source, selection); err != nil {
		if errors.Is(err, db.ErrFissionUpdated) || errors.Is(err, db.ErrFissionBusy) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeScriptHumanVoiceError(w, err)
		}
		return
	}
	select {
	case humanFissionWake <- struct{}{}:
	default:
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"merge_status": "queued"})
}

func (s *Server) mergeHumanFissionParts(ctx context.Context, job *db.HumanFissionJob, oss *storage.OSS, report func(string) error) error {
	selection := job.State.Merge
	if selection == nil || !selection.Matches(job.State.Parts) {
		return db.ErrFissionUpdated
	}
	if err := report("正在合并两片语音…"); err != nil {
		return err
	}
	first, second := job.State.Parts[selection.First-1], job.State.Parts[selection.Second-1]
	left, err := humanFissionRead(ctx, oss, first.Object)
	if err != nil {
		return errors.New("语音片暂时无法读取，请重试")
	}
	right, err := humanFissionRead(ctx, oss, second.Object)
	if err != nil {
		return errors.New("语音片暂时无法读取，请重试")
	}
	part, err := humanfission.JoinParts(first, second, len(left), len(right))
	if err != nil {
		return err
	}
	name := "merge-" + humanfission.Digest([]string{first.Object, second.Object})
	part.Object, err = s.putHumanFission(ctx, *job, oss, name, append(left, right...))
	if err != nil {
		return err
	}
	job.State.MergeParts(selection.First, part)
	job.State.Merge = nil
	return nil
}
