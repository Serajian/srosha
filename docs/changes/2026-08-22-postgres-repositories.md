Branch: `feat/postgres-repositories`

# Summary

سه repository باقی‌مانده نوشته شد — `notification`، `delivery`، `credential` — به‌علاوهٔ
`UnitOfWork`. و در میانهٔ کار ساختار خودِ پکیج عوض شد، چون داشت شلوغ می‌شد.

## ساختار: هر domain یک فایل

قبلش پکیج به‌شکل «لایه» تقسیم شده بود: `mapper.go` همهٔ mapping ها را داشت،
`errors.go` همهٔ ترجمهٔ خطاها را، `query.go` هم یک stub خالی بود که هیچ‌وقت پر نشد.
با یک repository جواب می‌داد؛ با چهارتا یعنی برای فهمیدن `delivery` باید سه فایل را
هم‌زمان باز نگه می‌داشتی.

```
قبل                          بعد
----                         ----
postgres.go   ← tx           postgres.go       ← tx + UoW + خواندن خطای درایور
errors.go     ← همه          source.go         ← repository + mapping خودش
mapper.go     ← همه          notification.go   ← repository + mapping خودش
uow.go        ← tx           delivery.go       ← repository + mapping خودش
query.go      ← خالی         credential.go     ← repository + mapping خودش
```

`mapper.go`، `errors.go`، `uow.go` و `query.go` حذف شدند. آنچه واقعاً **مشترک** بود
— پیدا کردن transaction، `UnitOfWork`، و `noRows`/`violates`/`failed`/`badRow` —
در `postgres.go` ماند. mapping هر domain کنار همان repository است چون فقط او
صدایش می‌زند.

## ترتیب: CRUD

متدهای هر repository و statement های هر فایل query به ترتیب **Create → Read →
Update → Delete** چیده شدند. `api_key.sql`، `credential.sql`، `source.sql` و
`webhook.sql` جابه‌جا شدند؛ محتوایشان دست نخورد (فقط ترتیب بلوک‌ها).

در `webhook.sql` یک نکته بود: کامنتی که می‌گوید «چرا `UpdateWebhook` واحد نداریم»
سرفصل گروه update هاست نه توضیح یک statement، پس بالای اولین update ماند وگرنه sqlc
آن را به `ReadWebhookBySourceID` می‌چسباند.

## دو مسابقه‌ای که به core رسید

هر دو موقع مرور query ها در SQL بسته شده بودند و منتظر repository خودشان بودند.

**۱ — کلید idempotency.** `Submit` قبل از ساختن هر چیزی کلید را چک می‌کند، ولی
کلاینتی که timeout خورده و دوباره فرستاده دقیقاً دو درخواست دو طرف همان چک می‌گذارد:

```
req A: چک → چیزی نیست
req B: چک → چیزی نیست
req A: نوشت  →  1 row
req B: نوشت  →  0 row  →  notification.ErrDuplicateKey
                              ↓
              transaction اش rollback شد، پس چیزی از ما نماند
                              ↓
              مالِ آن‌ها را می‌خواند و همان جوابی را می‌دهد که چک می‌داد
```

`ON CONFLICT ... DO NOTHING` تعارض را **می‌گیرد** به‌جای اینکه بالا بیندازد، پس صفر
ردیف یعنی «آن یکی برد» — یک نتیجه، نه یک خطا.

**۲ — دو worker روی یک delivery.** این چیزی است که at-least-once واقعاً تولید
می‌کند:

```
worker A و B هر دو نسخهٔ PENDING را می‌خوانند
هر دو می‌فرستند، هر دو در حافظه موفق می‌شوند
A می‌نویسد  →  1 row
B می‌نویسد  →  0 row  →  delivery.ErrAlreadySettled
```

`settled()` از قبل `ErrInvalidTransition` را موفقیت حساب می‌کرد، ولی آن حالتِ
**حافظه** بود — نسخه‌ای که در دست داریم از اول terminal بوده. حالتِ **ردیف** را فقط
دیتابیس می‌بیند، و حالا هر دو sentinel موفقیت‌اند.

## credential: یک جدول، دو مسیر

جدول دو چیز نگه می‌دارد و entity فقط جای یکی را دارد:

```
credentials
├── id, name, is_default, is_active   ← هویت (core این را می‌بیند)
└── config, secret                    ← ابزار (فقط sender registry، لحظهٔ ارسال)
```

