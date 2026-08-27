Branch: `feat/more-channels`

# Summary

**کانال پنجم، و اولین کانالی که آدرسش یک «جا» است نه یک «کس».**

Matrix فدرال است: هیچ `api.telegram.org` ای ندارد که برای همه درست باشد. و
مفهوم «به این شخص بفرست» ندارد — فقط «در این اتاق بنویس». هر دو تفاوت، دو جای
کوچک ولی واقعی در سرویس عوض کردند.

# `Message.DeliveryID` — تنها فیلد تازهٔ core

Matrix برای هر ارسال یک `txnId` می‌خواهد و همان را برای تکرارزدایی به کار
می‌برد: تراکنشی که قبلاً دیده، دوباره event نمی‌سازد.

```
id تصادفی بسازیم   →  هر retry یک پیام تازه در اتاق
delivery id        →  retry همان تراکنش است، اتاق تکراری نمی‌بیند
```

و srosha از قبل دقیقاً همین مقدار را دارد: یک delivery یعنی یک پیام به یک
گیرنده، و id اش در تمام تلاش‌ها ثابت است. پس فیلد اضافه شد، نه ساخته شد.
sender هایی که کاری با آن ندارند نادیده‌اش می‌گیرند.

# آدرس: فقط اتاق

```
!abc:matrix.org    ✓
@someone:matrix.org  ✗  invalid delivery address
```

user id پذیرفتنی به نظر می‌رسد و بعد **هر بار** ارسال شکست می‌خورد. رسیدن به یک
شخص یعنی پیدا کردن یا ساختن اتاق خصوصی با او — یعنی نگه‌داشتن وضعیت گفت‌وگو، که
`ARCHITECTURE.md` صریحاً ردش کرده. پس همان‌جا رد می‌شود که هنوز می‌شود اسمش را
«اشتباه کاربر» گذاشت: در `Submit`، قبل از هر نوشتنی.

# اولین کانالی که host اش مالِ source است

بقیه یک ثابت دارند. اینجا `homeserver` بخشی از credential است، و همین یک
`validate` تازه لازم داشت که هیچ کانال دیگری نداشت:

```
https اجباری        توکن روی http یعنی توکن لخت
بدون credential     user:pass در url جایی نمی‌رود جز log
بدون path           هرچه بعد از host بیاید، path ما را می‌شکند
```

بررسی یک بار در ساخت sender انجام می‌شود، نه در هر پیام.

# طبقه‌بندی: errcode قاعده است، status استثنا

برخلاف WhatsApp، Matrix کدهایش را جدی مستند کرده و همان‌ها را برمی‌گرداند:

```
M_FORBIDDEN · M_NOT_FOUND       →  NOT_REACHABLE   اتاق ما را راه نمی‌دهد
M_UNKNOWN_TOKEN · M_MISSING_TOKEN →  PERMANENT     کانفیگ خودمان است
M_LIMIT_EXCEEDED · 429          →  transient       با retry_after_ms
5xx                              →  transient
4xx دیگر                        →  permanent
```

# Files Changed

- `internal/adapter/sender/matrix/{sender,config,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/matrix/{sender,export}_test.go` *(تازه)*
- `internal/core/shared/message.go` *(`Message.DeliveryID`)*
- `internal/core/usecase/dispatch.go` *(پر کردنش از `del.ID`)*
- `internal/core/shared/{channel,const}.go` *(`ChannelMatrix`، `isMatrixRoom`)*
- `internal/core/shared/channel_test.go`
- `api/proto/notification/v1/common.proto` + `gen/` *(`CHANNEL_MATRIX = 5`)*
- `internal/adapter/api/grpcsrv/mapper.go` *(هر دو جهت)*
- `internal/adapter/sender/registry.go` *(`Fallback.Matrix`، دو شاخه)*
- `internal/adapter/sender/registry_test.go`
- `internal/config/settings/sender.go` *(`Matrix{Token, Homeserver}`)*
- `internal/bootstrap/dispatcher.go`
- `migrations/00004_create_credentials.sql`، `migrations/00007_create_deliveries.sql` *(هر دو CHECK)*
- `.env.dispatcher.example`، `docs/CONFIG.md`

# Tests Run

- `make prepush` — سبز
- دستی، با تماس واقعی به `matrix.org`:

```
Submit  @someone:matrix.org  →  InvalidArgument: invalid delivery address
                                (هیچ ردیفی نوشته نشد)

Register  homeserver = http://matrix.org   →  پذیرفته شد
Submit                                     →  NO_SENDER
                                               matrix homeserver must use https
                                               (هیچ درخواستی نرفت)

Credential.Update  →  https://matrix.org
Submit             →  FAILED · PERMANENT · attempts=1
                      M_UNKNOWN_TOKEN: Token is not active
```

دادهٔ تست پاک شد و باینری‌های موقت حذف شدند.

# Follow-ups / Risks

- **`Register` کانفیگِ کانال را نمی‌شناسد.** فقط `json.Valid` می‌کند، پس
  `homeserver` ــِ http پذیرفته می‌شود و source دیر — با یک پیامِ `NO_SENDER` —
  می‌فهمد. برای email هم همین بوده؛ حالا که host از source می‌آید بیشتر به چشم
  می‌آید. راه‌حلش یک اعتبارسنجی per-channel روی مسیر ثبت است، که کار خودش است.
- **ارسال موفق آزموده نشد**، مثل چهار کانال دیگر. یک اکانت واقعی می‌خواهد.
- **join نمی‌کنیم.** اگر اکانت srosha عضو اتاق نباشد، `M_FORBIDDEN` و
  `NOT_REACHABLE`. عضو کردن کارِ صاحب اتاق است، نه ما.
- **فقط `m.text`.** نه فرمت، نه پیوست، نه رمزنگاری سرتاسری — اتاق رمزشده پیام
  رمزنشده می‌گیرد و کسی نمی‌تواند بخواندش.

# Instruction

«برویم سراغ کانال‌ها، یک برنچ بساز، هر کانال در یک commit» — و «matrix را بزن»
به‌عنوان اولی.
