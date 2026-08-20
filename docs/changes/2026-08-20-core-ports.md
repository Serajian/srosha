Branch: `feat/core-ports`

# Summary

`internal/core/port` نوشته شد: دوازده interface و هیچ چیز دیگر. تایپ‌های قرارداد به
`shared` رفتند، صفحه‌بندی با cursor کار می‌کند، و یک بند از `docs/CONVENTIONS.md`
اصلاح شد چون با ساختار این repo نمی‌خواند.

## قانون port ها اصلاح شد

بند قبلی می‌گفت «هر domain یک `port.go` دارد که هر دو جهت را اعلام می‌کند». آن از
الگویی می‌آید که در آن هر domain یک برش عمودی است — entity و service و port در یک
package. این repo لایه‌ای است: `domain/` فقط aggregate هاست و سرویس جای دیگری است.
عیناً اجرا کردنش یعنی `domain/notification/port.go` باید interface سرویسی را اعلام
کند که هرگز صدایش نمی‌زند.

بند دوم همان بخش دقیق‌تر بود و با هر دو ساختار کار می‌کند، پس همان مبنا شد:

> a port is declared by its **consumer** ... An interface the core declares and
> never calls belongs on the other side of the boundary.

## سه چیز که به همین دلیل حذف شدند

**`Subscriber`** — هسته هیچ‌وقت `Subscribe` صدا نمی‌زند؛ `bootstrap` می‌زند.

**`EventHandler`** — نه interface بود نه نیاز هسته. چیزی است که هسته **می‌دهد**، و
چون Go تایپ‌بندی ساختاری دارد، adapter نتس خودش اعلامش می‌کند و امضاها بدون هیچ
اعلام مشترکی جفت می‌شوند.

**`Driving`** اصلاً نوشته نشد. مصرف‌کنندهٔ سرویس، adapter است، پس interface باریکی که
لازم دارد را خودش کنار تستش اعلام می‌کند.

## `port` فقط interface

اول تایپ‌ها را هم آنجا گذاشته بودم — `DispatchEvent`، `Message`، `SendError`،
صفحه‌بندی. مالک ایراد گرفت که port باید یک قرارداد باشد، یک اعلام نیاز، نه محل
تعریف تایپ.

تایپ‌ها رفتند به `shared`، که تعریفش دقیقاً همین است: چیزهایی که به یک aggregate
تعلق ندارند و هیچ چیز داخلی import نمی‌کنند. `port` هم می‌تواند از آن بخواند بدون
چرخه.

## صفحه‌بندی با cursor نه offset

```go
type Cursor struct { After *ID; Limit int }
type Pagination[T any] struct { Items []*T; NextCursor *ID }
func (p Pagination[T]) HasNext() bool { return p.NextCursor != nil }
```

با ULID طبیعی جفت می‌شود: ULID بر اساس زمان مرتب است، پس
`WHERE id > $after ORDER BY id` هم موقعیت را می‌دهد هم ترتیب، بدون ستون اضافه.

offset دو ایراد دارد: دیتابیس همهٔ ردیف‌های قبل از صفحه را می‌شمارد و دور می‌اندازد،
و اگر مجموعه زیر دست عوض شود ردیف‌ها **جا می‌افتند یا تکرار می‌شوند**.

`HasNext` عمداً متد است نه فیلد. به‌عنوان فیلد دستی پر می‌شد و یک بار فراموش‌کردنش
یعنی client صفحه‌بندی را متوقف کند در حالی که ردیف‌ها هنوز مانده‌اند — بی‌سروصدا و
بدون خطا. همان کاری که `WasDowngraded()` و `Notification.Status()` می‌کنند.

## تصمیم‌هایی که در خود port ثبت شدند

**`Save` فقط یک delivery می‌نویسد.** متد ذخیرهٔ کل مجموعه عمداً وجود ندارد: چند
worker همزمان delivery های یک پیام را می‌بندند و نوشتن مجموعه‌ای نتیجهٔ همدیگر را
پاک می‌کند.

**`LoadForDispatch`** هر دو aggregate را با یک فراخوانی می‌دهد، چون dispatcher هم
وضعیت می‌خواهد هم متن.

**`ListStale`** فقط شناسه برمی‌گرداند — reconciler می‌خواهد دوباره منتشر کند نه
بخواند.

**`FindByIdempotencyKey`** وقتی کلید استفاده نشده `nil` می‌دهد نه خطا: «قبلاً دیده
نشده» یک جواب است نه یک شکست.

**`UnitOfWork.Atomically`** transaction را داخل `ctx` می‌فرستد، پس هسته هیچ‌وقت
`pgx.Tx` را نمی‌بیند.

# Files Changed

- `internal/core/port/repository.go` *(شش interface)*
- `internal/core/port/sender.go`، `messaging.go`، `system.go`
- `internal/core/shared/pagination.go`، `message.go`، `senderror.go` *(جدید)*
- `internal/core/shared/const.go` *(کران‌های صفحه)*
- `docs/CONVENTIONS.md` *(بند port ها)*

# Tests Run

- `make prepush` — سبز: fmt، vet، arch-check، golangci-lint (`0 issues`)، `go test -race ./...`

# Follow-ups / Risks

- `WebhookNotifier` عمداً نوشته نشد. شکل payload آن batch هنوز طراحی نشده — چند
  outcome در یک callback، با چه ساختاری و چه شناسه‌ای برای dedupe سمت مشتری. نوشتنش
  حالا یعنی اختراع قراردادی که هیچ‌کس نخواسته. `ListUnnotified` هم به همین دلیل در
  `DeliveryRepository` نیست.
- `SourceRepository` فقط `FindByID` دارد. مسیر احراز هویت (جست‌وجو با هش کلید API)
  وقتی adapter auth نوشته شود اضافه می‌شود؛ آن جدول هنوز طراحی نشده.
- هیچ‌کدام از این port ها هنوز پیاده‌سازی یا fake ندارند. اولین تست سرویس نشان
  می‌دهد کدامشان واقعاً قابل fake شدن‌اند — که آزمون واقعی اندازهٔ یک port است.

# Instruction

مالک خواست برویم سراغ port ها. تعارض بین `docs/CONVENTIONS.md` و ساختار repo مطرح
شد و قرار شد قانون اصلاح شود و `Driving` در adapter بماند. بعد خواست `port` فقط
interface باشد و صفحه‌بندی با cursor کار کند، و در پایان خواست برنچ جدا ساخته شود.
