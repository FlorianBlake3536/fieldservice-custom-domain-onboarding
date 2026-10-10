package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/fieldservice-custom-domain-onboarding/internal/fieldservice"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "HTTP listen address")
	publicBaseURL := flag.String("public-base-url", "http://localhost:8080", "public base URL for webhook registration")
	domain := flag.String("domain", "", "customer domain")
	tenantID := flag.String("tenant-id", "", "tenant identifier")
	dispatchStatus := flag.String("dispatch-status", "scheduled", "dispatch status")
	technicianNote := flag.String("technician-note", "", "technician follow-up note")
	photo := flag.String("photo", "", "work-order photo URL")
	flag.Parse()

	if *domain == "" || *tenantID == "" {
		log.Fatal("-domain and -tenant-id are required")
	}

	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	app := fieldservice.NewApp(apiKey, *publicBaseURL, webhookSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("/webhooks/domain-verification", app.HandleVerificationWebhook)
	mux.HandleFunc("/onboard", func(w http.ResponseWriter, r *http.Request) {
		state, err := app.OnboardDomain(r.Context(), fieldservice.WorkOrderInput{
			TenantID:         *tenantID,
			Domain:           *domain,
			DispatchStatus:   *dispatchStatus,
			TechnicianFollow: *technicianNote,
			Photos:           nonEmpty(*photo),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})

	server := &http.Server{Addr: *listenAddr, Handler: mux}

	go func() {
		log.Printf("listening on %s", *listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		time.Sleep(200 * time.Millisecond)
		state, err := app.OnboardDomain(context.Background(), fieldservice.WorkOrderInput{
			TenantID:         *tenantID,
			Domain:           *domain,
			DispatchStatus:   *dispatchStatus,
			TechnicianFollow: *technicianNote,
			Photos:           nonEmpty(*photo),
		})
		if err != nil {
			log.Printf("onboard failed: %v", err)
			return
		}
		_ = json.NewEncoder(os.Stdout).Encode(state)
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func nonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
