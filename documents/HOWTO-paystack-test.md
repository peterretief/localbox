# Paystack Test Setup

Use Paystack test mode until the full order flow has been proven.

## Paystack Dashboard URLs

Set these under API Keys & Webhooks in Paystack test mode:

```text
Test Callback URL:
https://localboxmail.co.za/payment/paystack/return

Test Webhook URL:
https://localboxmail.co.za/payment/paystack/webhook
```

The callback URL is useful for the customer browser return. The webhook URL is the important one for automation. LocalBox only marks an order paid after server-side Paystack verification.

## Pi Config

On the Pi:

```sh
sudo cp /etc/localbox/examples/paystack.conf.example /etc/localbox/paystack.conf
sudo vi /etc/localbox/paystack.conf
sudo chown root:mail /etc/localbox/paystack.conf
sudo chmod 640 /etc/localbox/paystack.conf
sudo lbu add /etc/localbox/paystack.conf
sudo lbu commit -d
```

Example:

```ini
enabled=true
mode=test
api_url=https://api.paystack.co
currency=ZAR
domain_markup_percent=35
starter_price=150.00
public_key=pk_test_your-public-key
secret_key=sk_test_your-secret-key
callback_url=https://localboxmail.co.za/payment/paystack/return
webhook_url=https://localboxmail.co.za/payment/paystack/webhook
```

Keep the secret key out of HTML, JavaScript, screenshots, and git.

`currency` is the Paystack collection currency. Domain-registration checkout is collected in ZAR. Domains.co.za prices already arrive in ZAR. Cloudflare prices arrive in USD, so LocalBox adds `domain_markup_percent` to the USD amount and converts the result to ZAR before creating the Paystack checkout.

## Test Payment Cases

Use Paystack test mode cards from the official Paystack test payments documentation or the Paystack dashboard test helper.

Successful reusable card:

```text
4084 0840 8408 4081
Expiry: any future date, for example 06/27
CVV: 408
```

Also test the Paystack declined-card and bank-authentication-required scenarios from the current Paystack test helper. When a declined or failed transaction returns to LocalBox, the order is marked `payment_failed` and the browser shows a payment-not-completed page. Successful payments are still verified server-side before an order is marked `paid`.

## LocalBox Flow

1. Search for a domain on the site.
2. Click `Register now`.
3. Fill in the registrant details.
4. LocalBox saves the order details, re-checks the domain price, uses Domains.co.za ZAR prices as configured, adds `domain_markup_percent` only to Cloudflare USD quotes, converts those quotes to ZAR when needed, and redirects to Paystack.
5. Paystack calls `/payment/paystack/webhook`.
6. LocalBox verifies the webhook signature and verifies the transaction with Paystack.
7. LocalBox marks the order `paid`.