`credential.Credential` عمداً جایی برای secret ندارد، وگرنه domain باید بفهمد SMTP
چیست. پس `ListBySourceAndChannel` فقط ستون‌های هویت را می‌خواند و `ReadMaterial`
فقط دو ستون دیگر را. هیچ‌کدام هر دو را با هم نمی‌خواند.

secret **همان‌طور که ذخیره شده** برمی‌گردد، یعنی هنوز رمزشده. خود مقدار می‌گوید با
کدام کلید رمز شده، پس بازکردنش کار کسی است که کلیدها را دارد نه کار لایهٔ دیتابیس —
درز رمزگشایی همین‌جاست.

و جابه‌جا کردن default دو نصفه دارد که هیچ‌کدام تنها امن نیست: بدون `ClearDefault`
ایندکس ردیف جدید را رد می‌کند، و بدون `Create` کانال بدون default می‌ماند. هر دو در
یک transaction.

## سه تصمیم کوچک‌تر

- `CreateByList` تعداد نوشته‌شده را چک می‌کند. `copyfrom` می‌گوید چند ردیف نوشت، و
  کمتر بودنش یعنی ردیف‌هایی بی‌صدا افتاده‌اند — پیامی که یک گیرنده‌اش گم شده و
  هیچ‌کس نمی‌فهمد.
- `PageByNotificationID` یک ردیف بیشتر می‌خواهد. همان ردیف اضافه کل جواب «صفحهٔ
  بعدی هست؟» است، بدون query دومی که بشمارد.
- `DeliveryRepository` ساعت می‌گیرد چون port سن می‌خواهد نه لحظه
  (`ListStale(olderThan)`). تزریق‌شده، پس تست می‌تواند ساعت را دو ساعت جلو ببرد
  به‌جای اینکه دو ساعت صبر کند.

# Files Changed

- `internal/adapter/db/postgres/postgres.go` *(حالا tx + `UnitOfWork` + خواندن خطای درایور)*
- `internal/adapter/db/postgres/notification.go` *(تازه — repository + mapping)*
- `internal/adapter/db/postgres/delivery.go` *(تازه — repository + mapping)*
- `internal/adapter/db/postgres/credential.go` *(تازه — repository + mapping)*
- `internal/adapter/db/postgres/source.go` *(mapping خودش آمد داخلش، ترتیب CRUD)*
- `internal/adapter/db/postgres/{mapper,errors,uow,query}.go` *(حذف)*
- `internal/adapter/db/postgres/{notification,delivery,credential,uow}_test.go` *(تازه)*
- `internal/adapter/db/postgres/queries/{api_key,credential,source,webhook}.sql` *(ترتیب CRUD)*
- `internal/core/domain/notification/errors.go` *(`ErrDuplicateKey`)*
- `internal/core/domain/delivery/errors.go` *(`ErrAlreadySettled`)*
- `internal/core/usecase/submit.go` *(`raced`، `duplicateOf`)*
- `internal/core/usecase/dispatch.go` *(`settled` حالا هر دو sentinel را می‌پذیرد)*

# Tests Run

- `go test -tags integration ./internal/adapter/db/postgres/` — ۲۹ تست، همه سبز
- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- `make sqlc` — خروجی تولیدشده بعد از جابه‌جایی query ها بدون تغییر ماند

# Follow-ups / Risks

- `webhook.go` هنوز stub است. آخرین repository باقی‌مانده.
- سطح ادمین `credential` ناقص است: هیچ statement ای برای خاموش/روشن کردن یک
  credential یا دادن default به یکی که از قبل هست وجود ندارد. تست‌ها برای همین با
  SQL خام خاموش می‌کنند.
- `Create`/`ClearDefault`/`ReadMaterial` در هیچ port ای نیستند — منتظر use case
  ادمین و `SenderRegistry`.
- `MarkDeliveryNotified` هنوز صدا زننده ندارد: چیزی `notified_at` را نمی‌نویسد.
- `ListStaleDeliveries` ردیف را claim نمی‌کند. یک dispatcher امن است؛ دومی اول یک
  ستون `claimed_at` می‌خواهد.
- `Source.ID` هنوز `string` است نه `shared.ID`.

# Instruction

«برویم repository ها را بنویسیم» و بعد «notification»، «delivery»، «credential» —
با دو تصمیم در میانه: «sqlc بماند ولی فایل mapper و errors حذف شود، هر domain یک
فایل» و «ترتیب همه را بر اساس CRUD بگذار».
