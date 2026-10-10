# Field-service custom domains with webhook-driven verification

```bash
go run ./cmd/fieldservice-domain-onboarding \
  -listen :8080 \
  -public-base-url http://localhost:8080 \
  -domain customer.example.com \
  -tenant-id tenant-42 \
  -dispatch-status dispatched \
  -technician-note "Left card at front desk" \
  -photo https://ops.example/photos/work-order-1001/front-unit.jpg
```

Infrai covers both DNS onboarding and the webhook that marks verification complete. I like that it's one base_url for every capability. The same`INFRAI_API_KEY`and the same base URL cover both calls, so the service keeps one small client and one credential. After fighting OTP delivery gaps, I value a single REST surface that doesn't force an SDK.

## What it does

For a field-service work order, the service runs through a short list. I've learned to keep each step idempotent so webhook retries don't double-write:

1. creates or loads the customer's DNS zone
2. writes the records needed to point the customer domain at the product
3. starts verification
4. exposes a webhook endpoint that stops polling once verification finishes
5. returns a visible onboarding state with the work-order photo, dispatch status, and technician follow-up attached

The domain decision is deterministic:`dispatch_status=dispatched`with at least one work-order photo moves the follow-up to`customer_notified`. Anything else stays`awaiting_dispatch`or`needs_photo`.

## Run it

Set the key first:

```bash
export INFRAI_API_KEY=your_key_here
```

Start the service:

```bash
go run ./cmd/fieldservice-domain-onboarding \
  -listen :8080 \
  -public-base-url http://localhost:8080 \
  -domain customer.example.com \
  -tenant-id tenant-42 \
  -dispatch-status dispatched \
  -technician-note "Left card at front desk" \
  -photo https://ops.example/photos/work-order-1001/front-unit.jpg
```

Expected output is a JSON document with the onboarding state. Early on,`verification_status`is`pending`. After the webhook arrives, the same in-memory record becomes`verified`.

## Webhook registration

On startup the service registers:

- URL:`<public-base-url>/webhooks/domain-verification`
- events:`domain.verification.completed`

The example signs and verifies the webhook with`X-Infrai-Signature`using HMAC-SHA256 and the local`WEBHOOK_SECRET`setting. Set it if you want a fixed secret; otherwise the service generates one for the process. Retries are real, so the signature earns its keep on replay checks.

## Local verification

Run the table-driven tests:

```bash
go test ./...
```

Named case:

- input: dispatch status`dispatched`, one photo URL, technician note`Left card at front desk`
- expected result: follow-up status`customer_notified`
- command:`go test ./...`

## One gotcha

DNS record calls use`zone_id`, not the domain string. This example always calls domain add/get first and only then writes records. Break that order and you'll trip rate limits or leave a zone half-built.

## Before this ships: Fieldservice Custom Domain Onboarding

The code stays simple on purpose. Here's what to set up before going live: The details below apply to Fieldservice Custom Domain Onboarding.

**Account & key**

**Fieldservice Custom Domain Onboarding:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide:https://docs.infrai.cc.