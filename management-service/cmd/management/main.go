package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"livecompanion/management/internal/audit"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/coreclient"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/httpapi"
)

func main() {
	cfg := config.Load()

	store, err := appdb.Open(cfg)
	if err != nil {
		log.Fatalf("open management database: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate management database: %v", err)
	}
	if err := store.MigrateAuth(ctx); err != nil {
		log.Fatalf("migrate authentication database: %v", err)
	}

	if cfg.Env == "development" {
		if _, err := store.EnsureDevelopmentSeed(
			ctx,
			cfg.DevTenantCode,
			cfg.DevTenantName,
		); err != nil {
			log.Fatalf("seed development tenant: %v", err)
		}
	}

	defaultAdminHash, err := auth.HashPassword("12345678")
	if err != nil {
		log.Fatalf("hash default admin password: %v", err)
	}
	if err := store.EnsureDefaultAdmin(
		ctx,
		"Admin",
		"平台管理员",
		defaultAdminHash,
	); err != nil {
		log.Fatalf("seed default admin: %v", err)
	}
	_ = store.DeleteExpiredSessions(ctx, time.Now().UTC())

	auditStore := audit.New(
		net.JoinHostPort(cfg.RedisHost, cfg.RedisPort),
		cfg.RedisPassword,
		cfg.RedisDB,
		cfg.AuditLogLimit,
	)
	if err := auditStore.Ping(ctx); err != nil {
		log.Fatalf("connect management audit redis: %v", err)
	}
	defer auditStore.Close()

	authResolver := auth.NewResolver(cfg.Env, store)
	core := coreclient.New(cfg.CoreBaseURL, cfg.CoreToken)
	api := httpapi.New(store, authResolver, core, auditStore, cfg.Env)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("management-service listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
