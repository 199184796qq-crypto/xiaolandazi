package main

import (
	"encoding/json"
	"testing"
)

func TestPoolsPersistAcrossScenesWhileUsageResetsAndOnlyAffectedAngleRefreshes(t *testing.T) {
	c := newPoolCache()
	for _, angle := range []string{"angle-a", "angle-b"} {
		c.put("scene-1", directionPool{Owner: binding{TaskKey: angle}, Fingerprint: "facts-original", RequestID: angle, Items: directionItems("1. 一个方向\n2. 另一个方向", angle)})
	}
	if c.uses("scene-1", "angle-a") != 0 {
		t.Fatal("generation was counted as playback")
	}
	for i := 0; i < 3; i++ {
		c.confirmPlayed("scene-1", "angle-a", string(rune('a'+i)))
	}
	c.confirmPlayed("scene-1", "angle-a", "a")
	if c.uses("scene-1", "angle-a") != 3 {
		t.Fatal("duplicate acknowledgement counted")
	}
	if _, ok := c.lookup("scene-1", "angle-a", "facts-original", 3); ok {
		t.Fatal("reuse threshold ignored")
	}
	if _, ok := c.lookup("scene-1", "angle-b", "facts-original", 3); !ok {
		t.Fatal("unrelated angle was invalidated")
	}
	bytes, _ := json.Marshal(c.snapshot())
	restored := newPoolCache()
	if err := json.Unmarshal(bytes, &restored.data); err != nil {
		t.Fatal(err)
	}
	pool, ok := restored.lookup("scene-2", "angle-a", "facts-original", 3)
	if !ok || restored.uses("scene-2", "angle-a") != 0 || len(pool.Items) != 2 {
		t.Fatal("new scene lost pool or inherited old usage")
	}
	if _, ok = restored.lookup("scene-2", "angle-a", "facts-changed", 3); ok {
		t.Fatal("changed facts reused stale directions")
	}
	if _, ok = restored.lookup("scene-2", "angle-b", "facts-original", 3); !ok {
		t.Fatal("fact change affected unrelated angle")
	}
	restored.put("scene-1", directionPool{Owner: binding{TaskKey: "angle-a"}, Fingerprint: "facts-original", RequestID: "fresh", Items: directionItems("1. 新方向", "fresh")})
	if restored.uses("scene-1", "angle-a") != 0 || restored.snapshot().Scenes["scene-1"]["angle-a"].Total != 3 {
		t.Fatal("refresh lost total stats or did not reset refresh usage")
	}
}

func TestRefreshAccumulatesOldDirectionsAndCanReuseThem(t *testing.T) {
	c := newPoolCache()
	old := directionPool{Owner: binding{TaskKey: "angle-a"}, Fingerprint: "facts-v1", RequestID: "old", Items: directionItems("1. 旧方向甲\n2. 旧方向乙\n3. 旧方向丙", "old")}
	c.put("scene-1", old)
	fresh := directionPool{Owner: old.Owner, Fingerprint: "facts-v1", RequestID: "fresh", Items: directionItems("1. 新方向甲\n2. 新方向乙\n3. 新方向丙", "fresh")}
	c.put("scene-1", fresh)
	bank, ok := c.lookup("scene-1", "angle-a", "facts-v1", 3)
	if !ok || len(bank.Items) != 6 || len(c.snapshot().History["angle-a"]) != 2 {
		t.Fatal("refresh discarded old valid directions")
	}
	for i, item := range fresh.Items {
		c.confirmPlayed("scene-1", "angle-a", string(rune('a'+i)), item.ID)
	}
	// A new session has zero local uses and reuses an unused old direction.
	bank, ok = c.lookup("scene-2", "angle-a", "facts-v1", 3)
	if !ok || c.choose("scene-2", "angle-a", bank).ID != old.Items[0].ID || c.uses("scene-2", "angle-a") != 0 {
		t.Fatal("old directions are stored but never reusable")
	}
	c.confirmPlayed("scene-2", "angle-a", "once", old.Items[0].ID)
	c.confirmPlayed("scene-2", "angle-a", "once", old.Items[0].ID)
	if c.snapshot().DirectionTotals[old.Items[0].ID] != 1 {
		t.Fatal("duplicate play counted in long-term usage")
	}
	// Changed facts retain both old generations as history, but exclude them
	// from the current generation's selectable bank.
	changed := directionPool{Owner: old.Owner, Fingerprint: "facts-v2", RequestID: "changed", Items: directionItems("1. 新事实的方向", "changed")}
	c.put("scene-2", changed)
	bank, ok = c.lookup("scene-2", "angle-a", "facts-v2", 3)
	if !ok || len(bank.Items) != 1 || len(c.snapshot().History["angle-a"]) != 3 {
		t.Fatal("fact changes either deleted history or reused stale content")
	}
	bytes, _ := json.Marshal(c.snapshot())
	restored := newPoolCache()
	if err := json.Unmarshal(bytes, &restored.data); err != nil {
		t.Fatal(err)
	}
	if len(restored.snapshot().History["angle-a"]) != 3 || restored.uses("scene-3", "angle-a") != 0 {
		t.Fatal("saved history or new-scene reset lost")
	}
}
