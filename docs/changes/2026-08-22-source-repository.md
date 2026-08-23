Branch: `feat/postgres-repositories`

# Summary

اولین repository نوشته شد، و با آن پایه‌ای که بقیه رویش سوار می‌شوند: پیدا کردن
transaction، ترجمهٔ خطای درایور، و mapping بین ردیف و entity.

`source` عمداً اول انتخاب شد چون کوچک‌ترین است — و چون هر جدول دیگری کلید خارجی به
`sources` دارد، پس بدون آن هیچ تست یکپارچگی دیگری ممکن نیست.

## transaction چطور پایین می‌رود

port از قبل گفته بود چطور: «adapter تصمیم می‌گیرد چطور، و transaction را از طریق
ctx پایین می‌فرستد». پس همهٔ منطقش یک تابع است:

```
q := r.q(ctx)
      ↓
ctx یک tx دارد؟  →  gen.New(pool).WithTx(tx)
وگرنه            →  gen.New(pool)
```

هر statement قبل از اجرا این را می‌پرسد. اگر نپرسد، نوشتنش بیرون از بلوک اتمیکی
می‌افتد که core خواسته و از rollback آن جان سالم به در می‌برد.

`txKey` یک تایپ خصوصی است، پس هیچ‌کس از بیرون این پکیج نمی‌تواند transaction بگذارد
یا برداردش.

## ترجمهٔ خطا

سه تابع در `errors.go`. مهم‌ترینش روی **کد** خطا و **نام قید** تطبیق می‌دهد نه روی
متن:

```go
func violates(err error, constraint string) bool   // 23505 + نام قید
```

متن خطا برای آدم نوشته شده و بین نسخه‌ها عوض می‌شود. و نام قید لازم است چون یک جدول
چند قید یکتا دارد که معنی‌شان فرق می‌کند: کلید idempotency تکراری یعنی کلاینت دوباره
فرستاده، ولی hash کلید تکراری یعنی باگ.

`failed` مرز نشت را نگه می‌دارد: پیامی که کلاینت می‌بیند فقط اسم عملیات را دارد، و
متن درایور — که host و port و نام قید را حمل می‌کند — در reason می‌ماند و فقط به لاگ
می‌رود.

## mapping

یک ردیف که map نمی‌شود، **خطای داخلی** است نه ورودی نامعتبر: وقتی نوشته شد معتبر
بود، پس اگر حالا خوانده نمی‌شود چیزی سمت ما عوض شده و هیچ کلاینتی نمی‌تواند درستش
کند. `badRow` همین را می‌گوید و ستون و شناسه را در reason می‌گذارد.

`default_addresses` که خالی یا null باشد یک map خالی است نه خطا — source بدون آدرس
پیش‌فرض حالت عادی است و `Resolve` خودش کانالی که چیزی برایش پیدا نکند را رد می‌کند.

## چیزی که در core کم بود

`source` تنها domain بدون `ErrNotFound` بود؛ چهارتای دیگر دارند. و `Load` مستقیم
`src.EnsureActive()` را روی هر چه repository برگرداند صدا می‌زند — یعنی اگر آنجا
`nil, nil` برمی‌گشت، panic بود. sentinel اضافه شد.

## تست‌ها با tag

`//go:build integration`. پس `go test ./...` معمولی اصلاً کامپایلشان نمی‌کند و
`prepush` سریع و بدون نیاز به دیتابیس می‌ماند.

و داخلشان، اگر دیتابیسی نبود **skip** می‌کنند نه fail: کسی که با tag اجرا می‌کند ولی
کانتینر ندارد باید بشنود «make dev-up بزن»، نه یک دیوار خطای اتصال.

`truncate sources CASCADE` هر تست را با دیتابیس تمیز شروع می‌کند — نام بردن از
`sources` کافی است چون بقیه از آن آویزان‌اند.

دو تست چیزی را می‌بندند که موقع مرور query ها تصمیم گرفته شد:

- `TestSuspendedSourceStillReadsBack` — ردیف برمی‌گردد تا domain بتواند بگوید
  «معلق است» نه «وجود ندارد»
- `TestUpdateLeavesTheActiveFlagAlone` — تغییر نام یک مشتری، تعلیقش را برنمی‌گرداند

# Files Changed

- `internal/adapter/db/postgres/postgres.go` *(تازه — `base`، `txKey`، `withTx`)*
- `internal/adapter/db/postgres/errors.go` *(`noRows`، `violates`، `failed`)*
- `internal/adapter/db/postgres/mapper.go` *(`toSource`، `toAddresses`، `fromAddresses`، `badRow`)*
- `internal/adapter/db/postgres/source.go` *(`SourceRepository` با `ReadByID`، `Create`، `Update`، `Deactivate`، `Activate`)*
- `internal/adapter/db/postgres/testing_test.go` *(تازه — `connect`، `truncate`، `ulid`)*
- `internal/adapter/db/postgres/source_test.go` *(تازه — شش تست)*
- `internal/core/domain/source/errors.go` *(`ErrNotFound`)*
- `Makefile` *(`test-integration` حالا با tag اجرا می‌کند)*

# Tests Run

- `go test -tags integration ./internal/adapter/db/postgres/` — شش تست، همه سبز
- همان با دیتابیس خاموش — skip شدند، نه شکست
- `make prepush` — سبز، و تست‌های یکپارچگی را اصلاً کامپایل نمی‌کند

# Follow-ups / Risks

- `withTx` نوشته شده ولی هنوز صدا زننده ندارد: `UnitOfWork` بعد از اینکه دو
  repository وجود داشته باشند نوشته می‌شود.
- `Create`، `Update`، `Deactivate` و `Activate` در port نیستند. عمداً نوشته شدند
  (تصمیم موقع مرور `source.sql`) چون تست‌های بقیهٔ repository ها به ردیف source
  نیاز دارند و مسیر ثبت source قدم بعدی است.
- `internal/adapter/db/postgres/query.go` هنوز یک stub خالی است.
- دو مسابقه‌ای که در query ها بسته شد — پیام تکراری و delivery حل‌شده — هنوز به core
  نرسیده‌اند. هر کدام یک sentinel و یک تغییر کوچک در use case می‌خواهند، و با
  repository خودشان می‌آیند.

# Instruction

«برویم repository ها را بنویسیم» — با سه تصمیمی که قبلش گرفته شد: transaction از
طریق ctx، تست‌های یکپارچگی با build tag به‌علاوهٔ skip وقتی دیتابیس نیست، و شروع از
`source`.
