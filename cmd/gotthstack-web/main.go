package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gotthboard/gotth-stack/internal/site"
)

const defaultAddress = "127.0.0.1:8080"

// main binds process signals to the bounded public HTTP server lifecycle.
// Complexity: time Omega(1) and O(r) over runtime duration r, with no tighter
// useful Theta bound because shutdown time depends on external signal timing;
// auxiliary space O(1), Omega(1), tight Theta(1); delegated costs: net/http
// owns per-request allocations and goroutines.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("GOTTHSTACK_WEB_ADDR")
	if address == "" {
		address = defaultAddress
	}
	if err := run(ctx, address); err != nil {
		log.Printf("gotthstack-web: %v", err)
		os.Exit(1)
	}
}

// run serves until cancellation or a listener failure, then shuts down.
// Complexity: time Omega(1) and O(r+w) over runtime duration r and in-flight
// request work w, with no input-only tight Theta bound; auxiliary space O(c),
// Omega(1), where c is concurrent requests; delegated costs: net/http owns
// socket I/O, goroutines, and request buffers.
func run(ctx context.Context, address string) error {
	server := newServer(address)
	result := make(chan error, 1)
	go func() {
		result <- server.ListenAndServe()
	}()

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// newServer fixes the public process's transport time and handler boundaries.
// Complexity: time O(1), Omega(1), tight Theta(1); auxiliary space O(1),
// Omega(1), tight Theta(1); variables: all server fields and routes are fixed;
// delegated costs: site.NewHandler allocates its constant-size route table.
func newServer(address string) *http.Server {
	return &http.Server{
		Addr:                         address,
		Handler:                      site.NewHandler(),
		DisableGeneralOptionsHandler: true,
		ReadHeaderTimeout:            5 * time.Second,
		ReadTimeout:                  15 * time.Second,
		WriteTimeout:                 15 * time.Second,
		IdleTimeout:                  60 * time.Second,
		MaxHeaderBytes:               1 << 20,
	}
}
