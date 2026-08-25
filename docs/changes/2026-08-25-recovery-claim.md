Branch: `feat/recovery-claim`

# Summary

**dispatcher دوم حالا ممکن است.** تا امروز فقط یکی می‌توانست اجرا شود، و خودِ SQL
این را نوشته بود: *«THIS DOES NOT CLAIM THE ROWS.»*

# مسئله

recovery **مستقیم می‌فرستد**، دوباره publish نمی‌کند — که تصمیم درستی است و
`ARCHITECTURE.md` دلیلش را دارد. ولی نتیجه‌اش این است که پنجرهٔ تکراری broker
هرگز این ردیف‌ها را نمی‌بیند:

```
مسیر عادی    gateway → nats → consumer → send        ← dedup اینجاست
recovery     scheduler → DB → send                   ← از broker رد نمی‌شود
```

پس دو sweep هم‌زمان همان ردیف‌ها را می‌خواندند و هر دو می‌فرستادند.

# دو راه‌حلی که غلط بودند

هر دو از قبل در کامنت همان query امتحان و رد شده بودند:

```
FOR UPDATE SKIP LOCKED تنها      قفل فقط داخل transaction زنده است، و آن
                                 transaction باید در تمام مدت ارسال باز بماند
                                 — یک قفل ردیف و یک connection، برای ثانیه‌ها

علامت‌زدن با updated_at           سنِ ردیف خودش شمارندهٔ تلاش است
                                 جلو بردنش یعنی ردیف هرگز به GIVE_UP نمی‌رسد
```

# راه‌حل: `claimed_at` + اجاره، در یک statement

```sql
UPDATE deliveries SET claimed_at = @now
WHERE id IN (
    SELECT id FROM deliveries
    WHERE status = 'PENDING'
      AND updated_at < @older_than
      AND (claimed_at IS NULL OR claimed_at < @claim_expired_before)
    ORDER BY updated_at, id
    LIMIT @row_limit
    FOR UPDATE SKIP LOCKED
)
RETURNING *;
```

دو مکانیزم، دو بازهٔ زمانی، و هیچ‌کدام جای دیگری را نمی‌گیرد:

```
SKIP LOCKED   لحظهٔ رقابت را حل می‌کند       میلی‌ثانیه
claimed_at    مدت را نگه می‌دارد              اجاره
```

قفل فقط به‌اندازهٔ همین statement زنده است. `UnitOfWork` ای در کار نیست — postgres
هر statement تنها را در transaction ضمنی می‌گذارد.

# آنچه در بحث پیدا شد: اجاره بودجهٔ تلاش را نصف می‌کرد

این نکته تا وقتی `ARCHITECTURE.md` دوباره خوانده نشد دیده نشده بود. آنجا نوشته:

> *«فاصلهٔ بین آن دو تعیین می‌کند یک ردیف چند تلاش می‌گیرد: با reconcile هر پنج
> دقیقه و تسلیم در سی، یک ردیف تقریباً شش تا می‌گیرد.»*

یک ارسال که موقتاً شکست می‌خورد **هیچ‌چیز نمی‌نویسد** — ردیف PENDING می‌ماند. ولی
`claimed_at` پر است:

```
بدون آزادسازی   تا انقضای ۱۰ دقیقه‌ای دست‌نخوردنی   →  در ۳۰ دقیقه ۳ تلاش
با آزادسازی     sweep بعدی برش می‌دارد              →  در ۳۰ دقیقه ۶ تلاش
```

**اجاره بی‌صدا جای `RECONCILE_EVERY` را در آن حساب می‌گرفت.** پس شکست موقت ادعا را
صریحاً پس می‌دهد، و اجاره فقط یک معنی دارد:

```
انقضای اجاره     dispatcher مُرد
آزادسازی صریح    ارسال شکست خورد، فعلاً کارمان با ردیف تمام است
```

آزادسازی خودش best-effort است: شکستش فقط همان تفاوت را هزینه دارد، پس لاگ می‌شود
نه برگردانده.

