---
name: crypto
description: Check real-time cryptocurrency prices, market data, and price changes. Invoke when user asks "what is Bitcoin worth", "ETH price", "crypto market", "X coin price", "Bitcoin price in USD", or "crypto 24h change". Uses CoinGecko API — no API key required. Free tier: 50 req/min.
version: 1.1.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [crypto, cryptocurrency, bitcoin, ethereum, prices, finance]
prerequisites:
  commands: [curl]
---

# Crypto Ticker

CoinGecko API. No API key. Rate limit: 50 req/min on free tier.

> **Preferred path:** Call the `crypto_price` tool with `id` (e.g. `bitcoin`) and `vs` (default `USD`). It returns validated prices with provider fallback built in — use its output directly. Only use the `curl` commands below if the tool reports it is unavailable.

## Quick Reference

| Task                     | Command                                                                                                          |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| Single coin price        | `curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd"`                          |
| Multi-coin prices        | `curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum,solana&vs_currencies=usd,eur"`      |
| Single coin + 24h change | `curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd&include_24hr_change=true"` |
| Full market data         | `curl -s "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&ids=bitcoin"`                           |
| Search coins             | `curl -s "https://api.coingecko.com/api/v3/search?query=bitcoin"`                                                |

## Coin ID Mapping

CoinGecko uses full IDs, not ticker symbols. Common IDs:

| Ticker | CoinGecko ID  |
| ------ | ------------- |
| BTC    | bitcoin       |
| ETH    | ethereum      |
| SOL    | solana        |
| DOGE   | dogecoin      |
| ADA    | cardano       |
| XRP    | ripple        |
| DOT    | polkadot      |
| AVAX   | avalanche-2   |
| MATIC  | matic-network |

Find IDs at: `curl -s "https://api.coingecko.com/api/v3/search?query=NAME"` → use `coins[0].id`

## Output Parsing

Single price — extract value from JSON:

```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd" | python3 -c "import sys,json; print(json.load(sys.stdin)['bitcoin']['usd'])"
```

Multi-coin with 24h change:

```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd&include_24hr_change=true" | python3 -c "
import sys,json
data = json.load(sys.stdin)
for coin, vals in data.items():
    price = vals['usd']
    change = vals.get('usd_24h_change', 0)
    arrow = '▲' if change > 0 else '▼'
    print(f'{coin}: \${price:,.2f}  {arrow}{abs(change):.2f}%')
"
```

Full market response (includes market cap, volume, ATH):

```bash
curl -s "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&ids=bitcoin&per_page=1" | python3 -c "
import sys,json
d = json.load(sys.stdin)[0]
print(f'Price:      \${d[\"current_price\"]:,.2f}')
print(f'24h Change: {d[\"price_change_percentage_24h\"]:.2f}%')
print(f'Market Cap: \${d[\"market_cap\"]:,.0f}')
print(f'Volume:     \${d[\"total_volume\"]:,.0f}')
print(f'ATH:        \${d[\"ath\"]:,.2f}')
"
```

## Common Queries

### Bitcoin current price

```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd"
```

### Top 5 coins by market cap

```bash
curl -s "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=5&page=1&sparkline=false"
```

### Specific coin with custom fields

```bash
curl -s "https://api.coingecko.com/api/v3/coins/bitcoin?localization=false&tickers=false&community_data=false&developer_data=false"
```

## Rate Limit Handling

Free tier: 50 calls/minute. Cache results when possible — exchange rates don't change every second. If rate-limited, wait 60 seconds before retrying.

## Error Handling

| HTTP Code | Meaning                                            |
| --------- | -------------------------------------------------- |
| 200       | Success                                            |
| 429       | Rate limited — wait and retry                      |
| 404       | Coin ID not found — verify ID with search endpoint |
| 5xx       | Server error — wait and retry                      |
