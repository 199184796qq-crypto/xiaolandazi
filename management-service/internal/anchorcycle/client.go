package anchorcycle

import (
	"time"

	"github.com/redis/go-redis/v9"
)

// Separate from the latency-sensitive inbox client: cycle payloads include
// candidate texts and score traces and may travel over an SSH tunnel. Never
// automatically retry writes whose result might already have been committed.
func RedisOptions(addr, password string, db, timeoutMS int) *redis.Options {
	if timeoutMS <= 0 {
		timeoutMS = 5000
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	return &redis.Options{
		Addr: addr, Password: password, DB: db,
		DialTimeout: timeout, ReadTimeout: timeout, WriteTimeout: timeout,
		PoolTimeout: timeout, ContextTimeoutEnabled: true, MaxRetries: -1,
	}
}

// Detached persistence remains bounded, but must allow connection negotiation
// as well as the configured I/O timeout instead of imposing the old 3s cap.
func PersistenceTimeout(clientTimeout time.Duration) time.Duration {
	return max(10*time.Second, 2*clientTimeout)
}
