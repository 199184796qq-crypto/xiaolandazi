package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
)

type fissionNameRow struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

func (s *Server) handleHumanFissionNames(w http.ResponseWriter, r *http.Request, tenant, plan, source int64) {
	var err error
	if r.Method == http.MethodPatch {
		var edit db.HumanFissionNameEdit
		if readJSONWithLimit(w, r, &edit, 4<<10) != nil {
			writeError(w, http.StatusBadRequest, "名称参数无效")
			return
		}
		edit.Name = strings.TrimSpace(edit.Name)
		if !humanfission.ValidName(edit.Name) || len(edit.Version) != 64 {
			writeError(w, http.StatusBadRequest, "请输入 1 到 32 个字符的名称")
			return
		}
		err = s.store.RenameHumanFission(r.Context(), tenant, plan, source, edit)
	} else {
		err = s.store.QueueHumanFissionNames(r.Context(), tenant, plan, source)
	}
	if err != nil {
		if errors.Is(err, db.ErrFissionUpdated) || errors.Is(err, db.ErrFissionBusy) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeScriptHumanVoiceError(w, err)
		}
		return
	}
	if r.Method == http.MethodPost {
		wakeHumanFission()
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func wakeHumanFission() {
	select {
	case humanFissionWake <- struct{}{}:
	default:
	}
}

func (s *Server) nameHumanFission(ctx context.Context, job *db.HumanFissionJob, report func(string) error) error {
	if !job.State.NeedsNames() {
		return nil
	}
	if err := report("正在给语音片和组合取名…"); err != nil {
		return err
	}
	parts := make([]map[string]any, 0, len(job.State.Parts))
	plans := make([]map[string]any, 0, len(job.State.Recipes))
	for i, recipe := range job.State.Recipes {
		if !humanfission.ValidName(recipe.Name) {
			plans = append(plans, map[string]any{"number": i + 1, "order": recipe.Order})
		}
	}
	for i, part := range job.State.Parts {
		if !humanfission.ValidName(part.Name) || len(plans) != 0 {
			parts = append(parts, map[string]any{"number": i + 1, "text": part.Text, "needs_name": !humanfission.ValidName(part.Name)})
		}
	}
	var named struct {
		Parts []fissionNameRow `json:"parts"`
		Plans []fissionNameRow `json:"plans"`
	}
	if err := s.completeHumanFission(ctx, "name", map[string]any{"parts": parts, "plans": plans}, &named); err != nil {
		return err
	}
	applyHumanFissionNames(&job.State, named.Parts, named.Plans)
	if job.State.NeedsNames() {
		return errors.New("部分名称未生成，可重新生成名称")
	}
	return nil
}

func applyHumanFissionNames(state *humanfission.State, parts, plans []fissionNameRow) {
	for _, row := range parts {
		if row.Number >= 1 && row.Number <= len(state.Parts) && humanfission.ValidName(row.Name) &&
			!humanfission.ValidName(state.Parts[row.Number-1].Name) {
			state.Parts[row.Number-1].Name = strings.TrimSpace(row.Name)
		}
	}
	for _, row := range plans {
		if row.Number >= 1 && row.Number <= len(state.Recipes) && humanfission.ValidName(row.Name) &&
			!humanfission.ValidName(state.Recipes[row.Number-1].Name) {
			state.Recipes[row.Number-1].Name = strings.TrimSpace(row.Name)
		}
	}
}

func (s *Server) tryHumanFissionNames(ctx context.Context, job *db.HumanFissionJob, report func(string) error) {
	if err := s.nameHumanFission(ctx, job, report); err != nil && ctx.Err() == nil {
		log.Printf("human fission names source=%d: %v", job.Source, err)
	}
}
