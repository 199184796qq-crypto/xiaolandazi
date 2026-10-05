package main

import (
	"errors"
	"log"
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()
	store, err := newBindingStore(cfg.bindingsFile)
	if err != nil {
		log.Fatalf("load xiaozhi bindings: %v", err)
	}
	api := newGateway(cfg, store)
	server := &http.Server{
		Addr:              cfg.addr,
		Handler:           api.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf(
		"xiaozhi-gateway listening on %s websocket=%s core=%s bindings=%s",
		cfg.addr,
		cfg.publicWSURL,
		cfg.coreBaseURL,
		cfg.bindingsFile,
	)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
