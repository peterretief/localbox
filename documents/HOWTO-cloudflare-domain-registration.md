# Cloudflare Domain Registration HOWTO

## Goal

Use Cloudflare Registrar for non-`.za` domains from inside LocalBox.

The safe flow is:

```text
Cloudflare token setup
-> LocalBox stores token
-> user searches domain
-> LocalBox checks Cloudflare availability/pricing
-> user pays
-> LocalBox re-checks availability
-> LocalBox registers domain
-> LocalBox creates Cloudflare zone and tunnel route
-> LocalBox provisions mailbox/site
```

`.co.za` and other `.za` domains should use Domains.co.za instead.

## 1. Create Cloudflare Token

In Cloudflare:

1. Go to **Manage Account > API Tokens**.
2. Select **Create Token**.
3. Use a **custom token**, not the global API key.
4. Name it:

```text
LocalBox Registrar
```

5. Grant only the permissions LocalBox needs:
   - registrar/domain registration access for the account
   - zone create/edit access
   - DNS edit access

Cloudflare token docs:

```text
https://developers.cloudflare.com/fundamentals/api/get-started/create-token/
```

Important: the token secret is only shown once. Store it immediately.

## 2. Store Token On LocalBox

Store Cloudflare credentials outside the app source:

```ini
# /etc/localbox/cloudflare.conf
enabled=true
api_url=https://api.cloudflare.com/client/v4
account_id=your_cloudflare_account_id
api_token=your_cloudflare_api_token
```

Set permissions:

```sh
sudo chown root:mail /etc/localbox/cloudflare.conf
sudo chmod 640 /etc/localbox/cloudflare.conf
sudo lbu add /etc/localbox/cloudflare.conf
sudo lbu commit -d
```

## 3. Verify Token

LocalBox should provide a **Test Cloudflare** admin button.

The test should call:

```text
GET /user/tokens/verify
```

If the token is valid, store a successful check timestamp in the admin UI.

## 4. Domain Search

LocalBox currently exposes a read-only public endpoint:

```text
GET /api/domain/check?domain=example.com
```

Routing:

```text
.co.za / .org.za / .net.za / .web.za -> Domains.co.za
other domains -> Cloudflare Registrar
```

When a user searches for a domain, LocalBox should call Cloudflare's registrar search endpoint:

```text
GET /accounts/{account_id}/registrar/domain-search
```

For a selected domain, call the real-time check endpoint:

```text
POST /accounts/{account_id}/registrar/domain-check
```

Use `domain-check` again immediately before registration, because availability can change between search and payment.

Cloudflare Registrar API:

```text
https://developers.cloudflare.com/api/resources/registrar/
```

## 5. Payment First

LocalBox should never call Cloudflare registration directly from a public button.

Create an order first:

```text
pending_payment
```

Then send the user to the payment gateway.

Only after the verified payment webhook returns successfully should the order move to:

```text
paid
```

## 6. Register Domain

After payment:

1. Re-check availability with `domain-check`.
2. If unavailable, mark the order:

```text
refund_needed
```

3. If available, call:

```text
POST /accounts/{account_id}/registrar/registrations
```

4. Store the Cloudflare registration ID/status on the order.

Registration may be asynchronous, so LocalBox should poll:

```text
GET /accounts/{account_id}/registrar/registrations/{domain_name}/registration-status
```

## 7. DNS And Tunnel Setup

This LocalBox does not use a public Raspberry Pi IP address. Public web traffic reaches the Pi through Cloudflare Tunnel.

Do not create `A` records pointing to the Pi. Instead, create Cloudflare Tunnel hostname routes, which create CNAME records to the tunnel.

Required public web routes:

```text
domain.example        -> Cloudflare Tunnel
www.domain.example    -> Cloudflare Tunnel
```

Manual command on the Pi:

```sh
sudo localbox-add-tunnel-route example.com your-tunnel-name-or-id
```

Or configure the tunnel once:

```ini
# /etc/localbox/cloudflare-tunnel.conf
tunnel=your-tunnel-name-or-id
```

Then:

```sh
sudo lbu add /etc/localbox/cloudflare-tunnel.conf
sudo lbu commit -d
sudo localbox-add-tunnel-route example.com
```

The helper uses:

```sh
cloudflared tunnel route dns TUNNEL HOSTNAME
```

This creates the Cloudflare DNS CNAME route to the tunnel.

If `cloudflared` reports that it is not authenticated for DNS route changes, run Cloudflare's login step once on the Pi as the user that will manage the route, or add API-based DNS route creation to the LocalBox order worker later.

Mail-related records are separate from the HTTP tunnel:

```text
MX     @           mail target, if inbound mail is public
TXT    @           SPF record
TXT    _dmarc      DMARC policy
TXT    DKIM host   DKIM public key
```

Normal SMTP inbound mail cannot use the basic HTTP Cloudflare Tunnel route. If LocalBox is only using the tunnel for the website, outbound mail can still use Brevo and inbound mail needs either LocalBox reachable through a proper mail path or another mailbox provider.

Cloudflare zone and DNS docs:

```text
https://developers.cloudflare.com/api/resources/zones/
https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/routing-to-tunnel/dns/
```

## 8. Provision LocalBox

Once Cloudflare confirms the domain is active:

- create LocalBox domain
- create owner mailbox
- add domain admin role
- create default website
- create contact form target
- add domain to login dropdown

This should reuse the existing LocalBox provisioning/template logic where possible.

## 9. Provider Routing

Use Cloudflare for global TLDs:

```text
.com
.net
.org
other Cloudflare-supported TLDs
```

Use Domains.co.za for South African domains:

```text
.co.za
.org.za
.net.za
.web.za
```

LocalBox should route by TLD through a registrar provider interface instead of hardcoding Cloudflare directly into the order flow.

## 10. Safety Rules

- Never expose the Cloudflare token in HTML or JavaScript.
- Keep credentials in `/etc/localbox/cloudflare.conf`.
- Use a custom API token, not the global API key.
- Verify payment before registering.
- Re-check availability immediately before registration.
- Make registration idempotent so webhook retries cannot buy the same domain twice.
- Keep an admin retry path for failed registrations.
- Log every order state change.

## MVP Flow

Build this first:

```text
Admin saves Cloudflare token
-> admin tests token
-> user searches .com domain
-> LocalBox creates order
-> user pays
-> payment webhook marks paid
-> LocalBox re-checks availability
-> LocalBox registers domain with Cloudflare
-> LocalBox creates Cloudflare Tunnel routes
-> LocalBox provisions mail/site
```
