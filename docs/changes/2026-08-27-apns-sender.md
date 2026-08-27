Branch: `feat/more-channels`

# Summary

**کانال هفتم، و دومین کانالی که رازش فرستادنی نیست — این بار یک قدم جلوتر.**

FCM یک service account را با Google عوض می‌کند و توکن می‌گیرد. Apple **اصلاً
endpoint ای برای پرسیدن ندارد**: provider token یک JWT است که خودمان با کلید p8
امضا می‌کنیم و مستقیم ارائه می‌دهیم.

# چرا این هم یک resource است، نه یک تابع

هیچ I/O ای ندارد. ولی ساعتِ Apple دارد:

```
حداقل هر ۶۰ دقیقه   توکن باید نو شود
حداکثر هر ۲۰ دقیقه  زودتر از این، TooManyProviderTokenUpdates
```

و `SenderRegistry.For` به ازای هر پیام یک sender می‌سازد. پس آخرین توکن باید
جایی بماند — و همان‌جا `internal/infra/appleauth` است، قرینهٔ `googleauth`.
پنجره ۴۵ دقیقه است: بین دو قاعده، با فاصله از هر دو.

# چهار فیلد، و فقط یکی‌شان راز است

`ARCHITECTURE.md` نوشته بود که چهار فیلد «json داخل مقدارِ سربسته» می‌شوند. غلط
بود و اصلاح شد:

```
p8 key      راز       →  secret ــِ سربسته
key_id      نامِ فایل   →  config
team_id     نامِ حساب   →  config
topic       bundle id  →  config
```

سه‌تای آخر راز نیستند. مهر و موم کردنشان یعنی برای خواندنِ نامِ یک اپ باید کلید
رمزگشایی داشته باشی — و پنج کانال دیگر تنظیماتشان را همان‌جا نگه می‌دارند.

`environment` هم هست: `production` یا `sandbox`، با پیش‌فرض production. **دو
سرویس جدا با device token های جدا** اند، و توکنِ یکی برای دیگری ناشناس است — که
جوابش `BadDeviceToken` می‌شود و به نظر می‌رسد مشکل از دستگاه است، در حالی که
آدرسِ سرویس اشتباه بوده.

# JWT بدون کتابخانه

فقط یک جهت لازم است: می‌سازیم و هرگز نمی‌خوانیم. یک الگوریتم، دو ادعا. کتابخانهٔ
jwt عمدتاً همان parse و validate ای است که یک verifier می‌خواهد — و `pkg/crypto`
هم به همین دلیل دست‌نویس است.

نکتهٔ ریز که تست می‌گیردش: امضای ES256، **r و s به‌صورت دو نیمهٔ ثابتِ ۳۲ بایتی**
است، نه جفتِ ASN.1 که Go پیش‌فرض برمی‌گرداند. تست با `ecdsa.Verify` و کلید عمومیِ
همان فایل بررسی می‌کند.

# `ExpiredProviderToken` بدون یک کار اضافه، شش بار retry می‌گرفت

توکن ما کهنه می‌شود → Apple رد می‌کند → دوباره تلاش می‌کنیم → cache **همان توکنِ
کهنه** را پس می‌دهد → تا تمام شدن attempts.

پس `Source.Expire()` اضافه شد: نگه‌داشته را دور می‌اندازد تا تلاش بعدی تازه امضا
کند. و `TooManyProviderTokenUpdates` عمداً این کار را **نمی‌کند** — امضای دوباره
همان چیزی است که مسئله را ساخت. دو خطای موقت با دو رفتار متضاد.

# آدرس: hex، و این بار واقعاً چک می‌شود

```
fcm    device token در body ــِ json است   →  فقط طول
apns   device token در path ــِ url است    →  hex، و طول
```

همین یک تفاوت، دلیلِ کلِ قاعده است.

# apns-id: همان مقدار، در شکلی که Apple می‌خواهد

`DeliveryID` و UUID هر دو ۱۲۸ بیت اند، پس ULID بازنویسی می‌شود نه نگاشت.

**این tekrarزدایی نیست و APNs چنین چیزی ندارد** — پیامِ دوباره‌تحویل‌شده دوباره
push می‌شود. آنچه می‌خرد این است که «آیا این رسید؟» یک شناسه دارد نه دو تا: بدون
header، Apple خودش یکی می‌سازد و آن‌وقت ردیفِ ما و چیزی که Apple می‌تواند درباره‌اش
بگوید اصلاً به هم وصل نمی‌شوند.

# metadata کنارِ `aps` می‌نشیند، نه داخلش

