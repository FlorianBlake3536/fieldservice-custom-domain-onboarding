package fieldservice

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecideFollowUp(t *testing.T) {
	cases := []struct {
		name           string
		dispatchStatus string
		photos         []string
		want           string
	}{
		{
			name:           "dispatched with photo notifies customer",
			dispatchStatus: "dispatched",
			photos:         []string{"https://ops.example/photos/work-order-1001/front-unit.jpg"},
			want:           "customer_notified",
		},
		{
			name:           "dispatched without photo needs photo",
			dispatchStatus: "dispatched",
			photos:         nil,
			want:           "needs_photo",
		},
		{
			name:           "scheduled stays awaiting dispatch",
			dispatchStatus: "scheduled",
			photos:         []string{"https://ops.example/photos/work-order-1001/front-unit.jpg"},
			want:           "awaiting_dispatch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideFollowUp(tc.dispatchStatus, tc.photos)
			if got != tc.want {
				t.Fatalf("DecideFollowUp(%q, %v) = %q, want %q", tc.dispatchStatus, tc.photos, got, tc.want)
			}
		})
	}
}

func TestHandleVerificationWebhookMarksStateVerified(t *testing.T) {
	app := NewApp("test-key", "http://localhost:8080", "secret")
	app.onboardedState["customer.example.com"] = OnboardingState{
		Domain:             "customer.example.com",
		VerificationStatus: "pending",
	}

	body, err := json.Marshal(map[string]interface{}{
		"event": "domain.verification.completed",
		"data": map[string]string{"domain": "customer.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhooks/domain-verification", bytes.NewReader(body))
	req.Header.Set("X-Infrai-Signature", SignWebhookBody("secret", body))
	res := httptest.NewRecorder()

	app.HandleVerificationWebhook(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusAccepted)
	}

	got := app.onboardedState["customer.example.com"]
	if got.VerificationStatus != "verified" {
		t.Fatalf("verification status = %q, want verified", got.VerificationStatus)
	}
}