# این «دقیقاً یک بار» نیست و ادعا نمی‌کند

```
A ادعا می‌کند → ارسال هنگ می‌کند → اجاره منقضی می‌شود → B می‌فرستد → ارسال A تمام می‌شود
```

ذاتیِ اجاره است. فقط کم می‌شود، با اجاره‌ای بلندتر از کندترین ارسال — و
`LoadDispatch` مقداری کوچک‌تر یا مساوی `ACK_WAIT` را رد می‌کند.

و یک تفکیک که باید روشن بماند:

```
ردیف      امن بود      RecordSent دومی ErrAlreadySettled می‌گیرد و settled() هیچش می‌کند
گیرنده    امن نبود     پیام از قبل رفته
```

claim **گیرنده** را محافظت می‌کند، نه ردیف را.

# migration جدا نشد

اول یک `00008_claim_stale_deliveries.sql` نوشته شد و بعد برداشته شد: هیچ‌چیز هنوز
deploy نشده، پس تاریخچهٔ schema برای چیزی که هرگز بالا نرفته فقط سر و صداست. ستون
داخل خودِ `00007` نشست و دیتابیس توسعه از صفر ساخته شد.

# Files Changed

- `migrations/00007_create_deliveries.sql` *(`claimed_at` + index جزئی sweep)*
- `internal/adapter/db/postgres/queries/delivery.sql` *(`ClaimStaleDeliveries`، `ReleaseDeliveryClaim`)* + `gen/`
- `internal/adapter/db/postgres/delivery.go` *(`ListStale` → `ClaimStale`، و `Release`)*
- `internal/adapter/db/postgres/delivery_test.go` *(۵ تست، یکی‌شان همزمانی واقعی)*
- `internal/core/domain/delivery/{port,tracker}.go`
- `internal/core/usecase/dispatch.go` *(`Recover` ادعا می‌کند، `sendFailed` پس می‌دهد)*
- `internal/core/usecase/{dispatch,fakes}_test.go`
- `internal/config/settings/dispatch.go` *(`ReconcileLease` + guard در برابر `AckWait`)*
- `internal/bootstrap/dispatcher.go`
- `.env.dispatcher.example`، `docs/CONFIG.md`، `docs/ARCHITECTURE.md`

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

مهم‌ترین تست چیزی است که فقط دیتابیس واقعی و همزمانی واقعی ثابتش می‌کند:

```
TestTwoSweepsAtOnceSplitTheRows    ۴ sweep هم‌زمان، ۲۰ ردیف
                                   →  مجموع برداشته‌شده ۲۰، هر کدام دقیقاً یک بار
TestAClaimExpires                  اجارهٔ صفر  →  ردیف دوباره برداشته می‌شود
TestAReleasedRowCanBeTakenAgain    آزاد شد    →  همان sweep بعدی برش می‌دارد
TestClaimingDoesNotAgeTheRow       updated_at تکان نمی‌خورد
```

و در سطح use case، `TestARowStillHeldIsSkipped` — که تا fake ادعاها را واقعاً نگه
ندارد، پاس نمی‌شود.

# Follow-ups / Risks

- **هنوز با دو dispatcher واقعی آزموده نشده.** همزمانی در سطح دیتابیس ثابت شده،
  ولی دو پروسه بالا نیامده‌اند.
- اجاره روی `10m` پیش‌فرض است و `ACK_WAIT` روی `60s`. اگر روزی یک provider کندتر
  از ده دقیقه شود، تنظیم `ACK_WAIT` کافی نیست — این هم باید با آن بالا برود.
- `notified_at` هنوز هیچ‌وقت پر نمی‌شود؛ `MarkNotified` صداکننده ندارد. ربطی به
  این کار ندارد ولی در همان مسیر است.

# Instruction

«۳ را بزنیم» — یعنی همان `claimed_at` که در فهرست باقی‌مانده‌ها بود. با دو
اصلاحی که در بحث درآمد: ادعا در شکست موقت آزاد شود، و migration جدا نشود چون
هنوز چیزی بالا نرفته.
