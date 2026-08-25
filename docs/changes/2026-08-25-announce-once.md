Branch: `feat/announce-once`

# Summary

ستون `notified_at` از روز اول در جدول بود و هیچ‌کس پرش نمی‌کرد. کار **پر کردنش**
نبود — یک مسابقهٔ باز بود که هیچ‌کس ندیده بود.

# باگ

`announce` وقتی اجرا می‌شود که **آخرین** delivery یک پیام settle شود:

```go
ds := ListAllForNotification(n.ID)
for _, d := range ds { if !d.IsSettled() { return } }   // هنوز تمام نشده
notifier.Notify(w, batch)
```

دو worker که آخرین **دو** delivery را هم‌زمان settle می‌کنند، هر دو یک پیامِ
تمام‌شده می‌بینند:

```
worker A   RecordSent(d1)  →  همه settled  →  Notify
worker B   RecordSent(d2)  →  همه settled  →  Notify
                                                ↓
                                source همان batch را دو بار می‌گیرد
```

و این با چیزی که خودِ proto وعده داده تناقض دارد: *«یک بار فرستاده می‌شود و هرگز
retry نمی‌شود.»* با claim شدنِ recovery محتمل‌تر هم شده بود — حالا دو مسیر
می‌توانند یک delivery را settle کنند.

## و statement از قبل برای همین نوشته شده بود

```sql
WHERE id = @id AND notified_at IS NULL     ← این شرط تصادفی نیست
```

`IS NULL` یعنی «فقط اولی برنده است». کسی این را برای یک ستون گزارشی نمی‌نویسد.

# راه‌حل: ادعا، نه ثبت

```sql
-- name: ClaimNotificationAnnouncement :execrows
UPDATE deliveries SET notified_at = @notified_at
WHERE notification_id = @notification_id AND notified_at IS NULL;
```

**روی کل پیام، نه یک ردیف.** دو UPDATE هم‌زمان سریال می‌شوند: اولی همهٔ ردیف‌ها را
مهر می‌زند و تعدادش را می‌گوید، دومی در برابر آنچه اولی commit کرده دوباره ارزیابی
می‌شود و صفر می‌گیرد.

```
شمارش > 0   →  مالِ من است، می‌فرستم
شمارش = 0   →  یکی دیگر دارد خبر می‌دهد
```

نسخهٔ per-row این خاصیت را **ندارد**: دو caller می‌توانستند هر کدام زیرمجموعه‌ای
ببرند و هر دو فکر کنند برنده‌اند.

## و ادعا قبل از فرستادن است، نه ثبت بعد از آن

دلیلش دقیقاً همان چیزی است که callback را best-effort می‌کند: **هیچ‌چیز retry اش
نمی‌کند.** پس تنها چیزی که باید درست باشد این است که یک بار برود.

هزینه‌اش یک تغییر معناست، و در خودِ statement نوشته شد:

```
notified_at  =  «اعلامی برای این نتیجه انجام شد»
             ≠  «source دریافتش کرد»
```

که خواندنِ صادقانه‌تر است: تلاش، خودِ رویداد است — و اینکه رسید یا نه جای دیگری
ثبت می‌شود، در `consecutive_failures` خودِ webhook.

# `MarkNotified` حذف شد

روی entity بود و حالا واقعاً مرده: ادعا در سطح مجموعه و در SQL انجام می‌شود، پس
هیچ مسیری یک delivery را تکی مهر نمی‌زند. `NotifiedAt()` ماند — خواندن هنوز
معنی دارد — با کامنتی که می‌گوید چرا setter ی کنارش نیست.

# Files Changed

- `internal/adapter/db/postgres/queries/delivery.sql` *(`MarkDeliveryNotified` → `ClaimNotificationAnnouncement`)* + `gen/`
- `internal/adapter/db/postgres/delivery.go` *(`ClaimAnnouncement`)*
- `internal/adapter/db/postgres/delivery_test.go` *(۲ تست، یکی همزمانی واقعی)*
- `internal/core/domain/delivery/{port,tracker}.go`
- `internal/core/domain/delivery/entity.go` *(`MarkNotified` حذف، `NotifiedAt` توضیح گرفت)*
- `internal/core/domain/delivery/entity_test.go`
- `internal/core/usecase/dispatch.go` *(`announce` اول ادعا می‌کند)*
- `internal/core/usecase/{dispatch,fakes}_test.go`

هیچ migration ای لازم نشد: ستون از روز اول بود.

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

```
OnlyOneCallerTakesTheAnnouncement   ۴ caller هم‌زمان  →  دقیقاً یکی برنده
TheWholeMessageIsStamped            هر دو delivery مهر خوردند، نه فقط آخری
TheSourceIsToldOnce                 دو delivery پشت سر هم  →  یک callback
                                    و رویداد تکراری  →  همچنان یکی
```

# Follow-ups / Risks

- **اگر callback شکست بخورد، `notified_at` مهر خورده و پیام نرفته.** عمدی و
  مستند، ولی یعنی این ستون به‌تنهایی جواب «آیا source خبر دارد» نیست — باید کنار
  `consecutive_failures` خوانده شود.
- کرش بین ادعا و ارسال، آن اعلام را برای همیشه از دست می‌دهد. همان‌طور که قبلاً
  هم می‌داد: callback هرگز retry نمی‌شود و API پرسش‌وپاسخ راه مطمئن است.

# Instruction

«۵ را انجام بده» — یعنی `notified_at` که هیچ‌وقت پر نمی‌شد. با انتخاب **ب**:
ادعا قبل از فرستادن، نه ثبت بعد از آن.
