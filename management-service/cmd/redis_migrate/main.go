package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type fileConfig map[string]string

func main() {
	mode := flag.String("mode", "check", "check, copy, or verify")
	replaceTarget := flag.Bool("replace-target", false, "allow clearing a non-empty target before copy")
	configPath := flag.String("config", "../configs/cloud-dev.local", "path to local cloud config")
	flag.Parse()

	cfg, err := loadEnvFile(*configPath)
	if err != nil {
		fatal(err)
	}

	targetHost := strings.TrimSpace(cfg["REDIS_HOST"])
	targetPort := strings.TrimSpace(cfg["REDIS_PORT"])
	targetPassword := cfg["REDIS_PASSWORD"]
	targetDB, err := strconv.Atoi(defaultString(strings.TrimSpace(cfg["REDIS_DB"]), "0"))
	if err != nil {
		fatal(fmt.Errorf("invalid REDIS_DB: %w", err))
	}
	if targetHost == "" {
		fatal(fmt.Errorf("REDIS_HOST is empty in %s", *configPath))
	}
	if targetPort == "" {
		targetPort = "6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	source := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		DB:   0,
	})
	defer source.Close()

	target := redis.NewClient(&redis.Options{
		Addr:     targetHost + ":" + targetPort,
		Password: targetPassword,
		DB:       targetDB,
	})
	defer target.Close()

	if err := source.Ping(ctx).Err(); err != nil {
		fatal(fmt.Errorf("local redis unavailable: %w", err))
	}
	if err := target.Ping(ctx).Err(); err != nil {
		fatal(fmt.Errorf("cloud redis unavailable: %w", err))
	}

	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "check":
		localKeys, err := allKeys(ctx, source)
		if err != nil {
			fatal(err)
		}
		cloudKeys, err := allKeys(ctx, target)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("redis_check_ok=1 local_keys=%d cloud_keys=%d target=%s:%s db=%d\n", len(localKeys), len(cloudKeys), targetHost, targetPort, targetDB)
	case "copy":
		if err := copyAll(ctx, source, target, *replaceTarget); err != nil {
			fatal(err)
		}
		if err := verifyAll(ctx, source, target); err != nil {
			fatal(err)
		}
		keys, _ := allKeys(ctx, source)
		fmt.Printf("redis_copy_ok=1 keys=%d\n", len(keys))
	case "verify":
		if err := verifyAll(ctx, source, target); err != nil {
			fatal(err)
		}
		keys, _ := allKeys(ctx, source)
		fmt.Printf("redis_verify_ok=1 keys=%d\n", len(keys))
	default:
		fatal(fmt.Errorf("unsupported mode %q", *mode))
	}
}

func copyAll(ctx context.Context, source, target *redis.Client, replaceTarget bool) error {
	keys, err := allKeys(ctx, source)
	if err != nil {
		return err
	}
	cloudKeys, err := allKeys(ctx, target)
	if err != nil {
		return err
	}
	if len(cloudKeys) != 0 {
		if !replaceTarget {
			return fmt.Errorf("cloud redis is not empty: %d keys found; refusing to overwrite", len(cloudKeys))
		}
		if err := target.FlushDB(ctx).Err(); err != nil {
			return fmt.Errorf("clear cloud redis before copy: %w", err)
		}
	}

	for _, key := range keys {
		if err := copyKey(ctx, source, target, key); err != nil {
			return fmt.Errorf("copy key %q: %w", key, err)
		}
	}
	return nil
}

