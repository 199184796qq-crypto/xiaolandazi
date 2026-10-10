package httpapi

import (
	"encoding/json"
	"strconv"

	"livecompanion/management/internal/contentpools"
)

// Request-local aliases shorten references without interpreting fact text.
// Only this exact pool's explicit table can restore an ID; unknown references
// remain invalid and are sent to the existing bounded model repair.
type contentPoolWire struct {
	pool    contentpools.Pool
	toWire  map[string]string
	toSaved map[string]string
}

func newContentPoolWire(pool contentpools.Pool) contentPoolWire {
	w := contentPoolWire{pool: pool, toWire: map[string]string{}, toSaved: map[string]string{}}
	w.pool.Facts = append([]contentpools.Fact(nil), pool.Facts...)
	for i, f := range pool.Facts {
		alias := strconv.Itoa(i + 1)
		w.toWire[f.ID], w.toSaved[alias] = alias, f.ID
		w.pool.Facts[i].ID = alias
		w.pool.Facts[i].Sources = nil // Original provenance stays in the saved set.
	}
	w.pool.Tasks = w.tasks(pool.Tasks)
	return w
}

func (w contentPoolWire) tasks(tasks []contentpools.Task) []contentpools.Task {
	result := make([]contentpools.Task, 0, len(tasks))
	for _, task := range tasks {
		task.Sources = append([]string(nil), task.Sources...)
		for i, id := range task.Sources {
			if alias, ok := w.toWire[id]; ok {
				task.Sources[i] = alias
			}
		}
		result = append(result, task)
	}
	return result
}

func (w contentPoolWire) response(raw string, toSaved bool) string {
	var top map[string]json.RawMessage
	if json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &top) != nil {
		return raw
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(poolProtocolField(top, "tasks", "任务", "卖点角度任务"), &rows) != nil {
		return raw
	}
	for _, row := range rows {
		var refs []json.RawMessage
		if json.Unmarshal(poolProtocolField(row, "source_ids", "事实编号", "依据编号"), &refs) != nil {
			continue
		}
		for i, ref := range refs {
			var id string
			if json.Unmarshal(ref, &id) != nil {
				// Numeric aliases are exact table keys, never fuzzy source guesses.
				if !toSaved {
					continue
				}
				id = string(ref)
			}
			mapping := w.toWire
			if toSaved {
				mapping = w.toSaved
			}
			if mapped, ok := mapping[id]; ok {
				refs[i], _ = json.Marshal(mapped)
			}
		}
		row["source_ids"], _ = json.Marshal(refs)
	}
	top["tasks"], _ = json.Marshal(rows)
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return string(out)
}
