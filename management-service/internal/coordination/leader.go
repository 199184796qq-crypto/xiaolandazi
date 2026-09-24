package coordination

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var acquireLeaderScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  redis.call("PEXPIRE", KEYS[1], ARGV[2])
  return 1
end
if not current then
  local result = redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2])
  if result then
    return 1
  end
end
return 0
`)

var releaseLeaderScript = redis.NewScript(`
local current = redis.call("GET", KEYS[1])
if current == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

type LeaderLease struct {
	client  *redis.Client
	key     string
	owner   string
	ttl     time.Duration
	leader  atomic.Bool
	started atomic.Bool
	done    chan struct{}
}

func NewLeaderLease(
	host string,
	port string,
	password string,
	db int,
	key string,
	owner string,
	ttl time.Duration,
) (*LeaderLease, error) {
	key = strings.TrimSpace(key)
	owner = strings.TrimSpace(owner)
	if key == "" {
		return nil, fmt.Errorf("leader lease key is required")
	}
	if owner == "" {
		return nil, fmt.Errorf("leader lease owner is required")
	}
	if ttl < 5*time.Second {
		ttl = 15 * time.Second
	}
	return &LeaderLease{
		client: redis.NewClient(&redis.Options{
			Addr:     net.JoinHostPort(host, port),
			Password: password,
			DB:       db,
		}),
		key:   key,
		owner: owner,
		ttl:   ttl,
		done:  make(chan struct{}),
	}, nil
}

func (l *LeaderLease) Start(ctx context.Context) {
	if !l.started.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer close(l.done)
		l.run(ctx)
	}()
}

func (l *LeaderLease) IsLeader() bool {
	return l.leader.Load()
}

func (l *LeaderLease) Owner() string {
	return l.owner
}

func (l *LeaderLease) Close() error {
	if l.started.Load() {
		select {
		case <-l.done:
		case <-time.After(3 * time.Second):
		}
	}
	return l.client.Close()
}

func (l *LeaderLease) run(ctx context.Context) {
	interval := l.ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	l.refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() {
		l.leader.Store(false)
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = releaseLeaderScript.Run(
			releaseCtx,
			l.client,
			[]string{l.key},
			l.owner,
		).Int64()
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.refresh(ctx)
		}
	}
}

func (l *LeaderLease) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	result, err := acquireLeaderScript.Run(
		ctx,
		l.client,
		[]string{l.key},
		l.owner,
		l.ttl.Milliseconds(),
	).Int64()
	if err != nil {
		wasLeader := l.leader.Swap(false)
		if wasLeader {
			log.Printf(
				"management leader lease lost owner=%s key=%s error=%v",
				l.owner,
				l.key,
				err,
			)
		}
		return
	}
	isLeader := result == 1
	wasLeader := l.leader.Swap(isLeader)
	if wasLeader != isLeader {
		log.Printf(
			"management leader state owner=%s key=%s leader=%t",
			l.owner,
			l.key,
			isLeader,
		)
	}
}
