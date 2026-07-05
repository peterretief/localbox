# LocalBox Mail Pricing Model

## Goal

Price LocalBox so it still works when free tiers become paid or usage grows.

Pricing should cover:

- direct customer costs
- shared infrastructure costs
- payment fees
- support time
- risk buffer
- profit

Basic formula:

```text
selling price = direct cost + infrastructure share + support cost + risk buffer + profit
```

## Product Ideas

### Starter

Simple first offer.

Includes:

- domain registration or existing domain connection
- one email address
- starter web page
- contact form
- LocalBox web inbox
- basic support

Possible pricing shape:

```text
Setup fee:      R____ once-off
Domain fee:     R____ per year
Service fee:    R____ per month or R____ per year
```

### Extra Mailboxes

For people who need more email addresses.

```text
Extra mailbox:  R____ per mailbox per month
```

### Website Add-On

For more than a starter page.

```text
Extra page setup:       R____ once-off
Small update support:   R____ per month
```

### File Sharing Add-On

Syncthing/shared folders.

```text
Setup fee:      R____ once-off
Storage fee:    R____ per month
Support fee:    R____ per month
```

### Own LocalBox Pi

Customer owns the device.

```text
Hardware cost:       R____
Setup labour:        R____
Support/month:       R____
Backup option/month: R____
```

### Rack-Hosted LocalBox Pi

Customer owns the Pi, LocalBox hosts it.

```text
Rack/power/network:  R____ per month
Support/month:       R____
Backup/month:        R____
```

## Cost Buckets

### Direct Per-Customer Costs

| Item | Cost | Billing Period | Notes |
| --- | ---: | --- | --- |
| Domain registration | R____ | yearly | Depends on TLD |
| Domain renewal | R____ | yearly | Include margin |
| Brevo sending | R____ | monthly | May be free at first |
| Cloudflare | R____ | monthly/yearly | DNS likely free, paid features may change |
| Storage | R____ | monthly | Mail, site, attachments, files |
| Backup storage | R____ | monthly | Local/offsite |
| Payment gateway fee | R____ | per payment | Percentage + fixed fee |
| Support time | R____ | monthly | Estimate minutes/customer |

### Shared Infrastructure Costs

| Item | Monthly Cost | Notes |
| --- | ---: | --- |
| Internet | R____ | Shared across customers |
| Power | R____ | Include UPS/rack load |
| Rack/space | R____ | If hosting Pis |
| Backup drives | R____ | Amortised |
| Spare hardware | R____ | Amortised |
| Monitoring/admin tools | R____ | If paid |
| Your admin time | R____ | Do not ignore this |

### Risk Buffer

Add a buffer for:

- provider price increases
- hardware failure
- refunds
- spam/email abuse
- extra support
- payment disputes
- domain renewal mistakes

Suggested starting buffer:

```text
15% - 30% of monthly cost
```

## Starter Package Worksheet

| Line Item | Amount |
| --- | ---: |
| Domain cost/year | R____ |
| Domain margin/year | R____ |
| Setup labour once-off | R____ |
| Monthly infrastructure share | R____ |
| Monthly support cost | R____ |
| Monthly Brevo/relay allowance | R____ |
| Monthly backup/storage allowance | R____ |
| Payment gateway fee allowance | R____ |
| Risk buffer | R____ |
| Profit | R____ |

Suggested customer price:

```text
Setup fee:   R____ once-off
Domain:      R____ per year
Service:     R____ per month
```

## Pricing Rules

- Do not include domain registration in a monthly fee unless the customer pays enough upfront to cover the first year.
- Keep domain fees separate or yearly so cancellation does not leave you carrying the registration cost.
- Do not depend on Brevo or Cloudflare staying free.
- Price support time explicitly.
- Keep the first offer simple enough to understand in one sentence.

## Suggested First Public Offer

```text
LocalBox Starter

Get your domain, one email address, and a starter web page set up for you.

Setup: R____ once-off
Domain: R____ per year
Service: R____ per month
```

## Notes To Fill In

```text
Actual .co.za cost from Domains.co.za:
Actual payment gateway fee:
Expected support minutes per customer per month:
Expected storage per customer:
Expected email sending per customer:
Minimum monthly profit per customer:
```
