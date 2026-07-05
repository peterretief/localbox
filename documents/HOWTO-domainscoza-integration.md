# Domains.co.za Integration HOWTO

## Goal

Use Domains.co.za for South African domain operations:

```text
.co.za
.org.za
.net.za
.web.za
```

Cloudflare can still be used for DNS hosting by setting the domain's nameservers to the Cloudflare nameservers assigned to the zone.

## API Basics

Live API:

```text
https://api.domains.co.za/api
```

Development API:

```text
https://lapi-dev.domains.co.za/api
```

Authentication:

```text
POST /login
```

Form fields:

```text
username
password
code optional, if 2FA is enabled
```

Successful login returns a JWT token. Subsequent calls use:

```text
Authorization: Bearer TOKEN
```

## LocalBox Config

Store credentials on the Pi:

```ini
# /etc/localbox/domainscoza.conf
enabled=true
api_url=https://api.domains.co.za/api
username=localbox
password=your-domains-coza-password

co_za_register_price=150.00
co_za_renew_price=150.00
org_za_register_price=150.00
org_za_renew_price=150.00
net_za_register_price=150.00
net_za_renew_price=150.00
web_za_register_price=150.00
web_za_renew_price=150.00
```

Permissions:

```sh
sudo chown root:mail /etc/localbox/domainscoza.conf
sudo chmod 640 /etc/localbox/domainscoza.conf
sudo lbu add /etc/localbox/domainscoza.conf
sudo lbu commit -d
```

## Domain Check

Endpoint:

```text
GET /domain/check
```

Query parameters:

```text
sld=mydomainname
tld=co.za
```

Important: use `isAvailable`, not `intReturnCode`, to decide availability.

LocalBox exposes this safely as:

```text
GET /api/domain/check?domain=mydomainname.co.za
```

This is read-only and safe for the public website.

## Update Nameservers

Endpoint:

```text
POST /domain/ns
```

Form fields:

```text
sld=mydomainname
tld=co.za
dns=custom
ns1=first.cloudflare.nameserver
ns2=second.cloudflare.nameserver
```

Domains.co.za may return:

```text
intReturnCode=2
strMessage=Successful Action Pending
```

That means the update has been queued and should be treated as pending, not failed.

## Cloudflare DNS Handoff

For a `.za` domain that is registered or transferred through Domains.co.za:

1. Create the zone in Cloudflare.
2. Read the two assigned Cloudflare nameservers.
3. Call Domains.co.za `POST /domain/ns`.
4. Wait for the nameserver update to complete.
5. Add a Cloudflare Tunnel route for the domain.

Do not use `cloudflared` as a nameserver. `cloudflared` is the tunnel daemon. Nameservers are the two Cloudflare hostnames assigned to the DNS zone, for example:

```text
abby.ns.cloudflare.com
jake.ns.cloudflare.com
```

After the domain uses Cloudflare nameservers, route web traffic to the LocalBox tunnel:

```sh
sudo localbox-add-tunnel-route example.co.za your-tunnel-name-or-id
```

or, if `/etc/localbox/cloudflare-tunnel.conf` contains `tunnel=...`:

```sh
sudo lbu add /etc/localbox/cloudflare-tunnel.conf
sudo lbu commit -d
sudo localbox-add-tunnel-route example.co.za
```

This creates CNAME routes for:

```text
example.co.za
www.example.co.za
```

Do not create A records to the Raspberry Pi. This setup has no public Pi IP address.

## LocalBox Safety Rules

- Public users may check availability only.
- Nameserver updates must be admin-only or order-worker-only.
- Do not update nameservers before payment/order confirmation.
- For transfers, do not update nameservers until the domain belongs to the reseller account or the API confirms the operation is allowed.
- Treat `intReturnCode=1` as immediate success and `intReturnCode=2` as queued/pending success.
- Log the API transaction UUID and order ID when nameserver updates are added to the order flow.
