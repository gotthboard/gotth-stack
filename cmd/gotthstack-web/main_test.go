package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServerConfiguration(t *testing.T) {
	server := newServer("127.0.0.1:0")
	if server.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q", server.Addr)
	}
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.WriteTimeout != 15*time.Second || server.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts = header:%s read:%s write:%s idle:%s", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, 1<<20)
	}
	if !server.DisableGeneralOptionsHandler {
		t.Error("general OPTIONS handler is enabled outside the site boundary")
	}
	if server.Handler == nil {
		t.Error("Handler is nil")
	}
}

func TestRunRejectsInvalidAddress(t *testing.T) {
	err := run(context.Background(), "invalid address")
	if err == nil || !strings.Contains(err.Error(), "missing port") {
		t.Fatalf("run error = %v, want missing port", err)
	}
}

func TestRunStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan error, 1)
	go func() { result <- run(ctx, "127.0.0.1:0") }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("run error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after cancellation")
	}
}
