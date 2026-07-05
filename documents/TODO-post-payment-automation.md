# Post-Payment Automation TODO

## Goal

After a verified payment, LocalBox should automatically create the customer's starter setup:

```text
PayFast ITN verified
-> lock order
-> re-check domain
-> register domain with customer registrant details
-> configure admin/tech/billing contact as LocalBox
-> route web traffic through Cloudflare Tunnel
-> configure outbound sending domain/address
-> configure inbound receiving path
-> create LocalBox domain, mailbox, and default website
-> send welcome email
-> mark order active
```

The automation must be retryable. A failed API call must leave the order in a known state so an admin can retry from the failed step.

## Required Order Data

Capture this before payment:

```text
order_id
customer_name
customer_email
customer_phone
customer_company_optional
customer_address_line_1
customer_address_line_2_optional
customer_city
customer_province_or_state
customer_postal_code
customer_country
domain
domain_provider
wanted_mailbox
package
billing_period
amount_cents
currency=ZAR
terms_accepted_at
terms_version
```

For registrar records:

- registrant contact = customer details
- admin contact = LocalBox/admin details
- tech contact = LocalBox/admin details
- billing contact = LocalBox/admin details

Keep LocalBox/admin contact details in a private config file, for example:

```text
/etc/localbox/registrar-admin.conf
```

## Status Model

Recommended order statuses:

```text
pending_payment
paid
provisioning
active
failed
refund_needed
cancelled
```

Recommended provisioning step statuses:

```text
payment_verified
domain_rechecked
domain_registered
nameservers_set
cloudflare_zone_ready
cloudflare_tunnel_route_added
brevo_domain_added
brevo_dns_records_added
brevo_sender_added
inbound_email_route_added
localbox_domain_created
mailbox_created
default_site_created
welcome_email_sent
active
```

Each step should store:

```text
status
started_at
finished_at
attempt_count
last_error
provider_reference
raw_provider_uuid_if_available
```

## Worker Rule

The PayFast notification handler should not perform the whole setup directly.

It should:

1. Verify ITN signature and payment amount.
2. Mark order `paid`.
3. Append an order log event.
4. Queue or flag the order for provisioning.
5. Return success to PayFast quickly.

The provisioning worker should then process paid orders.

## Step 1: Re-Check Domain

Before registering:

- call the registrar availability check again
- confirm the domain is still available
- confirm the expected price is still acceptable

If unavailable:

```text
status = refund_needed
reason = domain no longer available
```

Do not create the LocalBox domain before registrar success.

## Step 2: Register Domain

For `.za` domains:

- use Domains.co.za
- registrant = customer details
- admin/tech/billing = LocalBox admin details

For global domains:

- use Cloudflare Registrar where supported
- registrant = customer details
- admin/tech/billing handling depends on Cloudflare's current registrar API requirements

Store registrar identifiers and expiry date.

## Step 3: Cloudflare Zone And Nameservers

If Cloudflare manages DNS:

1. Ensure Cloudflare zone exists.
2. Read assigned Cloudflare nameservers.
3. For Domains.co.za domains, update nameservers through Domains.co.za.
4. Wait for Cloudflare zone/nameserver status if required.

Do not create `A` records to the Raspberry Pi. The Pi has no public IP address.

## Step 4: Cloudflare Tunnel Route

There are two separate pieces:

1. DNS route to the tunnel:

```sh
cloudflared tunnel route dns TUNNEL example.co.za
cloudflared tunnel route dns TUNNEL www.example.co.za
```

2. Tunnel ingress rule to the local web server:

```yaml
ingress:
  - hostname: example.co.za
    service: http://127.0.0.1:80
  - hostname: www.example.co.za
    service: http://127.0.0.1:80
  - service: http_status:404
```

If the current tunnel already has a wildcard/catch-all rule to `http://127.0.0.1:80`, only the DNS route may be needed. Otherwise LocalBox needs an automated way to update the tunnel config and restart `cloudflared`.

## Step 5: Brevo Outbound Sending

For outbound mail:

1. Add or verify the customer's sending domain in Brevo.
2. Get Brevo DNS records for SPF/DKIM/domain verification.
3. Add required TXT/CNAME records to Cloudflare DNS.
4. Add/verify the sender address, for example:

```text
hello@example.co.za
```

5. Store Brevo domain/sender verification status.

Important: do not assume Brevo is verified immediately. The worker may need to poll or leave the order in `waiting_brevo_verification`.

## Step 6: Inbound Email Receiving

Choose one clear inbound path per domain.

Option A: LocalBox receives mail directly.

- create mailbox locally
- configure MX records to the correct reachable mail path
- only use this if public inbound SMTP is actually reachable

Option B: Cloudflare Email Routing/Worker receives mail and forwards/posts to LocalBox.

- enable Cloudflare Email Routing for the zone
- add destination/routing rule for the customer mailbox
- configure Worker route/action if using a Worker to deliver into LocalBox
- add required MX/TXT records in Cloudflare

This needs a separate design because Cloudflare Email Routing and Workers are different from Cloudflare Tunnel HTTP routing.

## Step 7: LocalBox Provisioning

Once the domain and routing are ready:

- create LocalBox domain folder
- create owner mailbox
- create domain admin role for the customer
- create default editable website
- create contact page/thank-you page
- add domain to login dropdown
- set permissions and persist Alpine state with `lbu`

Reuse existing provisioning/template logic where possible.

## Step 8: Welcome Email

Send a welcome email to the customer after LocalBox provisioning succeeds.

Include:

```text
domain
website URL
mailbox address
LocalBox login URL
temporary password or setup link
how to edit the starter website
support/contact address
terms link
backup/migration options
```

Do not send passwords in plain text long term. For MVP, a temporary password is acceptable only if the user is required to change it on first login.

## Idempotency Rules

Every external action needs a stable key:

```text
localbox_order_id
domain
provider_reference
```

Before doing a step, check whether it was already completed.

Examples:

- if domain already registered under the account, store reference and continue
- if Cloudflare route already exists, continue
- if Brevo domain already exists, continue
- if LocalBox mailbox already exists, continue
- if welcome email was already sent, do not send it again unless admin requests resend

## Admin Controls

Add an order detail page showing:

- payment status
- domain registration status
- DNS/tunnel status
- Brevo status
- inbound mail status
- LocalBox provisioning status
- welcome email status
- retry failed step
- mark refund needed
- admin notes

## First Build Target

Build this first:

```text
verified PayFast payment
-> paid order
-> admin button: Run provisioning
-> automated LocalBox domain/mail/default website
-> automated Cloudflare tunnel route
-> welcome email
```

Then add registrar automation and Brevo automation once the order state machine is solid.

This avoids risking paid domain registration while the worker is still new.
