Branch: `feat/portal-spec`

# Summary

پورتال از «وارد شو و اسمت را ببین» رسید به چیزی که مشتری واقعاً با آن کار
می‌کند: source ثبت می‌کند، کلید می‌گیرد، فرستنده و callback تعریف می‌کند — و
هیچ‌کدام تا وقتی یک اپراتور تأیید نکرده چیزی نمی‌فرستد. پیاده‌سازیِ
`docs/superpowers/plans/2026-08-29-portal-sources.md`.

## تأیید تقریباً مجانی بود

فکر می‌کردم باید ساخته شود. نبود:

```
source/auth.go     EnsureActive()  داخلِ Authenticate  ← کلید رد می‌شود
source/service.go  EnsureActive()  داخلِ Load          ← پیام رد می‌شود
```

`sources.is_active` از قبل همان گیت بود. تنها کاری که لازم بود این بود که
پیش‌فرضِ ستون `FALSE` شود. نه چکِ تازه‌ای، نه راهی دور از آنکه هست.

`approved_at` هم اضافه شد ولی **رکورد است نه گیت** — هیچ‌چیز برای تصمیم
نمی‌خواندش. فقط برای اینکه صفِ بررسی بتواند بپرسد «چه چیزی هرگز تأیید نشده»
بدونِ اینکه هرچه ماهِ پیش خاموش شد هم در فهرست بیاید.

## چیزی که spec جوابش را نداده بود

`Load` خودش `EnsureActive` را صدا می‌زند، و `Registrar.Register` و
`Credentials.Register` هر دو از آن رد می‌شدند. با sourceای که خاموش ساخته
می‌شود، مشتری **نمی‌توانست** برایش bot یا callback ثبت کند تا وقتی تأیید شود:
ثبت کن، منتظر بمان، برگرد، پیکربندی کن — و اپراتور پوسته‌ای خالی را تأیید کند.

دو مسیر جدا شدند. `Load` مالِ فرستادن ماند و `Manage` اضافه شد که ردیف را
می‌خواند و فعال بودن را لازم ندارد. پنج فراخوانِ مدیریتی منتقل شدند.

همان‌جا یک تست شکست — `TestRegisterRefusesAnInactiveSource` — و **درست شکست**.
برعکس شد و اسمش را گذاشتم `TestACallbackCanBeSetOnASourceThatCannotSendYet`،
با دلیلش در کامنت.

## Gate بالاخره caller گرفت

`usecase.Gate` را در تغییرِ قبلی ساختیم و هیچ‌کس صدایش نمی‌زد. حالا ثبتِ source،
صدور کلید و ابطالِ کلید هر سه از آن رد می‌شوند، و `audit_log` اولین ردیف‌های
عمرش را گرفت:

```
you@example.test | source.create | source
you@example.test | key.issue     | key
```

## دو قاعدهٔ ریز که با تست قفل شدند

**ابطالِ کلید اول فهرست می‌گیرد.** بدونش، کسی که یک source دارد می‌تواند شناسهٔ
کلیدِ مشتریِ دیگری را بنویسد و باطلش کند: مالکیت روی source چک می‌شود و شناسهٔ
کلید به‌تنهایی نمی‌گوید مالِ کدام source است.

**sourceِ دیگری `ErrNotFound` می‌دهد نه رد شدن.** رد شدن تأیید می‌کند که آن
شناسه وجود دارد، و شناسه حدس‌زدنی است در حالی که محتوای source نیست. تستِ صفحه
هر دو کد را با هم مقایسه می‌کند، نه اینکه فقط ۴۰۴ بودن را چک کند.

## صفحهٔ کلید redirect نمی‌کند

عمدی، و در کد نوشته شده: هر سه راهِ معمولِ «پیام را به صفحهٔ بعد ببر» — session،
flash، query string — یعنی کلید جایی نوشته شود که عمرش از آن صفحه بیشتر است.

# Files Changed

