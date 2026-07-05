# PayFast And Orders TODO

## Goal

Add a safe payment flow for LocalBox Starter:

```text
domain check
-> starter request
-> admin creates order
-> customer pays with PayFast
-> PayFast ITN confirms payment
-> order marked paid
-> domain/email/site setup continues
```

Do not register a domain from a browser redirect. Only trust a verified server-to-server PayFast ITN.

## Milestone 1: Order Storage

Create a simple order store before adding PayFast.

Recommended file path:

```text
/mnt/data/localbox/orders/orders.jsonl
```

Each order should include:

```text
id
created_at
updated_at
status
customer_name
customer_email
domain
domain_action
wanted_email
package
amount_cents
currency
notes
payment_provider
payment_reference
paid_at
admin_notes
```

Initial statuses:

```text
new
quoted
pending_payment
paid
provisioning
active
failed
cancelled
refund_needed
```

## Milestone 2: Admin Orders Page

Add an admin page:

```text
/orders
```

Admin actions:

- list orders
- view order
- create manual order
- update quote amount
- mark as quoted
- mark as cancelled
- add admin notes
- create PayFast payment link
- mark paid manually for EFT/testing

## Milestone 3: Convert Starter Requests To Orders

Current starter request forms send to the contact inbox.

Next step:

- keep contact inbox delivery
- also create an order record for starter setup requests
- set status to `new`
- show it in `/orders`

Do this before automated payment.

## Milestone 4: PayFast Config

Create:

```text
/etc/localbox/payfast.conf
```

Example:

```ini
enabled=true
sandbox=true
merchant_id=your_merchant_id
merchant_key=your_merchant_key
passphrase=your_passphrase_if_enabled
return_url=https://localboxmail.co.za/payment/return
cancel_url=https://localboxmail.co.za/payment/cancel
notify_url=https://localboxmail.co.za/payment/notify
```

Permissions:

```sh
sudo chown root:mail /etc/localbox/payfast.conf
sudo chmod 640 /etc/localbox/payfast.conf
sudo lbu add /etc/localbox/payfast.conf
sudo lbu commit -d
```

## Milestone 5: Create PayFast Checkout

Add admin action:

```text
Create PayFast payment
```

This should:

- require order status `quoted`
- require amount > 0
- create PayFast form fields
- include LocalBox order ID as `m_payment_id`
- set order status to `pending_payment`
- show/copy payment URL or redirect customer

Suggested PayFast fields:

```text
merchant_id
merchant_key
return_url
cancel_url
notify_url
m_payment_id
amount
item_name
item_description
name_first
email_address
signature
```

## Milestone 6: PayFast ITN/Webhook

Add endpoint:

```text
POST /payment/payfast/notify
```

ITN handler must:

- read form fields
- verify signature
- verify merchant ID/key
- verify amount matches the order
- verify payment status is complete
- optionally validate with PayFast server-to-server
- mark order `paid`
- store payment reference
- log raw ITN metadata safely

Important:

```text
Browser return_url is not proof of payment.
Only verified ITN marks an order paid.
```

## Milestone 7: Paid Order Actions

After an order is `paid`, admin can:

- re-check domain availability
- register domain through Domains.co.za or Cloudflare
- configure nameservers/DNS
- create LocalBox domain
- create owner mailbox
- publish starter page
- mark order `active`

Keep this admin-driven first. Full automation can come later.

Full post-payment automation plan:

```text
documents/TODO-post-payment-automation.md
```

The practical next step is an admin-triggered provisioning worker. The PayFast ITN should mark the order paid, then the worker should handle registrar/DNS/Brevo/LocalBox/welcome-email steps safely and idempotently.

## Milestone 8: Customer Pages

Later, add:

```text
/order/{id}
/payment/return
/payment/cancel
```

These pages should show:

- order status
- payment status
- next steps
- contact/support link

## MVP Build Order

Build in this order:

1. Order storage.
2. Admin orders page.
3. Starter request creates an order.
4. Manual quote amount.
5. Manual mark-paid.
6. PayFast config.
7. PayFast checkout generation.
8. PayFast ITN verification.
9. Paid order admin actions.

## Safety Rules

- Never register a domain before confirmed payment.
- Re-check domain availability after payment and before registration.
- Keep PayFast keys in `/etc/localbox/payfast.conf`.
- Do not expose PayFast passphrase in HTML.
- Make ITN handling idempotent so duplicate notifications do not double-mark or double-register.
- Log every order status change.
- Keep manual admin override for early testing.
