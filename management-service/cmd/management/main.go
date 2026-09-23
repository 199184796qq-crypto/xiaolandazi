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
	"livecompanion/management/internal/mailer"
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
	if err := store.MigrateOrganizations(ctx); err != nil {
		log.Fatalf("migrate organization database: %v", err)
	}
	if err := store.MigrateAuth(ctx); err != nil {
		log.Fatalf("migrate authentication database: %v", err)
	}
	if err := store.MigrateCommercial(ctx); err != nil {
		log.Fatalf("migrate commercial database: %v", err)
	}
	if err := store.MigrateOperatingFinance(ctx); err != nil {
		log.Fatalf("migrate operating finance database: %v", err)
	}
	if err := store.MigrateSales(ctx); err != nil {
		log.Fatalf("migrate sales database: %v", err)
	}

	if cfg.Env == "development" {
		if _, err := store.EnsureDevelopmentSeed(
			ctx,
			cfg.DevTenantCode,
			cfg.DevTenantName,
		); err != nil {
			log.Fatalf("seed development tenant: %v", err)
		}
		if err := store.EnsureDefaultTimeCards(ctx); err != nil {
			log.Fatalf("seed default time cards: %v", err)
		}
	}

	defaultAdminHash, err := auth.HashPassword("12345678")
	if err != nil {
		log.Fatalf("hash default admin password: %v", err)
	}
	if err := store.EnsureDefaultAdmin(
		ctx,
		"Admin",
		"超级系统管理员",
		defaultAdminHash,
	); err != nil {
		log.Fatalf("seed default admin: %v", err)
	}
	if err := store.MigrateStaff(ctx); err != nil {
		log.Fatalf("migrate staff database: %v", err)
	}

	if err := store.MigrateInvitations(ctx); err != nil {
		log.Fatalf("migrate invitations database: %v", err)
	}
	if err := store.MigrateResources(ctx); err != nil {
		log.Fatalf("migrate organization resource database: %v", err)
	}
	if err := store.MigrateInventory(ctx); err != nil {
		log.Fatalf("migrate inventory database: %v", err)
	}
	if cfg.Env == "development" {
		if err := store.EnsureDefaultWarehouse(ctx); err != nil {
			log.Fatalf("seed default warehouse: %v", err)
		}
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
	mailerClient := mailer.New(mailer.Config{
		Host:      cfg.SMTPHost,
		Port:      cfg.SMTPPort,
		Username:  cfg.SMTPUsername,
		Password:  cfg.SMTPPassword,
		FromEmail: cfg.SMTPFromEmail,
		FromName:  cfg.SMTPFromName,
		TLSMode:   cfg.SMTPTLSMode,
	})
	api := httpapi.New(store, authResolver, core, auditStore, mailerClient, cfg.Env, cfg.AvatarDir, cfg.PublicWebURL)

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