- `migrations/` *(شماره‌گذاریِ کامل: `users` رفت جلوی `sources`)*
- `migrations/00003_create_sources.sql` *(`owner_user_id`، `approved_at`، `is_active DEFAULT FALSE`، دو index)*
- `internal/core/domain/source/entity.go` *(`OwnerUserID`، `ApprovedAt`، `IsApproved`، `New`)*
- `internal/core/domain/source/{port,const,errors,service}.go` *(`Create`/`ListByOwner`/`KeyIssuer`/`Manage`)*
- `internal/core/domain/source/service_test.go` *(new)*
- `internal/core/usecase/{source,key}.go` *(new)*, `{source,key}_test.go` *(new)*
- `internal/core/usecase/const.go` *(`MaxSourcesPerUser` و verbها)*
- `internal/core/usecase/{register,credential}.go` *(پنج فراخوان به `Manage`)*
- `internal/adapter/db/postgres/source.go` *(`ListByOwner` و دو فیلدِ تازه)*
- `internal/adapter/db/postgres/queries/source.sql` *(`owner_user_id`، `ListSourcesByOwner`)*
- `internal/adapter/db/postgres/testing_test.go` *(وضعیتِ تمیز حالا یک صاحب دارد)*
- `internal/adapter/api/web/portal_{source,key,identity}.go` *(new)*
- `internal/adapter/api/web/portal_const.go` *(مسیرها، نامِ صفحه‌ها، فیلدها)*
- `public/templates/portal/{sources,source_new,source,keys,key_issued,senders,callback,callback_secret}.html` *(new)*
- `public/static/portal/portal.css` *(کارت، pill، empty، reveal — بدونِ رنگِ تازه)*
- `internal/config/console.go` *(`Crypto` و `WebhookPolicy`)*
- `internal/bootstrap/console.go` *(`consoleCore`، پنج use case)*
- `internal/bootstrap/const.go` *(`consoleRateLimit`)*
- `.env.console.example` *(کلیدهای crypto و سیاستِ callback)*
- `docs/CONFIG.md` *(ستونِ console برای crypto و webhook policy، و ترتیبِ تازهٔ migrationها)*

# Tests Run

- `make prepush` — pass
- روی دیتابیسِ واقعی، با خودِ باینری:

```
POST /sources          303   ردیف: is_active=f, approved_at IS NULL
صفحه                    Waiting for approval
UPDATE ... is_active=t  صفحه: Sending

کلید روی صفحه           srosha_KlbCGq7HNEm…
در دیتابیس              hash_len=64
ردیف‌هایی که خودِ کلید را دارند   ۰
تکرارِ کلید در صفحهٔ فهرست        ۰

callback روی sourceِ تأییدنشده   رمز یک‌بار نشان داده شد
در دیتابیس                        v1.1.… ← sealed
صفحهٔ callback بارِ دوم            هیچ reveal
```

- محافظ‌های جدول با دست: صاحبِ ناموجود سرِ **foreign key** رد شد (نه سرِ ulid)، و
  حذفِ کاربری که source دارد هم رد می‌شود.
- در حالتِ production، سیاستِ شلِ callback جلوی راه‌اندازی را می‌گیرد:
  `NOTIF_WEBHOOK_ALLOW_INSECURE_URL … must be off in production`.

# Follow-ups / Risks

- **Task 10 چیزی برای نوشتن نداشت.** قرار بود تست کنم که sourceِ بدونِ credential
  از هویتِ srosha استفاده می‌کند؛ `TestOursStandsInWhenTheSourceRegisteredNothing`
  و `TestANamedIdentityNeverFallsBack` از قبل بودند و همان را می‌سنجند. تستِ
  تکراری نوشتم و برداشتم.
- **صفحهٔ تأیید ساخته نشد.** فاز ۲. تا آن موقع تأیید یک `UPDATE` دستی است، مثلِ
  اولین اپراتور — و مثلِ همان، نباید عمرش از فاز ۲ بیشتر شود.
- **صفِ بررسی خواننده ندارد.** `approved_at` و `sources_unapproved_idx` هستند و
  هیچ‌کس نمی‌خواندشان.
- **`sources.UpdateSource` هنوز صفحه ندارد.** عوض‌کردنِ نام یا آدرسِ پیش‌فرض
  ممکن نیست.
- **`consoleRateLimit` یک عدد است نه سیاست.** `source.Service` یک limiter
  می‌خواهد و console هرگز `Admit` را صدا نمی‌زند، پس هیچ‌وقت مصرف نمی‌شود. اگر
  روزی console چیزی بفرستد، این عدد ناگهان معنی پیدا می‌کند.
- **artifactــِ طراحیِ Lapis هنوز جملهٔ قدیمی را دارد** — «No sender yet» به‌جای
  «Waiting for approval». قالب‌های واقعی درست‌اند.

# Instruction

«پورتال را ادامه بده» و بعدش «ok» برای اجرای plan. سه تصمیمِ مالک که این فاز را
شکل داد: sourceِ بدونِ credential از پیش‌فرضِ srosha استفاده می‌کند، بحثِ خرید و
پلن کنسل است و srosha با همه یکسان رفتار می‌کند، و مرحلهٔ تأییدِ ادمین برمی‌گردد.
