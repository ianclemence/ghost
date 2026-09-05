---
name: currency
description: Convert amounts between currencies using real-time exchange rates. Invoke for any money-conversion or exchange-rate question, including "convert X to Y", "how much is X in Y", "$100 in euros", "500 baht to dollars", "USD to EUR", "what is X worth in Y", or "exchange rate". No API key required.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [currency, conversion, exchange-rate, finance]
prerequisites:
  commands: [python]
---

# Currency Converter

Uses open.er-api.com. No API key. Free tier covers 160+ currencies.

> **Preferred path:** Call the `currency_convert` tool with `from`, `to` (and `amount` when converting). It returns validated rates with provider fallback built in — use its output directly. Only use the `curl` commands below if the tool reports it is unavailable.

## Quick Reference

| Task | Command |
|------|---------|
| Convert | `python workspace/skills/currency/scripts/convert.py 100 USD EUR` |
| All rates for base | `curl -s "https://open.er-api.com/v6/latest/USD"` |
| Parse rate | `curl -s "https://open.er-api.com/v6/latest/USD" | python3 -c "import sys,json; print(json.load(sys.stdin)['rates']['EUR'])"` |

## Primary Method (Python)

```bash
python workspace/skills/currency/scripts/convert.py 100 USD EUR
```

## Manual Method

```bash
curl -s "https://open.er-api.com/v6/latest/USD" | python3 -c "
import sys,json
rates = json.load(sys.stdin)['rates']
amount = float('${AMOUNT}')
from_cur = '${FROM}'.upper()
to_cur = '${TO}'.upper()
result = amount * rates[to_cur] / rates[from_cur]
print(f'{amount} {from_cur} = {result:.2f} {to_cur}')
"
```

Replace `${AMOUNT}`, `${FROM}`, `${TO}` with values.

## Rate Limit

No auth required. Be respectful — cache results rather than re-querying on every request.

## Supported Currencies

USD, EUR, GBP, JPY, CAD, AUD, CHF, CNY, INR, and 150+ more. Full list in API response.
