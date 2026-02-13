---
name: crypto-price
description: "Get real-time cryptocurrency prices, charts, and market data. Supports BTC, ETH, and all major coins."
metadata: {"nanobot":{"emoji":"₿","requires":{"bins":["curl"]}}}
---

# Crypto Price

Get real-time crypto prices using free APIs (no API key required).

## CoinGecko API (primary, free, no key)

### Single coin price
```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd,vnd&include_24hr_change=true&include_market_cap=true"
```

### Multiple coins
```bash
curl -s "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum,solana,cardano,dogecoin&vs_currencies=usd,vnd&include_24hr_change=true"
```

### Top coins by market cap
```bash
curl -s "https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=20&page=1&sparkline=false"
```

### Coin details
```bash
curl -s "https://api.coingecko.com/api/v3/coins/bitcoin?localization=false&tickers=false&community_data=false&developer_data=false"
```

### Search coin by name
```bash
curl -s "https://api.coingecko.com/api/v3/search?query=solana"
```

### Price history (for charts)
```bash
# Last 30 days
curl -s "https://api.coingecko.com/api/v3/coins/bitcoin/market_chart?vs_currency=usd&days=30"
# Last 7 days
curl -s "https://api.coingecko.com/api/v3/coins/bitcoin/market_chart?vs_currency=usd&days=7"
```

## Common Coin IDs
| Coin | ID |
|------|----|
| Bitcoin | bitcoin |
| Ethereum | ethereum |
| BNB | binancecoin |
| Solana | solana |
| XRP | ripple |
| Cardano | cardano |
| Dogecoin | dogecoin |
| TRON | tron |
| Polygon | matic-network |
| Avalanche | avalanche-2 |

## Binance API (alternative, for trading pairs)
```bash
# Spot price
curl -s "https://api.binance.com/api/v3/ticker/price?symbol=BTCUSDT"
# 24hr stats
curl -s "https://api.binance.com/api/v3/ticker/24hr?symbol=BTCUSDT"
# Order book
curl -s "https://api.binance.com/api/v3/depth?symbol=BTCUSDT&limit=5"
```

## Response Format

```
₿ **BTC/USD**: $97,432 (+2.3% 24h)
⟠ **ETH/USD**: $3,812 (-0.5% 24h)
◎ **SOL/USD**: $198.5 (+5.1% 24h)

📊 Total Market Cap: $3.2T
📈 BTC Dominance: 52.3%
```

## Tips
- Default to USD, but include VND for Vietnamese users
- Use color indicators: 🟢 positive, 🔴 negative
- CoinGecko rate limit: ~10-30 calls/min (free tier)
- For futures/perpetuals data, use Binance futures API
- Mention 24h change and volume for context