Apple پیام را زیر `aps` می‌گذارد و کلیدهای دلخواه در **ریشه** می‌نشینند — برخلاف
FCM که `data` ــِ جدا دارد. کلید `aps` رد می‌شود نه حذف، مثل کلیدهای رزروشدهٔ FCM.

# Files Changed

- `internal/infra/appleauth/{appleauth,const}.go` *(تازه)*
- `internal/infra/appleauth/appleauth_test.go` *(تازه)*
- `internal/registry/appleauth.go` *(تازه)*
- `internal/adapter/sender/apns/{sender,config,api,errors,id,const}.go` *(تازه)*
- `internal/adapter/sender/apns/{sender,export}_test.go` *(تازه)*
- `internal/adapter/sender/registry.go` *(`AppleTokens`، `Fallback.APNs`، `buildAPNs`)*
- `internal/adapter/sender/registry_test.go`
- `internal/core/shared/{channel,const}.go` + `channel_test.go` *(`isHex`)*
- `api/proto/notification/v1/common.proto` + `gen/` *(`CHANNEL_APNS = 7`)*
- `internal/adapter/api/grpcsrv/mapper.go`
- `internal/config/settings/sender.go` *(`APNs`؛ `fcmServiceAccount` به `decodeKey` تعمیم یافت)*
- `internal/bootstrap/dispatcher.go`
- `migrations/00004_create_credentials.sql`، `migrations/00007_create_deliveries.sql`
- `.env.dispatcher.example`، `docs/CONFIG.md`، `docs/ARCHITECTURE.md`

# Tests Run

- `make prepush` — سبز
- دستی، end-to-end با gateway و dispatcher واقعی و تماس واقعی با `api.push.apple.com`:

```
Submit  address = 64 حرفِ غیر-hex     →  InvalidArgument: invalid delivery address
                                          (هیچ ردیفی نوشته نشد)

Register  p8 + key_id/team_id/topic  →  سربسته ذخیره شد
Submit                               →  apns · FAILED · PERMANENT · attempts=1
                                          InvalidProviderToken
```

fallback ــِ base64 روی **sandbox** جدا آزموده شد، و کلید RSA به‌جای p8 — که
اشتباهِ رایج است، چون فایل FCM هم PEM است:

```
NO_SENDER · attempts=1
apns signing key is not usable (appleauth: the signing key is not an ecdsa key)
```

قبل از هر تماسی، و به‌عنوان جوابِ کانفیگ. base64 خراب هم سر boot گرفته شد:

```
dispatcher configuration: NOTIF_SENDER_APNS_KEY is not valid base64
```

کلید در `last_error` صفر بار. دادهٔ تست پاک شد و `.env.dispatcher` برگردانده شد.

# Follow-ups / Risks

- **امضا زنده ثابت نشد.** `InvalidProviderToken` همان جوابی است که یک JWT خرابِ
  ساختاری هم می‌گیرد، پس تماس واقعی transport و طبقه‌بندی را ثابت کرد نه امضا را.
  امضا با `ecdsa.Verify` در تست بررسی می‌شود؛ بیشتر از این، یک حساب Apple واقعی
  می‌خواهد.
- **ارسال موفق آزموده نشد**، مثل شش کانال دیگر. `Unregistered` هم فقط با یک
  device token واقعیِ مرده دیده می‌شود.
- **`environment` پیش‌فرضِ production دارد.** اشتباهش به‌شکل `BadDeviceToken`
  درمی‌آید که `NOT_REACHABLE` است — یعنی source فکر می‌کند دستگاه مشکل دارد. متن
  خطا reason را حمل می‌کند، ولی تشخیصش سخت است.
- **فقط `alert`.** نه background، نه voip، نه `apns-collapse-id`، نه
  `apns-expiration`. آخری قابل توجه است: پیام تا زمانی که دستگاه روشن شود در صف
  Apple می‌ماند، و ما نمی‌گوییم تا کی ارزش دارد.
- **HTTP/2 اجباری است و مسلّم گرفته شده.** `ForceAttemptHTTP2` روشن است، ولی اگر
  روزی transport عوض شود، APNs بی‌صدا از کار می‌افتد و هیچ تستی نمی‌گیردش.
- **cache سقف دارد (۱۰۲۴ identity)**، مثل googleauth و با همان معامله.

# Instruction

«برو سراغ APNs» — با تصمیم‌هایی که در حین کار گرفته شد: امضای JWT دست‌نویس
به‌جای کتابخانه (چون فقط یک جهت لازم است، مثل `pkg/crypto`)، سه فیلدِ غیرِراز در
`config` به‌جای داخلِ مقدارِ سربسته، `environment` جزو تنظیمات با پیش‌فرض
production، و `apns-id` از روی `DeliveryID` برای هم‌ترازیِ شناسه‌ها.
