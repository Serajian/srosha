Branch: `refactor/domain-services`

# Summary

هسته از لایه‌ای به برش عمودی تبدیل شد. هر domain حالا port و service خودش را دارد،
و یک لایهٔ نازک بالای آن‌ها ترتیب و مرز transaction را می‌داند.

## چرا

`core/port/repository.go` یک فایل بود که **هر پنج aggregate** را می‌شناخت، و
`Submit` نه وابستگی داشت. هر domain تازه یک interface به آن فایل اضافه می‌کرد.

## آنچه سر جایش ماند و آنچه جابه‌جا شد

مالک پیشنهاد داد port و service بروند در domain خودشان و **ترتیب** در لایهٔ driving
انجام شود. با نیمهٔ اولش موافقت شد و نیمهٔ دومش با یک قید: orchestration در هسته
می‌ماند، نه در adapter.

دلیلش این بود که ترتیب کارهای `Submit` — سقف نرخ، فعال‌بودن، idempotency، resolve،
یک transaction، انتشار بعد از commit — هیچ‌کدام transport نیستند. اگر بروند در
handler، بدون بالا آوردن سرور تست نمی‌شوند، با ورودی دوم تکرار می‌شوند، و مرز
transaction می‌رود در adapter — که `docs/CONVENTIONS.md` صریحاً ممنوع کرده.

پس دو سطح شد: `domain/<name>/service.go` روی یک aggregate، و `core/usecase` که
آن‌ها را می‌راند.

## `core/port` و `core/service` هر دو حذف شدند

`core/service` شد `core/usecase`، چون `service` حالا اسم لایهٔ domain است.

`core/port` کلاً رفت. آنچه داشت پخش شد به جایی که مصرف می‌شود:

| | کجا رفت |
| --- | --- |
| هر `Repository` | `domain/<name>/port.go` |
| `RateLimiter` | `domain/source/port.go` — سقف نرخ به ازای source است |
| `Sender`, `SenderRegistry` | `domain/delivery/port.go` — فرستادن کار delivery است |
| `Publisher` | `domain/delivery/port.go` |
| `UnitOfWork` | بالای `usecase/submit.go` |
| `Clock`, `IDGenerator` | حذف شدند — تابع شدند، پایین |

قاعده‌اش در `CONVENTIONS.md` نوشته شد: **هیچ package ای که کارش فقط نگه‌داشتن
interface باشد وجود ندارد.**

## یک اشتباه که وسط کار گرفته شد

اول `LoadForDispatch` در `delivery.Repository` نوشته شده بود که هم delivery
برمی‌گرداند هم notification — یعنی یک repository که **دو aggregate** را می‌شناسد،
دقیقاً همان کوپلینگی که برش عمودی برای حذفش است.

شد `ReadByID`، و خواندن هر دو کار usecase شد. هزینه‌اش یک query اضافه در dispatch
است؛ هر دو lookup روی کلید اصلی‌اند. به‌عنوان قانون نوشته شد:

> A repository interface that names two aggregates is a repository in the wrong
> package.

## واژگان یکدست repository

مالک این را در `delivery` شروع کرد و به بقیه تعمیم داده شد:

```
Create  ReadByID  ReadByIdempotencyKey
CreateByList  ReadByID  PageByNotificationID  ListStale  Update
ListBySourceAndChannel
Create  ReadBySourceID  Update
```

`Read` یکی، `List` چند تا، `Page` یک صفحه. فیلتر در پسوند `By...`.

و قانونش نوشته شد: **repository زبان CRUD حرف می‌زند، service زبان کسب‌وکار**
(`Open`، `Sent`، `Failed`، `Publish`، `Admit`). یک اسم کسب‌وکاری روی یک lookup ساده
پیداکردنش را سخت‌تر می‌کند و خواندنش را روشن‌تر نمی‌کند؛ یک اسم CRUD روی یک عملیات
کسب‌وکار معنا را دور می‌اندازد.

## `Clock` و `IDGenerator` دیگر interface نیستند

```go
type (
	NowFunc func() time.Time
	IDFunc  func() ID
)
```

یک interface تک‌متده و یک تابع همان قراردادند و تابع سبک‌تر است. `delivery.IDFunc`
که از قبل بود با همین یکی شد.

## وابستگی‌های `Submit` از ۹ به ۶

**`source.Admit`** سقف نرخ و فعال‌بودن را یکی کرد. یک سؤال است — «آیا این source
الان حق دارد کاری بکند؟» — و حالا هیچ use case ای نمی‌تواند یکی از آن دو را فراموش
کند.

**`ids` و `clock`** رفتند داخل domain service ها. entity همچنان زمان و id را به‌عنوان
آرگومان می‌گیرد، پس آن قاعده نشکست؛ تزریق یک لایه پایین‌تر رفت.

اختلاف چند میکروثانیه‌ای بین `notification.CreatedAt` و `delivery.UpdatedAt` مسئله
نیست: اولی یعنی «پیام پذیرفته شد» و دومی یعنی «این delivery باز شد». دو رویداد
متفاوت، دو زمان.

## و `usecase` فقط use case ماند

`port.go`، `errors.go` و `types.go` حذف شدند. هر فایل خودکفاست: ورودی، خروجی، منطق.

# Files Changed

- `internal/core/domain/*/port.go`، `service.go` *(جدید — پنج domain)*
- `internal/core/usecase/submit.go`، `query.go` *(از `core/service` منتقل و بازنویسی)*
- `internal/core/usecase/fakes_test.go`، `submit_test.go`، `query_test.go`
- `internal/core/shared/clock.go` *(جدید)*
- `internal/core/port/`، `internal/core/service/` *(حذف)*
- `docs/CONVENTIONS.md` *(قاعدهٔ port، نام‌گذاری repository در برابر service)*
- `.golangci.yml` *(`shadow` خاموش شد)*

# Tests Run

- `make prepush` — سبز: fmt، vet، arch-check، golangci-lint (`0 issues`)، `go test -race ./...`
- ۲۰ تست در `usecase`، همه با fake روی repository ها و سرویس‌های واقعی domain

# Follow-ups / Risks

- تست‌های `usecase` عمداً **repository** ها را fake می‌کنند نه domain service ها را،
  تا منطق واقعی domain اجرا شود. اگر روزی کسی سرویس‌ها را fake کند، آن هفت حالت رد
  در `TestSubmitRefuses` دیگر چیزی را اثبات نمی‌کنند.
- `dispatch`، `notify`، `reconcile` و `register` هنوز نوشته نشده‌اند. `dispatch` به
  `attempt` در امضای handler نیاز دارد، چون تعداد تحویل را broker می‌داند نه ما.
- `shadow` در golangci خاموش شد: به `if err := f(); err != nil` ایراد می‌گرفت، که
  idiom است نه اشتباه.

# Instruction

مالک تشخیص داد که ساختار لایه‌ای دارد کد را کثیف می‌کند و خواست port و service به
domain خودشان بروند. سر جای orchestration بحث شد و قرار شد در هسته بماند. بعد
واژگان repository را در `delivery` شروع کرد و خواست به بقیه تعمیم داده شود، و در
پایان چهار ایراد روی `usecase` گرفت — port، types، errors، و تعداد وابستگی‌ها — که
هر چهار تا بسته شدند.
