package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func Configure(service string, filePath string) (func(), error) {
	service = strings.TrimSpace(service)
	if service == "" {
		service = "service"
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC)
	log.SetPrefix("[" + service + "] ")

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		log.SetOutput(os.Stdout)
		return func() {}, nil
	}

	cleanPath := filepath.Clean(filePath)
	parent := filepath.Dir(cleanPath)
	if parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
	}

	file, err := os.OpenFile(
		cleanPath,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	log.SetOutput(io.MultiWriter(os.Stdout, file))

	return func() {
		_ = file.Close()
	}, nil
}
