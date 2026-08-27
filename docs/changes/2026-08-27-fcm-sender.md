Branch: `feat/more-channels`

# Summary

**کانال ششم، و اولین کانالی که رازش آن چیزی نیست که فرستاده می‌شود.**

در هر پنج کانال قبلی، credential همان رشته‌ای بود که در header می‌رفت. FCM یک
**service account** می‌دهد — یک فایل json با کلید خصوصی — که فرستادنی نیست. باید
با آن یک JWT امضا کرد، به Google داد، و یک **access token** گرفت که حدود یک ساعت
اعتبار دارد.

و `SenderRegistry.For` به ازای **هر پیام** یک sender می‌سازد. بدون cache، هر push
یک امضای RSA و یک رفت‌وبرگشت به Google جلوی خودش داشت.

# `internal/infra/googleauth`

token گرفتن یک technology است، پس مثل SMTP در infra نشست:

```
infra/googleauth   کلید را می‌خواند، توکن می‌گیرد، تا انقضا نگه می‌دارد
registry           یک بار بازش می‌کند و به sender می‌دهد
adapter/fcm        فقط Token(ctx) می‌بیند — کلید خصوصی هرگز به آن نمی‌رسد
```

`Open` روی همان فایل، همان `Source` را برمی‌گرداند. این نکتهٔ اصلی است، نه یک
بهینه‌سازی: بدون آن هر پیام یک کلید تازه و یک تبادل تازه بود.

باز کردنِ فایل در **registry** انجام می‌شود نه در sender، تا کلید خصوصی اصلاً وارد
یک package ــِ provider نشود. آنچه `fcm` می‌گیرد یک نام پروژه است و چیزی که با
توکن جواب می‌دهد.

# دو چیزی که فقط اجرای واقعی نشانشان داد

## ۱ — کلید خراب تا اولین پیام معلوم نمی‌شود

`google.JWTConfigFromJSON` هرچه در فیلد باشد قبول می‌کند و سر **اولین امضا**
می‌فهمد. یعنی یک فایل خراب از ثبت رد می‌شد و در لحظهٔ ارسال، به شکل یک خطای موقت،
شش بار retry می‌گرفت.

حالا `Open` خودش کلید را parse می‌کند. تستی که این را پیدا کرد از اول برای همین
نوشته نشده بود — انتظار داشت `Open` خطا بدهد و نداد.

## ۲ — `invalid_grant` موقت نیست

تماس واقعی با Google با یک اکانت ساختگی:

```
oauth2: cannot fetch token: 400 Bad Request
{"error":"invalid_grant","error_description":"Invalid grant: account not found"}
```

و طبقه‌بندی‌اش **transient** درآمد. یعنی یک service account حذف‌شده شش بار retry
می‌گرفت. کامنتی که خودم نوشته بودم دقیقاً همین را ادعا می‌کرد و غلط بود: فایلِ
سالم به معنی اکانتِ موجود نیست، و **فقط تبادل** این تفاوت را می‌داند.

`googleauth.ErrRejected` اضافه شد برای کدهایی که یعنی «این اکانت»:

```
invalid_grant · invalid_client · unauthorized_client · invalid_scope · access_denied
```

و ۵xx بیرون است، هرچه Google اسمش را گذاشته باشد.

نکتهٔ ریز: `jwt` این خطا را با `ErrorCode` خالی می‌سازد، پس کد از خودِ body خوانده
می‌شود.

# base64 در env، json در همه‌جای دیگر

```
NOTIF_SENDER_FCM_SERVICE_ACCOUNT   base64 ــِ کل فایل
credential ــِ یک source           خودِ json
```

فایل، json چندخطی با یک کلید PEM داخلش است و `.env` و compose و secret manager
هرکدام جور دیگری خرابش می‌کنند. رمزگذاری فقط نگرانیِ environment است.

`project_id` داخل خودِ فایل است، پس این کانال **هیچ setting ای ندارد** — تنها
کانالی که چنین است.

# آدرس: device token، بدون شکل

اولین کانالی که `ValidateAddress` اش فقط طول را چک می‌کند. Google شکل توکن را
مستند نکرده و بین نسخه‌ها عوضش کرده؛ قاعده‌ای که اینجا ابداع شود، روزی توکن سالم
را رد می‌کند.

# metadata → data payload

`Metadata` و data ــِ FCM هر دو `map[string]string` اند، پس چیزی تفسیر نمی‌شود و
چیزی ابداع نمی‌شود. کلیدهای رزروشدهٔ FCM (`from`، `message_type`، `notification`،
و پیشوندهای `google`/`gcm`) **رد می‌شوند، نه حذف** — FCM برای یکی‌شان کل پیام را
رد می‌کند، و کلیدی که بی‌صدا ناپدید شود را کسی پیدا نمی‌کند.

