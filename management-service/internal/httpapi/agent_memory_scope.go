package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/model"
)

type agentMemoryPlanReader interface {
	GetLiveAgentPlan(context.Context, int64, int64) (model.LiveAgentPlan, error)
	GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error)
	GetAgentMemoryItem(context.Context, int64, int64, int64) (model.AgentMemoryItem, error)
}

// Older clients without plan_id see only the room's bound plan, never every
// plan's memories. An explicit selection must resolve inside the same tenant.
func agentMemoryPlan(w http.ResponseWriter, r *http.Request, store agentMemoryPlanReader, tenant, room int64) (int64, bool) {
	var plan model.LiveAgentPlan
	var err error
	if values, explicit := r.URL.Query()["plan_id"]; explicit {
		var id int64
		if len(values) == 1 {
			id, err = strconv.ParseInt(values[0], 10, 64)
		}
		if err != nil || id <= 0 {
			writeError(w, 400, "请选择有效的智能体方案")
			return 0, false
		}
		plan, err = store.GetLiveAgentPlan(r.Context(), tenant, id)
	} else {
		plan, err = store.GetLiveAgentPlanForRoom(r.Context(), tenant, room)
		if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) || errors.Is(err, sql.ErrNoRows) {
			return 0, true
		}
	}
	if err != nil || plan.ID <= 0 || plan.TenantID != tenant || plan.Status != "active" {
		writeError(w, 404, "当前方案不存在或不可用")
		return 0, false
	}
	return plan.ID, true
}

func agentMemoryInPlan(w http.ResponseWriter, r *http.Request, store agentMemoryPlanReader, tenant, room, plan, id int64) (model.AgentMemoryItem, bool) {
	item, err := store.GetAgentMemoryItem(r.Context(), tenant, room, id)
	if err != nil || !interactionlearning.MemoryInPlan(item, tenant, room, plan) {
		writeError(w, 404, "当前方案中不存在这条回答策略")
		return model.AgentMemoryItem{}, false
	}
	return item, true
}

func agentLearningSourceInPlan(w http.ResponseWriter, r *http.Request, store agentMemoryPlanReader, tenant, room, plan int64, ref string) bool {
	if !strings.HasPrefix(ref, "agent_memory:") {
		return true
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(ref, "agent_memory:"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, 400, "回答策略编号无效")
		return false
	}
	_, ok := agentMemoryInPlan(w, r, store, tenant, room, plan, id)
	return ok
}
