# LocalBox Mail Business Proposition Flow

## Positioning

LocalBox Mail gives a small business its own email, website, and domain setup without complicated hosting.

Core promise:

```text
You own the box. We manage the setup.
```

Plain-language offer:

```text
We help small businesses register or connect a domain, create email addresses, publish a simple website, and manage everything from a small private server.
```

Avoid leading with "Raspberry Pi hosting". Lead with the customer outcome:

- professional email
- simple website
- domain setup
- contact forms
- lower monthly cost
- local control
- practical support

## Target Customers

Start with small local customers who need simple email and a simple website:

- one-person businesses
- local shops
- tradespeople
- consultants
- churches and clubs
- guest houses
- local service providers

Do not target enterprise customers first. LocalBox should not initially be sold as a replacement for enterprise Microsoft 365 or Google Workspace uptime.

## Customer Entry Flow

The homepage should ask:

```text
What do you need?

[ ] I need a new domain and email
[ ] I already have a domain
[ ] I want a LocalBox Pi
```

Each option creates a different request/order flow.

## Flow 1: New Domain And Email

Use this when the customer does not already own a domain.

Customer steps:

1. Search for a domain.
2. Choose an available domain.
3. Choose a package.
4. Enter owner/contact details.
5. Pay setup/domain fee.
6. LocalBox registers the domain.
7. LocalBox configures DNS.
8. LocalBox creates mailbox and website.
9. Customer receives login details.

Admin/order states:

```text
new
pending_payment
paid
checking_availability
registering_domain
configuring_dns
provisioning_localbox
active
failed
refund_needed
```

Important rule:

```text
Always re-check domain availability after payment and before registration.
```

## Flow 2: Existing Domain, Connect Only

Use this when the customer owns a domain and wants to keep it at their current registrar.

Customer steps:

1. Enter existing domain.
2. Choose package.
3. Enter owner/contact details.
4. LocalBox checks current DNS.
5. Customer receives DNS instructions.
6. Customer updates nameservers or DNS records.
7. LocalBox verifies DNS.
8. LocalBox creates mailbox and website.

Admin/order states:

```text
new
waiting_for_dns_details
waiting_for_customer_dns
checking_dns
provisioning_localbox
active
failed
```

Recommended DNS options:

- change nameservers to Cloudflare, or
- keep current DNS provider and add required records manually.

This should be the first MVP flow because it is low risk and does not require payment automation before testing demand.

## Flow 3: Existing Domain Transfer

Use this when the customer owns a domain and wants LocalBox Mail to manage the registrar side.

Customer steps:

1. Enter existing domain.
2. Choose transfer option.
3. Enter owner/contact details.
4. Provide EPP/transfer code if required.
5. Pay transfer/setup fee.
6. LocalBox starts transfer with the correct registrar provider.
7. LocalBox waits for transfer completion.
8. LocalBox configures DNS.
9. LocalBox creates mailbox and website.

Admin/order states:

```text
new
waiting_for_epp_code
pending_payment
paid
transfer_started
waiting_for_transfer
configuring_dns
provisioning_localbox
active
failed
refund_needed
```

Build this after new registrations and connect-only domains. Transfers take longer and fail more often.

## Flow 4: Order A LocalBox Pi

Use this when the customer wants a physical LocalBox device.

Customer steps:

1. Choose Pi package.
2. Choose domain option:
   - new domain
   - existing domain connect
   - existing domain transfer
3. Enter delivery/contact details.
4. Choose mailboxes needed.
5. Pay hardware/setup fee.
6. LocalBox device is prepared.
7. Device is delivered or installed.
8. Remote admin is enabled through VPN.
9. Customer receives login details.

Admin/order states:

```text
new
pending_payment
paid
preparing_device
configuring_domain
configuring_mail
ready_for_delivery
delivered
active
support_required
```

## Product Packages

### Starter

For one-person businesses.

Includes:

- connect or register one domain
- 1 to 3 email addresses
- simple one-page website
- contact form
- LocalBox web inbox
- basic setup support

### Business

For small teams.

Includes:

- one domain
- up to 10 email addresses
- multi-page website
- contact forms
- Thunderbird/Outlook access over VPN
- backup setup
- monthly maintenance option

### Managed Pi

For businesses that want ownership.

Includes:

- Raspberry Pi LocalBox device
- domain and DNS setup
- email and website setup
- remote admin via VPN
- backup image
- optional monthly support

## Revenue Streams

### Once-Off Setup

Charge for:

- domain setup
- DNS configuration
- mailbox setup
- website setup
- contact form setup
- customer handover

### Hardware Sale

Charge for:

- Raspberry Pi
- SD card
- case
- power supply
- prepared LocalBox image
- setup labour

### Monthly Care

Charge for:

- updates
- backups
- monitoring
- domain/DNS changes
- email support
- small website edits

## Pricing Shape

Example starting range:

```text
Starter setup:        R750 - R1,500 once-off
Business setup:       R2,500 - R5,000 once-off
Managed Pi setup:     hardware cost + R2,500 setup
Monthly care:         R150 - R500/month
```

These are placeholders for review, not final prices.

## Homepage Draft

```markdown
# LocalBox Mail

Business email, website, and domain setup without complicated hosting.

We help small businesses register or connect a domain, create email addresses, publish a simple website, and manage everything from a small private server.

## What do you need?

- I need a new domain and email
- I already have a domain
- I want a LocalBox Pi

## What you get

- Professional email addresses
- A simple website
- Contact forms
- Domain and DNS setup
- LocalBox web inbox
- Optional Thunderbird or Outlook access over VPN
- Practical support

## Why LocalBox?

You own the box. We manage the setup.

LocalBox is designed for small businesses that want simple, understandable email and website hosting without complicated control panels.

## Get started

Tell us what you need and we will help you choose the right setup.
```

## MVP Build Order

1. Add "I already have a domain" request form.
2. Add "I need a new domain" request form.
3. Add "I want a LocalBox Pi" request form.
4. Store requests in the admin interface.
5. Email the owner when a request arrives.
6. Add manual admin status changes.
7. Add domain search APIs.
8. Add payment gateway.
9. Add automated registration.
10. Add automated provisioning.

## Safety Notes

- Do not register domains before payment is confirmed.
- Do not promise enterprise uptime at the start.
- Use Brevo or another relay for reliable outbound email.
- Use Cloudflare Tunnel for the public website/admin where appropriate.
- Do not expose LocalBox admin directly without authentication/VPN controls.
- Keep API keys in `/etc/localbox/*.conf`, not in templates or client-side code.
