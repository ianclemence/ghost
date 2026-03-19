---
name: currency
description: Convert amounts between currencies using real-time exchange rates. Invoke when user asks "convert X to Y", "how much is USD in EUR", "exchange rate", or "currency conversion". No API key required.
version: 1.0.0
author: Ghost
license: MIT
metadata:
  ghost:
    tags: [currency, conversion, exchange-rate, finance]
prerequisites:
  commands: [python]
---

# Currency Converter

## Tools

- **Exchange Rate API**: `https://open.er-api.com/v6/latest/{BASE_CURRENCY}` (Free, no key required).
- **PowerShell**: Used to fetch and parse JSON data.

## Cross-Platform Method (Python)

Works on Windows, Linux, and Mac.

1.  **Run Script**:
    ```bash
    # Usage: python convert.py <amount> <from> <to>
    python workspace/skills/currency/scripts/convert.py 100 USD EUR
    ```

## Workflow (PowerShell Only)

1.  **Fetch Rates**:
    - Use `Invoke-RestMethod` in PowerShell to get the latest rates for a base currency (default: USD).
      ```powershell
      $rates = Invoke-RestMethod "https://open.er-api.com/v6/latest/USD"
      ```

2.  **Calculate**:
    - Get the rate for the target currency (e.g., EUR).
    - Multiply the amount by the rate.
      ```powershell
      $amount = 100
      $targetRate = $rates.rates.EUR
      $result = $amount * $targetRate
      Write-Output "100 USD = $result EUR"
      ```

3.  **Example Command**:
    - To convert 50 USD to GBP:
      ```powershell
      $r = Invoke-RestMethod "https://open.er-api.com/v6/latest/USD"; $r.rates.GBP * 50
      ```

## Supported Currencies

- Common: USD, EUR, GBP, JPY, CAD, AUD, CHF, CNY, INR, etc.
- The API returns 160+ currencies.