func copyKey(ctx context.Context, source, target *redis.Client, key string) error {
	typ, err := source.Type(ctx, key).Result()
	if err != nil {
		return err
	}

	switch typ {
	case "string":
		value, err := source.Get(ctx, key).Result()
		if err != nil {
			return err
		}
		if err := target.Set(ctx, key, value, 0).Err(); err != nil {
			return err
		}
	case "list":
		values, err := source.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		if len(values) > 0 {
			items := make([]any, len(values))
			for i := range values {
				items[i] = values[i]
			}
			if err := target.RPush(ctx, key, items...).Err(); err != nil {
				return err
			}
		}
	case "hash":
		values, err := source.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) > 0 {
			if err := target.HSet(ctx, key, values).Err(); err != nil {
				return err
			}
		}
	case "set":
		values, err := source.SMembers(ctx, key).Result()
		if err != nil {
			return err
		}
		if len(values) > 0 {
			items := make([]any, len(values))
			for i := range values {
				items[i] = values[i]
			}
			if err := target.SAdd(ctx, key, items...).Err(); err != nil {
				return err
			}
		}
	case "zset":
		values, err := source.ZRangeWithScores(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		if len(values) > 0 {
			if err := target.ZAdd(ctx, key, values...).Err(); err != nil {
				return err
			}
		}
	case "stream":
		messages, err := source.XRange(ctx, key, "-", "+").Result()
		if err != nil {
			return err
		}
		const batchSize = 256
		for start := 0; start < len(messages); start += batchSize {
			end := start + batchSize
			if end > len(messages) {
				end = len(messages)
			}
			pipe := target.Pipeline()
			for _, message := range messages[start:end] {
				pipe.XAdd(ctx, &redis.XAddArgs{
					Stream: key,
					ID:     message.ID,
					Values: message.Values,
				})
			}
			if _, err := pipe.Exec(ctx); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported redis type %q", typ)
	}

	ttl, err := source.PTTL(ctx, key).Result()
	if err != nil {
		return err
	}
	if ttl > 0 {
		if err := target.PExpire(ctx, key, ttl).Err(); err != nil {
			return err
		}
	}
	return nil
}

func verifyAll(ctx context.Context, source, target *redis.Client) error {
	sourceKeys, err := allKeys(ctx, source)
	if err != nil {
		return err
	}
	targetKeys, err := allKeys(ctx, target)
	if err != nil {
		return err
	}
	if strings.Join(sourceKeys, "\x00") != strings.Join(targetKeys, "\x00") {
		return fmt.Errorf("key set mismatch: local=%d cloud=%d", len(sourceKeys), len(targetKeys))
	}

	for _, key := range sourceKeys {
		if err := verifyKey(ctx, source, target, key); err != nil {
			return fmt.Errorf("verify key %q: %w", key, err)
		}
	}
	return nil
}

func verifyKey(ctx context.Context, source, target *redis.Client, key string) error {
	sourceType, err := source.Type(ctx, key).Result()
	if err != nil {
		return err
	}
	targetType, err := target.Type(ctx, key).Result()
	if err != nil {
		return err
	}
	if sourceType != targetType {
		return fmt.Errorf("type mismatch local=%s cloud=%s", sourceType, targetType)
	}

	switch sourceType {
	case "string":
		a, err := source.Get(ctx, key).Result()
		if err != nil {
			return err
		}
		b, err := target.Get(ctx, key).Result()
		if err != nil {
			return err
		}
		if a != b {
			return fmt.Errorf("string value mismatch")
		}
	case "list":
		a, err := source.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		b, err := target.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		if !equalStrings(a, b) {
			return fmt.Errorf("list value mismatch")
		}
	case "hash":
		a, err := source.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		b, err := target.HGetAll(ctx, key).Result()
		if err != nil {
			return err
		}
		if !equalMap(a, b) {
			return fmt.Errorf("hash value mismatch")
		}
	case "set":
		a, err := source.SMembers(ctx, key).Result()
		if err != nil {
			return err
		}
		b, err := target.SMembers(ctx, key).Result()
		if err != nil {
			return err
		}
		sort.Strings(a)
		sort.Strings(b)
		if !equalStrings(a, b) {
			return fmt.Errorf("set value mismatch")
		}
	case "zset":
		a, err := source.ZRangeWithScores(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		b, err := target.ZRangeWithScores(ctx, key, 0, -1).Result()
		if err != nil {
			return err
		}
		if len(a) != len(b) {
			return fmt.Errorf("zset length mismatch")
		}
		for i := range a {
			if fmt.Sprint(a[i].Member) != fmt.Sprint(b[i].Member) || a[i].Score != b[i].Score {
				return fmt.Errorf("zset value mismatch")
			}
		}
	case "stream":
		a, err := source.XRange(ctx, key, "-", "+").Result()
		if err != nil {
			return err
		}
		b, err := target.XRange(ctx, key, "-", "+").Result()
		if err != nil {
			return err
		}
		if len(a) != len(b) {
			return fmt.Errorf("stream length mismatch local=%d cloud=%d", len(a), len(b))
		}
		for i := range a {
			if a[i].ID != b[i].ID || !equalAnyMap(a[i].Values, b[i].Values) {
				return fmt.Errorf("stream message mismatch at index %d", i)
			}
		}
	default:
		return fmt.Errorf("unsupported redis type %q", sourceType)
	}

	sourceTTL, err := source.PTTL(ctx, key).Result()
	if err != nil {
		return err
	}
	targetTTL, err := target.PTTL(ctx, key).Result()
	if err != nil {
		return err
	}
	if (sourceTTL < 0) != (targetTTL < 0) {
		return fmt.Errorf("ttl mode mismatch local=%s cloud=%s", sourceTTL, targetTTL)
	}
	if sourceTTL > 0 && targetTTL > 0 {
		delta := math.Abs(float64(sourceTTL - targetTTL))
		if delta > float64(5*time.Second) {
			return fmt.Errorf("ttl mismatch local=%s cloud=%s", sourceTTL, targetTTL)
		}
	}
	return nil
}

func allKeys(ctx context.Context, client *redis.Client) ([]string, error) {
	var cursor uint64
	var keys []string
	for {
		batch, next, err := client.Scan(ctx, cursor, "*", 500).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func loadEnvFile(path string) (fileConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", abs, err)
	}
	out := fileConfig{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		out[strings.TrimSpace(line[:idx])] = line[idx+1:]
	}
	return out, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func equalAnyMap(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if fmt.Sprint(value) != fmt.Sprint(b[key]) {
			return false
		}
	}
	return true
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
