package anchorcycle

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCycleRedisOptionsHaveDedicatedTimeoutWithoutRetries(t *testing.T) {
	for _, milliseconds := range []int{0, 5000, 8000} {
		o := RedisOptions("127.0.0.1:16379", "test-password", 2, milliseconds)
		want := time.Duration(milliseconds) * time.Millisecond
		if milliseconds == 0 {
			want = 5 * time.Second
		}
		if o.Addr != "127.0.0.1:16379" || o.Password != "test-password" || o.DB != 2 || o.ReadTimeout != want || o.WriteTimeout != want || o.DialTimeout != want || o.PoolTimeout != want || o.MaxRetries != -1 || !o.ContextTimeoutEnabled {
			t.Fatal("invalid dedicated cycle Redis options")
		}
		if PersistenceTimeout(want) < 2*want {
			t.Fatal("persistence deadline shorter than I/O budget")
		}
	}
}

// A deterministic delayed Redis peer reproduces a tunnel jitter above the old
// 500ms cap without depending on real network conditions or issuing model calls.
func TestCycleRedisToleratesReadDelayAboveOldHalfSecondCap(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil || !strings.HasPrefix(line, "*") {
						return
					}
					n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
					parts := []string{}
					for i := 0; i < n; i++ {
						if _, err = reader.ReadString('\n'); err != nil {
							return
						}
						value, e := reader.ReadString('\n')
						if e != nil {
							return
						}
						parts = append(parts, strings.TrimSpace(value))
					}
					if len(parts) == 0 {
						return
					}
					switch strings.ToUpper(parts[0]) {
					case "HELLO":
						fmt.Fprint(conn, "-ERR unknown command 'HELLO'\r\n")
					case "PING":
						time.Sleep(700 * time.Millisecond)
						fmt.Fprint(conn, "+PONG\r\n")
					default:
						fmt.Fprint(conn, "+OK\r\n")
					}
				}
			}()
		}
	}()
	old := RedisOptions(listener.Addr().String(), "", 0, 500)
	oldClient := redis.NewClient(old)
	defer oldClient.Close()
	if err := oldClient.Ping(context.Background()).Err(); err == nil {
		t.Fatal("old 500ms timeout did not reproduce")
	} else {
		var timeout net.Error
		if !errors.As(err, &timeout) || !timeout.Timeout() {
			t.Fatalf("expected read timeout, got %v", err)
		}
	}
	fixed := redis.NewClient(RedisOptions(listener.Addr().String(), "", 0, 5000))
	defer fixed.Close()
	if err := fixed.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("dedicated 5s client still failed: %v", err)
	}
}
