---
name: stock-vn
description: "Tra cứu giá chứng khoán Việt Nam. Hỗ trợ HOSE, HNX, UPCOM. Giá realtime, lịch sử, phân tích cơ bản."
metadata: {"nanobot":{"emoji":"📈","requires":{"bins":["curl"]}}}
---

# Stock VN — Chứng khoán Việt Nam

Tra cứu giá chứng khoán Việt Nam sử dụng các API miễn phí.

## SSI iBoard API (primary, realtime, no key)

### Giá hiện tại của một mã
```bash
curl -s "https://iboard-query.ssi.com.vn/stock/type/s/market/HOSE"  -H "Accept: application/json" | python3 -c "
import json,sys
data = json.load(sys.stdin)
for s in data.get('data',[]):
    if s.get('ss','') == 'FPT':
        print(json.dumps(s, indent=2))
        break
" 2>/dev/null
```

### Top thay đổi giá
Dùng `web_fetch` để lấy dữ liệu từ các trang:
```
web_fetch(url="https://banggia.cafef.vn/stockhandler.ashx?center=1")
```

## TCBS API (free, detailed)

### Giá và thông tin cơ bản
```bash
curl -s "https://apipubaws.tcbs.com.vn/stock-insight/v2/stock/bars-long-term?ticker=FPT&type=stock&resolution=D&countBack=20"
```

### Thông tin công ty
```bash
curl -s "https://apipubaws.tcbs.com.vn/tcanalysis/v1/ticker/FPT/overview"
```

### Tài chính doanh nghiệp
```bash
curl -s "https://apipubaws.tcbs.com.vn/tcanalysis/v1/finance/FPT/incomestatement?yearly=1&isAll=false"
```

### Cổ đông lớn
```bash
curl -s "https://apipubaws.tcbs.com.vn/tcanalysis/v1/ticker/FPT/large-share-holder"
```

## VNDirect API

### Giá hiện tại
```bash
curl -s "https://finfo-api.vndirect.com.vn/v4/stock_prices?sort=date&q=code:FPT~date:gte:2026-02-01&size=20&page=1"
```

### Thông tin sàn
```bash
curl -s "https://finfo-api.vndirect.com.vn/v4/stocks?q=code:FPT"
```

## Wichart / CafeF (via web_fetch)
```
web_fetch(url="https://cafef.vn/thi-truong-chung-khoan.chn")
```

## Các mã phổ biến

| Nhóm | Mã tiêu biểu |
|------|---------------|
| Ngân hàng | VCB, BID, CTG, TCB, MBB, ACB, VPB, HDB |
| Bất động sản | VHM, VRE, NVL, KDH, DXG |
| Công nghệ | FPT, CMG |
| Thép | HPG, HSG, NKG |
| Chứng khoán | SSI, VCI, HCM, VND |
| Retail | MWG, PNJ, DGW |
| Dầu khí | GAS, PLX, PVD, PVS |
| VN-Index | VNINDEX |

## Response Format

```
📈 **FPT** — FPT Corporation
💰 Giá: 134,500 VND (+2.3% | +3,000)
📊 KL: 5.2M | GT: 698 tỷ
📉 Thấp/Cao ngày: 131,000 - 135,200
📅 52w: 95,000 - 142,000

💡 P/E: 22.5 | P/B: 4.8 | ROE: 21.3%
🏢 Vốn hóa: 175,890 tỷ VND
```

## Tips
- Thị trường VN mở: 9:00-11:30, 13:00-14:45 (GMT+7)
- Ngoài giờ: hiển thị giá đóng cửa gần nhất
- TCBS API ổn định và có nhiều data nhất
- Luôn show giá bằng VND, format: 134,500
- Nêu rõ nguồn dữ liệu khi trả kết quả
