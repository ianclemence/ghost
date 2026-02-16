---
name: "crypto"
description: "Checks cryptocurrency prices. Invoke when user asks 'Check Bitcoin price', 'What is ETH worth?', or 'Crypto market status'."
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
