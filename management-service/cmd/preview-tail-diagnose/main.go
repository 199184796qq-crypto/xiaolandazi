// Read-only, narrowly scoped comparison of recorded TTS responses. Does not
// request synthesis, expose media URLs/credentials, or alter live playback.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/ttsgateway"
)

func main() {
	var input struct {
		TenantID int64    `json:"tenant_id"`
		PlanID   int64    `json:"plan_id"`
		Texts    []string `json:"texts"`
	}
	if json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&input) != nil || input.TenantID <= 0 || input.PlanID <= 0 || len(input.Texts) == 0 || len(input.Texts) > 16 {
		fmt.Println("explicit tenant, plan and 1-16 target sentences required")
		return
	}
	cfg := config.Load()
	c := mysql.NewConfig()
	c.User = cfg.DBUser
	c.Passwd = cfg.DBPassword
	c.Net = "tcp"
	c.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	c.DBName = cfg.DBName
	c.Timeout = 5 * time.Second
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		fmt.Println("database unavailable")
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, text := range input.Texts {
		if len(text) == 0 || len(text) > 3000 {
			fmt.Println("sentence length invalid")
			continue
		}
		var reqRaw, respRaw string
		err = db.QueryRowContext(ctx, `SELECT request_json,response_json FROM live_model_exchanges WHERE tenant_id=? AND plan_id=? AND stage='mainline_tts' AND status='completed' AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.Text'))=? ORDER BY id DESC LIMIT 1`, input.TenantID, input.PlanID, text).Scan(&reqRaw, &respRaw)
		if err != nil {
			fmt.Println("record not available for requested sentence")
			continue
		}
		var req ttsgateway.SynthesizeRequest
		var resp ttsgateway.SynthesizeResponse
		if json.Unmarshal([]byte(reqRaw), &req) != nil || json.Unmarshal([]byte(respRaw), &resp) != nil {
			fmt.Println("record invalid")
			continue
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, resp.AudioURL, nil)
		if err != nil {
			fmt.Println("upstream source unavailable")
			continue
		}
		result, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
		if err != nil {
			fmt.Println("upstream source unavailable")
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(result.Body, 8<<20))
		result.Body.Close()
		if err != nil || result.StatusCode != 200 {
			fmt.Println("upstream source unavailable")
			continue
		}
		var pcm, format []byte
		for off := 12; off+8 <= len(raw); {
			size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
			end := off + 8 + size
			if end > len(raw) {
				end = len(raw)
			}
			switch string(raw[off : off+4]) {
			case "fmt ":
				format = raw[off+8 : end]
			case "data":
				pcm = raw[off+8 : end]
			}
			off = end + size%2
		}
		if len(format) < 16 || len(pcm) == 0 {
			fmt.Println("upstream WAV invalid")
			continue
		}
		rate := binary.LittleEndian.Uint32(format[8:12])
		if rate == 0 {
			continue
		}
		out := map[string]any{"text": text, "rate": req.Rate, "model": req.Model, "upstream_pcm_bytes": len(pcm), "upstream_pcm_sha256": fmt.Sprintf("%x", sha256.Sum256(pcm)), "upstream_duration_ms": len(pcm) * 1000 / int(rate)}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
	}
}
