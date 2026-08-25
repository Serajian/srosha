Branch: `feat/webhook-notifier`

# Summary

**آخرین stub پر شد.** حالا وقتی همهٔ delivery های یک پیام تمام شوند، نتیجه به
آدرسی که source ثبت کرده POST می‌شود — امضاشده، تا بتواند بفهمد از ماست.

```
usecase.announce
      ↓
notifier.Notify(webhook, batch)
      ↓
   sign(secret, now, body)
      ↓
   POST  X-Srosha-Signature: v1=<hex>
         X-Srosha-Timestamp: <unix>
```

## چرا timestamp امضا می‌شود، نه اینکه فقط فرستاده شود

این تنها تصمیم واقعی این commit است:

```
امضا فقط روی body        →  تا ابد معتبر است
                            هرکس یک‌بار یک callback دید، هر وقت خواست replay می‌کند
                            و درست verify می‌شود

امضا روی <ts>.<body>     →  گیرنده کهنه‌اش را رد می‌کند
                            replay باید هر دو را جعل کند
```

و آن `.` تزئینی نیست. بدون جداکننده، `ts=1` با `body="23…"` همان بایت‌ها را
امضا می‌کند که `ts=12` با `body="3…"` — دو پیام متفاوت، یک امضا.

`v1=` هم برای روزی است که الگوریتم عوض شود: گیرنده می‌تواند مدتی هر دو را قبول
کند، وگرنه هر source باید در همان دقیقه‌ای که ما عوض می‌کنیم عوض کند.

## هیچ‌چیز بدون امضا نمی‌رود

اگر برای یک source رازی در `NOTIF_WEBHOOK_SECRETS` نباشد، callback **فرستاده
نمی‌شود**:

```
بفرستیم بدون امضا  →  کار می‌کند  ←  و مشکل همین است
                      گیرنده چیزی برای بررسی ندارد
                      و آن رازِ گم‌شده روزی معلوم می‌شود که کسی
                      شروع کند به بررسی‌کردن، نه روزی که deploy شد
```

پس خطا برمی‌گرداند و لاگ می‌شود. هزینه‌اش این است که آن source به سقف
`MAX_FAILURES` می‌رسد و callback اش خاموش می‌شود — که بلندتر است از سکوت.

## SSRF: نگهبان‌ها اینجا نیستند، و نباید باشند

`gosec` روی `client.Do` علامت زد و **حق داشت**: آدرس را خودِ source انتخاب
کرده. ولی جوابش یک چک در این فایل نیست:

```
ثبت‌نام      webhook.validateURL   https، بدون credential، host دارد
dial         DenyPrivateAddresses  اگر روی شبکهٔ خصوصی resolve شد، وصل نمی‌شود
redirect     FollowRedirects=false راه دیگرِ دور زدنِ همان چک
```

لایهٔ دوم مهم‌ترین است و **فقط در لحظهٔ dial** می‌تواند درست باشد: نامی که موقع
ثبت به یک آدرس عمومی resolve می‌شد، فردا می‌تواند به `10.0.0.1` resolve شود.

## body قرارداد است، و مالِ domain

`webhook.Batch` تگ‌های json خودش را دارد و این adapter هیچ‌کدام را نمی‌سازد.
کامنت خودش می‌گوید چرا: *«یک adapter تازه نباید بتواند اسمشان را عوض کند و هر
client را بشکند.»*

## جزئیات کوچک‌تر

- **body خوانده و دور ریخته می‌شود، با سقف.** چیزی از آن استفاده نمی‌شود — فقط
  status مهم است — ولی بدون خالی‌کردنش connection قابل استفادهٔ دوباره نیست، و
  آن سر سیم مالِ ما نیست: endpoint ای که بی‌نهایت body بدهد وگرنه تا timeout
  حافظه می‌گیرد.
- **راز per-source از `settings.Webhook.SecretFor` می‌آید، نه یک map.** چیزی که
  کل مجموعه را نگه دارد، چیزی است که می‌شود هر کدامش را از آن خواست.
- **۲xx یعنی رسید.** ۳xx نه — redirect خاموش است، پس یک ۳۰۱ یعنی آدرس عوض شده
  و source باید دوباره ثبتش کند.

# Files Changed

- `internal/adapter/notifier/{notifier,signature,const}.go` *(تازه)*
- `internal/adapter/notifier/notifier_test.go` *(تازه — ۱۵ تست)*
- `internal/config/settings/webhook.go` *(`SecretFor`)*

# Tests Run

- `make prepush` — سبز
- `golangci-lint run ./internal/... ./pkg/...` — صفر ایراد

تست‌ها همان کاری را می‌کنند که یک گیرندهٔ واقعی باید بکند — HMAC را خودشان از
نو می‌سازند و مقایسه می‌کنند — پس اگر روزی شکل امضا عوض شود، تست می‌شکند نه
مشتری:

```
ASourceCanVerifyWhatItGot            امضا از نو ساخته و برابر است
TheTimestampIsPartOfWhatIsSigned     همان body با ts دیگر → امضای دیگر
OneSourcesSecretDoesNotSignForAnother
NothingIsSentUnsigned                بدون راز، هیچ درخواستی نمی‌رود
WhatCountsAsDelivered                ۸ status
```

# Follow-ups / Risks

- **هنوز هیچ‌کس صدایش نمی‌زند.** `dispatcher.go` سیم‌کشی نشده. ولی این آخرین
  stub بود: consumer، senderها و notifier هر سه آماده‌اند.
- **افزودن یک source یعنی redeploy**، چون رازها از env می‌آیند. همان چیزی که
  `CONFIG.md` از قبل نوشته بود.
- شکل امضا هنوز جایی که مشتری بخواند مستند نشده. صفحهٔ API لازم دارد.

# Instruction

«ادامه بده» بعد از senderها — یعنی notifier، با همان شکل امضایی که پیشنهاد شد:
`HMAC-SHA256` روی `<timestamp>.<body>`، در `X-Srosha-Signature: v1=<hex>` و
`X-Srosha-Timestamp`.
