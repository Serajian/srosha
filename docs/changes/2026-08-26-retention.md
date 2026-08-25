Branch: `feat/retention`

# Summary

**جدول‌ها دیگر تا ابد رشد نمی‌کنند.** یک job شبانه پیام‌های کهنه‌تر از یک ماه را
پاک می‌کند، و delivery هایشان با کلید خارجی می‌روند.

srosha بایگانی نیست. یک پیام و delivery هایش به سؤال «چه بر سر این آمد» جواب
می‌دهند، و بعد از مدتی کسی نمی‌پرسد — پس نگه‌داشتنشان یعنی جدولی که فقط بزرگ‌تر
می‌شود، و query کندتر برای هر کسی که **می‌پرسد**.

# شرط «همه settled» عمداً نیست

اولین طرح این بود که فقط پیامی پاک شود که همهٔ delivery هایش تعیین تکلیف شده‌اند
— وگرنه کارِ نفرستاده بی‌صدا دور می‌رود. رد شد، و استدلالِ رد کردنش درست است:

```
RECONCILE_GIVE_UP   ۳۰ دقیقه
retention           ۱ ماه
```

هر delivery که بیش از ۳۰ دقیقه PENDING بماند، در sweep بعدی `FAILED` می‌شود. پس
ردیفی که **بعد از یک ماه** هنوز PENDING است یعنی recovery هرگز ندیدش — یک
ناهنجاری، نه کارِ در انتظار. و کاری که یک ماه نرفته، دیگر رفتنش ارزشی ندارد.

## ولی آن استدلال به یک چیز بند است

به اینکه آن دو عدد **دور** بمانند. اگر کسی retention را روی یک ساعت بگذارد، job
شروع می‌کند به پاک‌کردن کاری که recovery هنوز می‌فرستاد.

پس همان الگوی `RECONCILE_LEASE > ACK_WAIT`:

```go
r.Check(t.Age > d.ReconcileGiveUp*minRetentionMultiple, ...)
```

با پیش‌فرض‌ها نسبت واقعی **۱۴۴۰ برابر** است، پس این هرگز سر راه نیست. برای روزی
است که کسی یک عدد را بدون دیگری عوض کند — و آن روز در boot می‌فهمد، نه شش ماه
بعد.

# یک statement، نه دو

```sql
DELETE FROM notifications
WHERE id IN (SELECT id FROM notifications WHERE created_at < @before ORDER BY id LIMIT @row_limit)
```

delivery ها با `ON DELETE CASCADE` می‌روند، پس هیچ statement دومی نیست که باید با
این هماهنگ بماند. و یک عدد در config است نه دو تا که بتوانند با هم اختلاف پیدا
کنند.

**دسته‌ای**، چون یک `DELETE` بی‌سقف روی جدولی که یک سال جمع شده، یک transaction
است که روی همه‌اش قفل نگه می‌دارد.

## و run تا ته می‌رود

```
تا وقتی دسته‌ای کوتاه‌تر از سقف برگردد  →  چیزی کهنه‌تر نمانده
سقف ۱۰۰ دسته                          →  backstop، نه تنظیم
```

یک دسته در هر اجرا کافی نبود: جدولی که یک بار عقب بیفتد هرگز جبران نمی‌شد.

و توقفش بین دسته‌هاست نه وسط یکی — `select` روی `ctx.Done()` قبل از هر دسته. آنچه
رفته رفته است، و اجرای بعدی از همان‌جا ادامه می‌دهد.

# job دوم روی همان scheduler

```go
[]registry.Job{
    {Name: "recovery",  Schedule: cfg.Dispatch.ReconcileSchedule, Run: core.dispatcher.Recover},
    {Name: "retention", Schedule: cfg.Retention.Schedule,         Run: core.retention.Purge},
}
```

`registry.Scheduler` از روز اول `[]Job` می‌گرفت — دقیقاً برای همین. و
`WithSingletonMode` یعنی اگر یک sweep طولانی شد، بعدی رویش نمی‌افتد.

شبانه در ساعتی که خودمان انتخاب کرده‌ایم (`0 3 * * *`) نه هر ۲۴ ساعت، چون کار
سنگین نباید به زمان restart گره بخورد.

# Files Changed

- `internal/adapter/db/postgres/queries/notification.sql` *(`DeleteNotificationsBefore`)* + `gen/`
- `internal/adapter/db/postgres/notification.go` *(`DeleteOlderThan`)*
- `internal/adapter/db/postgres/notification_test.go` *(۳ تست integration)*
- `internal/core/domain/notification/{port,service}.go`
- `internal/core/usecase/retention.go` *(تازه)*، `const.go` *(تازه — سقف دسته‌ها)*
- `internal/core/usecase/{retention,fakes}_test.go`
- `internal/config/settings/retention.go` *(تازه)*، `const.go` *(نگهبان)*
- `internal/config/dispatcher.go`، `internal/config/config_test.go`
- `internal/bootstrap/dispatcher.go` *(job دوم)*
- `.env.dispatcher.example`، `docs/CONFIG.md`

هیچ migration ای لازم نشد: `ON DELETE CASCADE` از روز اول در schema بود.

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

```
DeletingAMessageTakesItsDeliveries   CASCADE واقعاً کار می‌کند
DeleteOlderThanTakesOneBatch         سقف رعایت می‌شود، و حلقه تا ته می‌رود
PurgeKeepsGoingUntilNothingIsLeft    ۱۰ ردیف با دستهٔ ۳ → همه رفتند
PurgeStopsWhenItIsToldTo             ctx لغو شده → آرام می‌ایستد
RetentionMustOutliveGivingUp         ۱ ساعت کنار ۳۰ دقیقه → boot رد می‌کند
```

و روی dispatcher واقعی، با `@every 5s` و `BATCH=2`:

```
سه پیام از ۴۰ روز پیش، هر کدام با یک delivery ــِ PENDING
یک پیام از امروز
                    ↓
INFO  old messages deleted  count=3  older_than=720h
                    ↓
notifications  فقط امروزی‌ها ماندند
deliveries     ۰ — با CASCADE رفتند
```

`BATCH=2` یعنی سه ردیف در **دو دسته** رفتند، که همان حلقه است. دادهٔ تستی بعدش
پاک شد.

# Follow-ups / Risks

- **delivery ــِ PENDING بدون سر و صدا پاک می‌شود.** تصمیم گرفته شده و مستند، ولی
  یعنی هیچ‌کس هرگز نمی‌فهمد یک ماه پیش کاری نرفت. یک شمارش در لاگ قبل از حذف
  ارزان است و آن نقطهٔ کور را می‌بندد.
- **retention فقط روی `notifications` است.** `sources`، `api_keys`، `credentials` و
  `webhooks` هیچ‌کدام انقضا ندارند — و نباید هم داشته باشند، ولی یعنی «جدول‌ها
  دیگر رشد نمی‌کنند» فقط دربارهٔ دو جدول درست است.
- job در dispatcher است. اگر روزی dispatcher خاموش بماند و gateway کار کند،
  پاک‌سازی هم می‌ایستد در حالی که نوشتن ادامه دارد.

# Instruction

«retention را بزن، ولی شرط نگذار» — چون با یک ماه، delivery ای که یک ماه نرفته
دیگر مهم نیست. با سه تصمیم: در dispatcher، شبانه ساعت ۳، و دسته‌ای تا وقتی چیزی
نماند.
