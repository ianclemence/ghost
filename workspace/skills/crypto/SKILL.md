---
name: crypto
description: Check real-time cryptocurrency prices and market data. Invoke when user asks "what is Bitcoin worth", "ETH price", "crypto market", "X coin price", or "Bitcoin price in USD". Uses CoinGecko API — no API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [crypto, cryptocurrency, bitcoin, ethereum, prices, finance]
prerequisites:
  commands: [curl]
---

# Crypto Ticker

Fetches real-time crypto prices using `curl` and a public API (CoinGecko).

## Commands

### Check Price (Simple)

Fetches the price of Bitcoin (BTC) in USD.

```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd"
```

### Check Specific Coin

Replace `ethereum` with any coin ID (e.g., `dogecoin`, `solana`).

```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=ethereum&vs_currencies=usd"
```
