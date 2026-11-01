package strategycenter

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInteractionControlsCoreRestartMySQL(t *testing.T) {
	addr := os.Getenv("INTERACTION_CONTROLS_MYSQL_ADDR")
	if addr == "" {
		t.Skip("requires owned localhost MySQL fixture")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("only isolated localhost fixture accepted")
	}
	db, err := sql.Open("mysql", "root@tcp("+addr+")/?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	name := fmt.Sprintf("interaction_controls_test_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err = db.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP DATABASE `"+name+"`")
	connect := func() *sql.DB {
		t.Helper()
		c, err := sql.Open("mysql", "root@tcp("+addr+")/"+name+"?parseTime=true")
		if err != nil {
			t.Fatal(err)
		}
		c.SetMaxOpenConns(1)
		return c
	}
	c := connect()
	defer c.Close()
	exec := func(q string) {
		t.Helper()
		if _, err := c.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../../../management-service/internal/db/live_runtime_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"live_room_interaction_preferences", "live_room_interaction_controls"} {
		found := false
		for _, stmt := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n-- +statement\n") {
			if strings.HasPrefix(strings.TrimSpace(stmt), "CREATE TABLE IF NOT EXISTS "+table+" (") {
				exec(stmt)
				found = true
			}
		}
		if !found {
			t.Fatal(table)
		}
	}
	exec(`CREATE TABLE live_strategy_center_configs(tenant_id BIGINT,config_json JSON)`)
	exec(`INSERT INTO live_room_interaction_preferences(tenant_id,room_id,overall_interaction,question_preference,welcome_preference,engagement_preference,chat_preference,conversion_preference,auto_heat) VALUES(14,29,50,50,50,50,50,50,1)`)
	exec(`INSERT INTO live_room_interaction_controls(tenant_id,room_id,active,switches,cooldowns,disabled_revisions,revision,updated_by) VALUES(14,29,1,'{"welcome":false}','{"like":120,"welcome":180}','{"welcome":7}',7,8)`)
	for i := 0; i < 2; i++ {
		fresh := New()
		if err = fresh.Load(ctx, c); err != nil {
			t.Fatal(err)
		}
		p := fresh.RoomInteractionPreferences(29)
		if p.ControlEnabled("welcome") || !p.ControlEnabled("follow") || p.Cooldowns["like"] != 120 || p.ControlsRevision != 7 {
			t.Fatal("restart lost room controls", p)
		}
		c.Close()
		c = connect()
	}
	c.Close()
}
