package fieldservice

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

type WorkOrderInput struct {
	TenantID         string   `json:"tenant_id"`
	Domain           string   `json:"domain"`
	DispatchStatus   string   `json:"dispatch_status"`
	TechnicianFollow string   `json:"technician_follow_up"`
	Photos           []string `json:"photos"`
}

type OnboardingState struct {
	TenantID           string   `json:"tenant_id"`
	Domain             string   `json:"domain"`
	ZoneID             string   `json:"zone_id"`
	VerificationStatus string   `json:"verification_status"`
	DispatchStatus     string   `json:"dispatch_status"`
	FollowUpStatus     string   `json:"follow_up_status"`
	TechnicianFollow   string   `json:"technician_follow_up"`
	Photos             []string `json:"photos"`
	WebhookID          string   `json:"webhook_id"`
}

type App struct {
	infrai         *InfraiClient
	publicBaseURL  string
	webhookSecret  string
	mu             sync.Mutex
	onboardedState map[string]OnboardingState
}

func NewApp(apiKey, publicBaseURL, webhookSecret string) *App {
	if webhookSecret == "" {
		webhookSecret = randomHex(32)
	}
	return &App{
		infrai:         NewInfraiClient(apiKey),
		publicBaseURL:  strings.TrimRight(publicBaseURL, "/"),
		webhookSecret:  webhookSecret,
		onboardedState: map[string]OnboardingState{},
	}
}

func (a *App) OnboardDomain(ctx context.Context, input WorkOrderInput) (OnboardingState, error) {
	if input.Domain == "" || input.TenantID == "" {
		return OnboardingState{}, errors.New("tenant_id and domain are required")
	}

	zone, err := a.infrai.DNSDomainAdd(ctx, DNSDomainAddRequest{
		Domain:    input.Domain,
		AccountID: input.TenantID,
		Metadata: map[string]interface{}{
			"dispatch_status": input.DispatchStatus,
		},
	})
	if err != nil {
		zone, err = a.infrai.DNSDomainGet(ctx, DNSDomainGetRequest{Domain: input.Domain})
		if err != nil {
			return OnboardingState{}, err
		}
	}

	if err := a.infrai.DNSRecordUpsert(ctx, DNSRecordUpsertRequest{
		ZoneID:     zone.ZoneID,
		RecordType: "CNAME",
		Name:       input.Domain,
		Content:    "tenant-gateway.fieldservice.example",
		TTL:        300,
		Metadata: map[string]interface{}{
			"tenant_id": input.TenantID,
		},
	}); err != nil {
		return OnboardingState{}, err
	}

	if err := a.infrai.DNSRecordUpsert(ctx, DNSRecordUpsertRequest{
		ZoneID:     zone.ZoneID,
		RecordType: "TXT",
		Name:       "_fieldservice-verify." + input.Domain,
		Content:    "tenant=" + input.TenantID,
		TTL:        300,
		Metadata: map[string]interface{}{
			"dispatch_status": input.DispatchStatus,
		},
	}); err != nil {
		return OnboardingState{}, err
	}

	webhook, err := a.infrai.RegisterWebhook(ctx, WebhookRegisterRequest{
		URL:         a.publicBaseURL + "/webhooks/domain-verification",
		Events:      []string{"domain.verification.completed"},
		Description: "field-service custom domain verification",
		Secret:      a.webhookSecret,
	})
	if err != nil {
		return OnboardingState{}, err
	}

	if err := a.infrai.DNSDomainVerify(ctx, DNSDomainVerifyRequest{Domain: input.Domain}); err != nil {
		return OnboardingState{}, err
	}

	state := OnboardingState{
		TenantID:           input.TenantID,
		Domain:             input.Domain,
		ZoneID:             zone.ZoneID,
		VerificationStatus: "pending",
		DispatchStatus:     input.DispatchStatus,
		FollowUpStatus:     DecideFollowUp(input.DispatchStatus, input.Photos),
		TechnicianFollow:   input.TechnicianFollow,
		Photos:             input.Photos,
		WebhookID:          webhook.ID,
	}

	a.mu.Lock()
	a.onboardedState[input.Domain] = state
	a.mu.Unlock()

	return state, nil
}

func DecideFollowUp(dispatchStatus string, photos []string) string {
	switch dispatchStatus {
	case "dispatched":
		if len(photos) == 0 {
			return "needs_photo"
		}
		return "customer_notified"
	default:
		return "awaiting_dispatch"
	}
}

type verificationWebhook struct {
	Event string `json:"event"`
	Data  struct {
		Domain string `json:"domain"`
	} `json:"data"`
}

func (a *App) HandleVerificationWebhook(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	payload := json.NewDecoder(r.Body)
	var event verificationWebhook
	if err := payload.Decode(&event); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Infrai-Signature")
	bodyBytes, _ := json.Marshal(event)
	if !validSignature(a.webhookSecret, bodyBytes, sig) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	a.mu.Lock()
	state, ok := a.onboardedState[event.Data.Domain]
	if ok {
		state.VerificationStatus = "verified"
		a.onboardedState[event.Data.Domain] = state
	}
	a.mu.Unlock()

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func validSignature(secret string, body []byte, got string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(got))
}

func SignWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	if err != nil {
		panic(fmt.Errorf("random secret: %w", err))
	}
	return hex.EncodeToString(buf)
}
