Branch: `feat/postgres-repositories`

# Summary

آخرین repository نوشته شد. ولی قبلش یک تصادم واقعی بین port و query ها باید حل
می‌شد، و حلش core را عوض کرد.

## تصادم

port یک `Update(w)` داشت. ولی موقع مرور `webhook.sql` تصمیم گرفته بودیم
**`UpdateWebhook` واحد نداشته باشیم**، چون دو نویسندهٔ کاملاً متفاوت به یک ردیف
می‌نویسند:

```
dispatcher  ── بعد از هر callback ──→ consecutive_failures
source API  ── وقتی آدرس عوض شود ──→ callback_url

یک statement که همهٔ ستون‌ها را می‌نویسد یعنی:

  t1: source آدرس را عوض می‌کند             → callback_url = جدید
  t2: callback ای که در راه بود موفق می‌شود
  t3: کل ردیف را برمی‌گرداند سر جایش         → callback_url = قدیمی ✗
```

و entity هیچ ردیابی‌ای ندارد که کدام فیلد عوض شده، پس `Update(w)` نمی‌تواند بفهمد
کدام statement را صدا بزند.

## port به شکل statement ها درآمد

```go
Create(ctx, w) error
ReadBySourceID(ctx, sourceID) (*Webhook, error)
Redirect(ctx, w) error
RecordSuccess(ctx, w) error
RecordFailure(ctx, w) (int, error)
Deactivate(ctx, w) error
Activate(ctx, w) error
```

## شمارش از حافظه به SQL منتقل شد

این مهم‌ترین تغییر است. `RecordWebhookFailure` در SQL می‌شمارد و عدد جدید را
برمی‌گرداند، ولی `Webhook.RecordFailure` هم در حافظه `++` می‌کرد — یعنی یا دوبار
می‌شمردند، یا اگر شمارش حافظه نوشته می‌شد increment گم می‌شد.

حالا ترتیب برعکس است — **اول می‌شمارد، بعد قضاوت می‌کند**:

```
repo.RecordFailure()  →  SQL می‌شمارد، عدد واقعی برمی‌گردد
        ↓
w.RecordFailure(count, max, now)  →  domain قانون را روی همان عدد می‌زند
        ↓
تازه از فعال به غیرفعال رفت؟  →  repo.Deactivate()
```

امضای `Webhook.RecordFailure` عوض شد: دیگر خودش نمی‌شمارد، عدد را می‌گیرد. entity
حالا فقط **قانون** را دارد. نوشتن دوم فقط در همان یک بار که webhook خاموش می‌شود
اتفاق می‌افتد، نه در هر failure.

`TestFailuresAreCountedInStorageNotInMemory` همان صحنه را می‌سازد: دو نسخه از یک
webhook خوانده می‌شود، هر دو صفر failure دارند، و شمارش‌ها ۱ و ۲ برمی‌گردند نه ۱ و
۱. اگر در حافظه می‌شمرد، آن endpoint هیچ‌وقت به حدی که خاموشش می‌کند نمی‌رسید.

## `SetActive` شد جفتِ `Deactivate`/`Activate`

اول یک متد بود که جهت را از خود entity می‌خواند. اسمش گمراه‌کننده بود — مثل یک
فرمان خوانده می‌شد در حالی که یک ذخیره‌سازی بود. حالا جفت است و جهت از صدازننده
می‌آید، هم‌شکل `SourceRepository`.

و این یک شکاف قدیمی را بست: کامنت `Registrar.Deactivate` وعده می‌داد «روشن کردن
دوباره یعنی ثبت‌نام مجدد نیست»، ولی هیچ راه برگشتی وجود نداشت. `Webhook.Activate`
در entity بود و `SetWebhookActive` در SQL، ولی هیچ مسیری از بالا به آن نمی‌رسید.
حالا `Registrar.Activate → Service.Activate → repo.Activate` کامل است.

## یک استثنا که عمدی است

`Deactivate`/`Activate` صفر ردیف را **موفقیت** حساب می‌کنند — برخلاف هر جای دیگر
این فایل. statement فقط ردیفی را می‌گیرد که در آن حالت نیست، و دو callback که
هم‌زمان از حد رد شوند هر دو به اینجا می‌رسند؛ دومی چیزی عوض نکرده و چیزی خراب نیست.

هزینه‌اش این است که یک webhook پاک‌شده از یکی که از قبل خاموش بوده قابل تشخیص نیست.
در کد نوشته شده.

بقیهٔ نوشتن‌ها صفر ردیف را `webhook.ErrNotFound` می‌کنند، چون همه با entity ای صدا
زده می‌شوند که همین الان خوانده شده.

# Files Changed

- `internal/adapter/db/postgres/webhook.go` *(تازه — repository + mapping)*
- `internal/adapter/db/postgres/webhook_test.go` *(تازه — نه تست)*
- `internal/core/domain/webhook/port.go` *(هفت متد به‌جای سه‌تا)*
- `internal/core/domain/webhook/service.go` *(`RecordFailure` برعکس شد، `Activate` اضافه شد)*
- `internal/core/domain/webhook/entity.go` *(`RecordFailure` حالا عدد می‌گیرد)*
- `internal/core/domain/webhook/entity_test.go` *(امضای تازه)*
- `internal/core/usecase/register.go` *(`Registrar.Activate`)*
- `internal/core/usecase/fakes_test.go` *(fake خودش می‌شمارد، مثل دیتابیس)*

# Tests Run

- `go test -tags integration ./internal/adapter/db/postgres/` — ۳۷ تست، همه سبز
- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

# Follow-ups / Risks

- سطح ادمین `credential` هنوز ناقص است: statement ای برای خاموش/روشن کردن یک
  credential یا دادن default به یکی که از قبل هست وجود ندارد.
- `MarkDeliveryNotified` هنوز صدا زننده ندارد.
- `ListStaleDeliveries` ردیف را claim نمی‌کند؛ دومین dispatcher اول یک ستون
  `claimed_at` می‌خواهد.
- `Source.ID` هنوز `string` است نه `shared.ID`.
- هیچ‌کدام از این repository ها هنوز در `registry` وصل نشده‌اند.

# Instruction

«برویم webhook» — و بعد از طرح تصادم، «الف» (port به شکل statement ها) و سپس «ب»
(جفتِ `Deactivate`/`Activate` به‌جای یک `SetActive`).
