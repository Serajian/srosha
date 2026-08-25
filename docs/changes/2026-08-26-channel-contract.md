Branch: `feat/channel-contract`

# Summary

هفت کانال دیگر خواسته شد: SMS، FCM، APNs، Web Push، RCS، WhatsApp، Instagram و
Matrix. **هیچ‌کدام ساخته نشد** — و این تصمیم است، نه تعویق.

آنچه شد، پایین‌آوردن هزینهٔ ساختنشان بود:

```
✅ Message قابل گسترش شد
✅ metadata رسمی شد
✅ channel contract مستند شد
✅ FailureNotReachable اضافه شد
✅ تفاوت اعتبارنامه‌ها حفظ شد

❌ هیچ implementation ای برای WhatsApp، Instagram یا Web Push
❌ هیچ abstraction ای برای بیست کانال خیالی
```

# Instagram گروه سومی را آشکار کرد

مدل تا امروز این بود:

```
source  →  به این آدرس بفرست
```

WhatsApp و Instagram این‌اند:

```
source  →  در پنجره‌ای که گیرنده باز کرده، جواب بده
```

و Instagram سخت‌گیرتر است: به کسی که اول به تو پیام نداده، **اصلاً** نمی‌شود
پیام داد.

## ولی این پنجره را مدل نکردیم، و نباید می‌کردیم

دانستنِ پنجره یعنی گرفتن webhook از Meta و نگه‌داشتن وضعیت گفت‌وگو — یک **مسیر
ورودی کامل** که این سرویس ندارد، برای سرویسی که کارش فرستادن است.

پس provider مرجع است، و جواب یک کلمه است نه یک زیرسیستم:

```go
FailureExpired · FailureMaxAttempts · FailurePermanent · FailureNoSender
                                                       + FailureNotReachable
```

قبلش این شکست‌ها `PERMANENT` می‌شدند و متن provider در `last_error` می‌ماند — که
برای اپراتور کافی است و **به مشتری هیچ نمی‌گوید**، چون `Reason` چیزی است که او
می‌بیند.

# `SendError` یک bool دوم نگرفت

اینجا قضاوت کردم و می‌گویمش:

```go
SendError{ Permanent bool }                     امروز
SendError{ Permanent, NotReachable bool }       ← دو bool برای سه حالت
SendError{ Kind SendKind }                      ← سه ثابت، و چهارمی یک خط است
```

سه‌حالتی که با پرچم مدل شود، حالت چهارم را گران می‌کند: هر صداکننده یک شاخهٔ تازه
و هر sender یک فیلد تازه. abstraction خیالی نیست — **امروز دو حالت داشتیم و
داشتیم سومی را اضافه می‌کردیم.**

```
SendTransient     صفرِ نوع، و عمداً: شکست طبقه‌بندی‌نشده بیشتر یک تپق است تا بن‌بست
SendPermanent     پیام را رد کرد
SendUnreachable   گیرنده را رد کرد
```

`IsPermanentSend` برای هر دو حالت پایانی true می‌ماند، پس هیچ صداکنندهٔ موجودی
رفتار عوض نکرد.

## و چه چیزی واقعاً `NOT_REACHABLE` تولید می‌کند

فقط یک چیز، و عمداً: **۴۰۳ در telegram و bale** — یعنی bot بلاک شده یا به آن چت
دسترسی ندارد. بقیه دست‌نخورده ماندند.

طبقه‌بندی دوبارهٔ SMTP وسوسه‌انگیز بود — یک `550 no such user` هم گیرنده است نه
پیام — ولی تشخیصش یعنی خواندنِ انگلیسیِ سرور، که همان کاری است که گفتیم نکنیم.

# `metadata` بالاخره به sender می‌رسد

از روز اول در جدول بود، نوشته می‌شد، خوانده می‌شد — و در `deliver()` دور ریخته
می‌شد:

```go
shared.Message{ Recipient, Title, Body }     ← n.Metadata() اینجا نبود
```

حالا هست، و کامنتش می‌گوید این **seam** است نه feature:

> srosha آن را نمی‌خواند و نخواهد خواند. کلید-مقدارهای خودِ source است. یک
> adapter می‌تواند در آن دنبال چیزی بگردد که API خودش لازم دارد — کدام template،
> کدام tag — و هیچ provider دیگری تحت تأثیر نیست، چون هیچ‌جا تعریف نشده که
> کلیدها یعنی چه.

این همان راهی است که کانالی که بیشتر از یک عنوان و یک متن می‌خواهد، آن را
می‌گیرد — بدون اینکه نیازِ هر کانال یک فیلد روی `Message` شود.

**هیچ migration ای لازم نداشت**: `metadata JSONB` از `00006` آنجا بود. تنها
تغییر schema برای `NOT_REACHABLE` بود، چون `failure_reason` یک `CHECK` دارد — و
طبق تصمیم قبلی داخل `00007` نشست، نه یک فایل تازه.

# Files Changed

- `internal/core/shared/senderror.go` *(`SendKind`، `SendKindOf`)*
- `internal/core/shared/message.go` *(`Metadata`)*
- `internal/core/domain/delivery/status.go` *(`FailureNotReachable`)*
- `internal/core/usecase/dispatch.go` *(نوع را حمل می‌کند، و metadata را پاس می‌دهد)*
- `internal/adapter/sender/{telegram,bale}/errors.go` *(۴۰۳ جدا شد)*
- `internal/adapter/sender/email/errors.go` *(روی نوع تازه)*
- `api/proto/notification/v1/common.proto` *(`FAILURE_REASON_NOT_REACHABLE`)* + `gen/`
- `internal/adapter/api/grpcsrv/mapper.go`
- `migrations/00007_create_deliveries.sql` *(`CHECK` گسترش یافت)*
- `docs/ARCHITECTURE.md` *(بخش «What a channel is, and what adding one costs»)*
- تست‌ها در `usecase`، `telegram`، `bale`

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، روی gateway و dispatcher واقعی:

```
Submit با metadata            →  postgres: {"order":"42","template":"order_shipped"}
dispatcher                    →  توکن جعلی → ۴۰۱ → PERMANENT
                                 (که درست است: توکن بد کانفیگ ماست، نه گیرنده)
                                     ↓
NOT_REACHABLE در ردیف          →  CHECK قبولش کرد
Get                           →  FAILURE_REASON_NOT_REACHABLE
                              →  metadata سالم برگشت
```

# Follow-ups / Risks

- **`NOT_REACHABLE` زنده تولید نشد**، فقط در تست. برای دیدن ۴۰۳ واقعی یک توکن
  معتبر و چتی که bot را بلاک کرده لازم است. مسیرش از ردیف تا خروجی gRPC دستی
  آزموده شد.
- **SMTP طبقه‌بندی نشد.** یک `550 no such user` هم گیرنده است، ولی جدا کردنش از
  «پیام را رد کرد» یعنی خواندنِ متن انگلیسی سرور.
- **`metadata` سقف ندارد.** حالا به provider می‌رسد، پس یک source می‌تواند چیز
  بزرگی در آن بگذارد و ما حملش کنیم.
- **هیچ‌کدام از هفت کانال ساخته نشد.** درزها آماده‌اند؛ کار نشده است.

# Instruction

«همه را به فهرست کانال‌ها اضافه کن» و بعد گزارش تغییرات لازم — و از دو راه، **ب**:
مرزهای domain طوری طراحی شوند که تغییر آینده local بماند، بدون نوشتن
implementation از الان. پنج کارِ مشخص، و چهار چیزی که عمداً نشد.
