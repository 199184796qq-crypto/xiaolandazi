package db

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestQuestionStatsArchiveMySQL(t *testing.T) {
	store := salesIsolatedMySQL(t)
	ctx := t.Context()
	load := func(tenant, plan, room, id int64) (QuestionStatsSession, []QuestionStatsComment, int, error) {
		var comments []QuestionStatsComment
		session, total, err := store.StreamQuestionStatsComments(ctx, tenant, plan, room, id, func(comment QuestionStatsComment) error {
			comments = append(comments, comment)
			return nil
		})
		return session, comments, total, err
	}
	for _, query := range []string{
		"CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY, tenant_id BIGINT, status VARCHAR(16))",
		"CREATE TABLE live_agent_plan_room_bindings(tenant_id BIGINT, plan_id BIGINT, room_id BIGINT, status VARCHAR(16))",
		"CREATE TABLE live_agent_room_plan_selections(tenant_id BIGINT, plan_id BIGINT, room_id BIGINT)",
		"CREATE TABLE core_rooms(id BIGINT PRIMARY KEY, tenant_id BIGINT, name VARCHAR(128))",
		"CREATE TABLE mgmt_room_deletions(tenant_id BIGINT,room_id BIGINT)",
		"CREATE TABLE live_room_event_archive(id BIGINT PRIMARY KEY, tenant_id BIGINT,room_id BIGINT,event_type VARCHAR(40),content TEXT,occurred_at DATETIME(3))",
		"INSERT INTO live_agent_plans VALUES(7,14,'active'),(8,18,'active')",
		"INSERT INTO core_rooms VALUES(29,14,'本店'),(30,14,'已删除'),(31,18,'其他租户'),(32,14,'当前使用方案'),(34,14,'已解绑')",
		"INSERT INTO live_agent_plan_room_bindings VALUES(14,7,29,'active'),(14,7,30,'active'),(14,7,31,'active'),(18,8,31,'active'),(14,7,34,'unbound')",
		"INSERT INTO mgmt_room_deletions VALUES(14,30)",
		"INSERT INTO live_agent_room_plan_selections VALUES(14,7,29),(14,7,30),(14,7,31),(14,7,32),(18,8,31)",
		"INSERT INTO live_room_event_archive VALUES " +
			"(9,14,29,'chat','开场前','2026-11-25 10:00:00'),(10,14,29,'session_start',NULL,'2026-11-25 10:00:00')," +
			"(11,14,29,'chat','运费多少','2026-11-25 10:00:00'),(12,14,29,'comment',' 运费多少 ','2026-11-25 10:00:01')," +
			"(13,14,29,'member','进场','2026-11-25 10:00:01'),(21,14,29,'enter','进房','2026-11-25 10:00:02'),(22,14,29,'like','点赞','2026-11-25 10:00:03'),(23,14,29,'follow','关注','2026-11-25 10:00:04'),(24,14,29,'gift','送礼','2026-11-25 10:00:05'),(25,14,29,'order_signal','下单事件','2026-11-25 10:00:06'),(14,14,29,'chat','多少钱','2026-11-25 11:00:00')," +
			"(15,14,29,'session_start',NULL,'2026-11-25 11:00:00'),(16,14,29,'chat','后场问题','2026-11-25 11:00:00')," +
			"(17,18,31,'session_start',NULL,'2026-11-25 10:00:00'),(18,18,31,'chat','其他租户问题','2026-11-25 10:00:01'),(19,14,32,'session_start',NULL,'2026-11-25 10:00:00'),(20,14,32,'chat','没有绑定的关联房间','2026-11-25 10:00:01')",
	} {
		if _, err := store.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	rooms, err := store.ListQuestionStatsRooms(ctx, 14, 7, 0)
	if err != nil || len(rooms) != 2 || rooms[0].ID != 29 || rooms[1].ID != 32 {
		t.Fatalf("rooms: %+v, %v", rooms, err)
	}
	rooms, err = store.ListQuestionStatsRooms(ctx, 18, 8, 0)
	if err != nil || len(rooms) != 1 || rooms[0].ID != 31 {
		t.Fatalf("one-room plan: %+v, %v", rooms, err)
	}
	selected, err := store.ListQuestionStatsRooms(ctx, 14, 7, 32)
	if err != nil || len(selected) != 1 || selected[0].ID != 32 {
		t.Fatalf("selected-only room access: %+v, %v", selected, err)
	}
	_, selectedComments, selectedTotal, err := load(14, 7, 32, 19)
	if err != nil || selectedTotal != 1 || len(selectedComments) != 1 || selectedComments[0].Text != "没有绑定的关联房间" {
		t.Fatalf("selected-only room comments: %+v, %v", selectedComments, err)
	}
	sessions, err := store.ListQuestionStatsSessions(ctx, 14, 29)
	if err != nil || len(sessions) != 2 || sessions[0].ID != 15 || sessions[1].ID != 10 {
		t.Fatalf("sessions: %+v, %v", sessions, err)
	}
	session, comments, total, err := load(14, 7, 29, 10)
	if err != nil || total != 3 || len(comments) != 2 || comments[0] != (QuestionStatsComment{Text: "运费多少", Count: 2}) ||
		comments[1].Text != "多少钱" || session.StartedAt.Hour() != 10 {
		t.Fatalf("session/comments: %+v %+v %d %v", session, comments, total, err)
	}
	_, comments, total, err = load(14, 7, 29, 15)
	if err != nil || total != 1 || len(comments) != 1 || comments[0].Text != "后场问题" {
		t.Fatalf("next session: %+v %d %v", comments, total, err)
	}
	for _, ids := range [][4]int64{{14, 7, 29, 17}, {14, 7, 31, 17}, {18, 8, 29, 10}, {14, 7, 30, 10}} {
		if _, _, _, err := load(ids[0], ids[1], ids[2], ids[3]); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("scope %+v leaked: %v", ids, err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO live_room_event_archive VALUES
        (26,14,29,'session_end',NULL,'2026-11-25 11:10:00'),
        (27,14,29,'session_resume',NULL,'2026-11-25 11:20:00'),
        (28,14,29,'comment','重连后问题','2026-11-25 11:30:00'),
        (29,14,29,'session_end',NULL,'2026-11-25 11:31:00'),
        (30,14,29,'chat','收播后问题','2026-11-25 11:31:00')`); err != nil {
		t.Fatal(err)
	}
	_, comments, total, err = load(14, 7, 29, 15)
	if err != nil || total != 2 || len(comments) != 2 || comments[1].Text != "重连后问题" {
		t.Fatalf("resumed and ended session: %+v %d %v", comments, total, err)
	}
	// Include distinct long comments with an identical sorting prefix.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO live_room_event_archive
        WITH RECURSIVE series(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM series WHERE n<650)
        SELECT 100+n,14,29,'comment',CONCAT(REPEAT('a',2000),n),'2026-11-25 10:30:00' FROM series`); err != nil {
		t.Fatal(err)
	}
	_, comments, total, err = load(14, 7, 29, 10)
	if err != nil || total != 653 || len(comments) != 652 || len(comments[len(comments)-1].Text) != 2003 {
		t.Fatalf("full session: %d rows, %d comments: %v", len(comments), total, err)
	}
	if !sessions[0].StartedAt.After(sessions[1].StartedAt.Add(59 * time.Minute)) {
		t.Fatal("session order differs")
	}
}
