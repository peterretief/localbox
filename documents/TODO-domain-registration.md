# Domain Registration TODO

## Goal

Add a safe domain ordering flow to LocalBox:

```text
Search domain
-> create order
-> take payment
-> verify payment webhook
-> register domain
-> provision LocalBox domain, mail, DNS, and site
```

For South Africa, `.co.za` will need a registrar/reseller API separate from Cloudflare. Cloudflare can still be useful for supported TLDs and DNS automation.

## Milestone 1: Order System

- Add an `Orders` admin page.
- Store orders with status:
  - `draft`
  - `pending_payment`
  - `paid`
  - `registering`
  - `active`
  - `failed`
  - `refund_needed`
- Save selected domain, owner email, price, payment reference, and registrar reference.
- Add order logs so every step is visible.

## Milestone 2: Domain Search

- Add domain search on the Add Domain page.
- Support `.com`, `.net`, and `.org` through Cloudflare availability API.
- Add provider interface placeholder for `.co.za`.
- Show availability, registration price, renewal price, and unsupported domains clearly.

## Milestone 3: Payment Gateway

- Choose first payment provider, likely Yoco for MVP.
- Create payment checkout from an order.
- Add payment webhook endpoint.
- Verify webhook signatures.
- Move order from `pending_payment` to `paid` only after verified payment.
- Keep manual "mark paid" admin fallback for testing.

## Milestone 4: Registrar Integration

- Add `RegistrarProvider` interface:
  - `CheckAvailability`
  - `RegisterDomain`
  - `GetDomainStatus`
  - `ConfigureNameservers`
- Implement Cloudflare provider for supported TLDs.
- Investigate and choose `.co.za` registrar/reseller API.
- Add manual registrar fallback for `.co.za` while API provider is not ready.

## Milestone 5: Safe Registration Flow

- After payment, re-check domain availability.
- If unavailable, mark order `refund_needed`.
- If available, start registration.
- Store registrar result and domain expiry date.
- Handle registration failure without creating the LocalBox domain too early.

## Milestone 6: LocalBox Provisioning

- Once registration succeeds, automatically:
  - create LocalBox domain
  - create owner mailbox
  - add domain admin role
  - create default website
  - create contact form target
  - add domain to login dropdown
- Reuse the template/provisioning code where possible.

## Milestone 7: DNS Automation

- Add DNS provider interface.
- For Cloudflare-managed domains, create:
  - `A` record for root domain
  - `A` or `CNAME` for `www`
  - `MX` records
  - `TXT` SPF record
  - optional DKIM/DMARC later
- For `.co.za`, support manual DNS instructions first if registrar DNS API is not ready.

## Milestone 8: Admin Controls

- Add admin screens for:
  - payment settings
  - registrar settings
  - DNS settings
  - orders
  - retry failed registration
  - refund/manual handling notes
- Store API keys in `/etc/localbox/*.conf`, not directly in app source or templates.

## Milestone 9: Notifications

- Email customer/order owner when:
  - payment is received
  - domain registration starts
  - domain is active
  - registration failed
  - manual action is needed
- Log all outgoing notifications.

## Milestone 10: Hardening

- Add idempotency so webhooks/retries do not double-register domains.
- Add audit log for admin actions.
- Add rate limiting on domain search and payment endpoints.
- Add tests for payment webhook validation and order state transitions.
- Add backup/export support for orders and registrar metadata.

## MVP Target

Build this first:

```text
Search domain
-> create order
-> pay with Yoco
-> webhook marks paid
-> admin manually registers .co.za or automated Cloudflare registers .com
-> LocalBox provisions domain/mail/site
```

This keeps risk low while proving the full business flow.