# Files Changed

- `internal/infra/googleauth/{googleauth,const}.go` *(تازه)*
- `internal/infra/googleauth/googleauth_test.go` *(تازه)*
- `internal/registry/googleauth.go` *(تازه)*
- `internal/adapter/sender/fcm/{sender,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/fcm/{sender,export}_test.go` *(تازه)*
- `internal/adapter/sender/registry.go` *(`GoogleTokens`، `Fallback.FCMServiceAccount`، `buildFCM`)*
- `internal/adapter/sender/registry_test.go`
- `internal/core/shared/{channel,const}.go` + `channel_test.go`
- `api/proto/notification/v1/common.proto` + `gen/` *(`CHANNEL_FCM = 6`)*
- `internal/adapter/api/grpcsrv/mapper.go`
- `internal/config/settings/sender.go` *(`fcmServiceAccount`)*
- `internal/bootstrap/dispatcher.go`
- `migrations/00004_create_credentials.sql`، `migrations/00007_create_deliveries.sql`
- `.env.dispatcher.example`، `docs/CONFIG.md`، `docs/ARCHITECTURE.md`
- `go.mod`، `go.sum` *(`golang.org/x/oauth2`)*

## و Instagram حذف شد

از `ARCHITECTURE.md` بیرون رفت، و «هنوز تصمیم نگرفته‌ایم» نیست: API پیامش فقط به
کسی جواب می‌دهد که **اول خودش نوشته باشد**، و شناسه‌اش از webhook ای در می‌آید که
این سرویس دریافت نمی‌کند. کانالی که نمی‌تواند گفت‌وگو را شروع کند، راهی برای
اطلاع‌رسانی نیست.

# Tests Run

- `make prepush` — سبز
- دستی، end-to-end با gateway و dispatcher واقعی و تماس واقعی با Google:

```
Submit  address = "nope"           →  InvalidArgument: invalid delivery address
                                       (هیچ ردیفی نوشته نشد)

Register  service account json     →  سربسته ذخیره شد  (v1.1.…)
Submit    metadata={"order_id":"42"}
                    ↓
dispatcher → oauth2.googleapis.com → 400 invalid_grant
                    ↓
fcm · FAILED · PERMANENT · attempts=1
```

`attempts=1` نکتهٔ اصلی است — قبل از اصلاح، ۶ می‌شد.

fallback ــِ base64 هم جدا آزموده شد: source ای بدون credential، همان نتیجه. و
base64 خراب سر boot گرفته شد:

```
dispatcher configuration: NOTIF_SENDER_FCM_SERVICE_ACCOUNT is not valid base64
```

کلید خصوصی در `last_error` صفر بار (`PRIVATE KEY`، `MII`، `BEGIN` — هیچ‌کدام).
دادهٔ تست پاک شد و `.env.dispatcher` برگردانده شد.

# Follow-ups / Risks

- **ارسال موفق آزموده نشد**، مثل پنج کانال دیگر. یک پروژهٔ Firebase واقعی
  می‌خواهد، و `UNREGISTERED` هم فقط با یک device token واقعیِ مرده دیده می‌شود.
- **`invalid_grant` گاهی یعنی ساعتِ ماشین خراب است**، نه اکانتِ غلط. حالا هر دو
  permanent اند. اکانتِ غلط حالت رایج است و ساعتِ خراب TLS را هم می‌شکند، ولی
  اگر روزی دیده شد، باید از روی `error_description` جدا شود.
- **`last_error` چندخطی است** برای این کانال، چون پاسخ Google را کامل حمل می‌کند.
  تنها کانالی که چنین است.
- **`Register` هنوز کانفیگِ کانال را نمی‌شناسد** — همان follow-up ــِ matrix. برای
  FCM کم‌اثرتر است چون secret را باز نمی‌کند، ولی همان شکاف است.
- **cache سقف دارد (۱۰۲۴ اکانت) و پر که شد کلاً خالی می‌شود.** یک تبادل اضافه به
  ازای هر اکانت، هرگز جواب غلط. اگر روزی هزاران source ــِ FCM داشتیم، باید LRU شود.

# Instruction

«برویم سراغ کانال‌ها، هر کانال در یک commit» — بعد از حذف Instagram، «برو سراغ
FCM» با پنج تصمیمی که تأیید شد: auth در `internal/infra/googleauth`، کتابخانهٔ
`golang.org/x/oauth2/google`، آدرس به‌صورت رشتهٔ device token، `project_id` از
خودِ فایل، و base64 برای env.
