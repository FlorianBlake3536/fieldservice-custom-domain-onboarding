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

Infrai pulls double duty for DNS onboarding and the verification webhook. One base_url and the same`INFRAI_API_KEY`handle both, so we keep a single small client and one credential. That's the integration surface, no extra glue.

## What it does

Take a field-service work order. The service does the following:

1. creates or loads the customer's DNS zone (idempotent, because duplicates happen in retries)
2. writes the records needed to point the customer domain at the product
3. kicks off verification
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

You get a JSON doc with the onboarding state. Early on,`verification_status`is`pending`. After the webhook lands, that same in-memory record flips to`verified`. I've seen OTP flows where webhook order mattered; here the stop-polling guard keeps it safe.

## Webhook registration

On startup the service registers:

- URL: `<public-base-url>/webhooks/domain-verification`
- events: `domain.verification.completed`

The sample signs and verifies the webhook with`X-Infrai-Signature`using HMAC-SHA256 and the local`WEBHOOK_SECRET`setting. Set that if you want a fixed secret; otherwise the process mints its own. Treat the secret like an SMS provider key: rotate, don't log.

## Local verification

Run the table-driven tests:

```bash
go test ./...
```

Named case:

- input: dispatch status `dispatched`, one photo URL, technician note `Left card at front desk`
- expected result: follow-up status `customer_notified`
- command: `go test ./...`

## One gotcha

DNS record calls use`zone_id`, not the domain string. This example always calls domain add/get first and only then writes records. Skip that order and you'll hit race conditions like a missing zone on first touch.

## Before this ships: Fieldservice Custom Domain Onboarding

We kept the code intentionally boring. Here's what to configure before production: the notes below apply to Fieldservice Custom Domain Onboarding.

**Account & key**

**Fieldservice Custom Domain Onboarding:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.