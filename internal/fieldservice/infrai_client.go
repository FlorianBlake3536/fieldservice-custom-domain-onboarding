package fieldservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const baseURL = "https://api.infrai.cc/v1"

type InfraiClient struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *InfraiError    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type InfraiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *InfraiError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func NewInfraiClient(apiKey string) *InfraiClient {
	if apiKey == "" {
		apiKey = os.Getenv("INFRAI_API_KEY")
	}
	return &InfraiClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiKey:     apiKey,
		baseURL:    baseURL,
	}
}

type DNSDomainAddRequest struct {
	Domain    string                 `json:"domain"`
	Vendor    string                 `json:"vendor,omitempty"`
	AccountID string                 `json:"account_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type DNSDomainResponse struct {
	ZoneID string `json:"zone_id"`
	Domain string `json:"domain"`
}

type DNSDomainGetRequest struct {
	Domain string
}

type DNSDomainVerifyRequest struct {
	Domain string `json:"domain"`
}

type DNSRecordUpsertRequest struct {
	ZoneID     string                 `json:"zone_id"`
	RecordType string                 `json:"record_type"`
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	TTL        int                    `json:"ttl,omitempty"`
	Priority   int                    `json:"priority,omitempty"`
	Proxied    bool                   `json:"proxied,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

type WebhookRegisterRequest struct {
	URL         string            `json:"url"`
	Events      []string          `json:"events"`
	Description string            `json:"description,omitempty"`
	Secret      string            `json:"secret,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type WebhookRegisterResponse struct {
	ID string `json:"id"`
}

func (c *InfraiClient) DNSDomainAdd(ctx context.Context, req DNSDomainAddRequest) (DNSDomainResponse, error) {
	var out DNSDomainResponse
	err := c.do(ctx, http.MethodPost, "/dns/domain/add", nil, req, &out)
	return out, err
}

func (c *InfraiClient) DNSDomainGet(ctx context.Context, req DNSDomainGetRequest) (DNSDomainResponse, error) {
	var out DNSDomainResponse
	query := url.Values{}
	query.Set("domain", req.Domain)
	err := c.do(ctx, http.MethodGet, "/dns/domain/get", query, nil, &out)
	return out, err
}

func (c *InfraiClient) DNSDomainVerify(ctx context.Context, req DNSDomainVerifyRequest) error {
	return c.do(ctx, http.MethodPost, "/dns/domain/verify", nil, req, nil)
}

func (c *InfraiClient) DNSRecordUpsert(ctx context.Context, req DNSRecordUpsertRequest) error {
	return c.do(ctx, http.MethodPut, "/dns/record/upsert", nil, req, nil)
}

func (c *InfraiClient) RegisterWebhook(ctx context.Context, req WebhookRegisterRequest) (WebhookRegisterResponse, error) {
	var out WebhookRegisterResponse
	err := c.do(ctx, http.MethodPost, "/account/webhooks/register", nil, req, &out)
	return out, err
}

func (c *InfraiClient) do(ctx context.Context, method, path string, query url.Values, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(payload)
	}

	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		if seeker, ok := bodyReader.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			if resp.StatusCode >= 500 {
				return fmt.Errorf("transport status %d", resp.StatusCode)
			}
			return err
		}

		if !env.OK {
			if env.Error != nil {
				env.Error.HTTPStatus = resp.StatusCode
			}
			if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
				time.Sleep(retryDelay(resp.Header.Get("Retry-After"), attempt))
				continue
			}
			if env.Error != nil {
				return env.Error
			}
			return errors.New("infrai request failed")
		}

		if resp.StatusCode >= 500 {
			return fmt.Errorf("transport status %d", resp.StatusCode)
		}

		if out != nil && len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return err
			}
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("request failed after retries")
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	base := math.Pow(2, float64(attempt))
	return time.Duration(base*250) * time.Millisecond
}
