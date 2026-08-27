Branch: `feat/sdk-module`

# Summary

**قدمِ اولِ SDK، و عمداً هیچ کدِ SDK ای در آن نیست.**

قرارداد جابه‌جا شد: `gen/` حذف، و کدِ تولیدشده حالا در `sdk/go/notification/v1`
داخلِ ماژولی از خودش می‌نشیند. سرور از همان‌جا می‌خواندش.

این قدم وقتی تمام است که **سرور دقیقاً مثل قبل build و pass شود**. اگر با نوشتنِ
کلاینت قاطی می‌شد، وقتی چیزی می‌شکست نمی‌شد فهمید از کدام بود.

# چه چیزی عوض شد

```
buf.gen.yaml    out: gen  →  out: sdk/go   و پیشوندِ go_package
gen/            حذف
sdk/go/         ماژولِ تازه — grpc و protobuf، و هیچ چیزِ دیگر
grpcsrv         شش خطِ import
go.mod          یک require و یک replace ./sdk/go
```

`go 1.23` در ماژولِ SDK، نه `1.26` ــِ سرور: یک SDK نباید مشتری را مجبور به ارتقا
کند.

# فرض ثابت شد

ادعای این چیدمان این بود که مشتری وابستگی‌های سرور را نمی‌گیرد. اندازه گرفته شد:

```
pgx · nats · go-mail · gocron · ulid     هیچ‌کدام
grpc · protobuf · x/net                  همین‌ها و بس
```

# دو چیزی که سرِ راه پیدا شد

## ۱ — `- gen` در `.golangci.yml` یک regex بود، نه یک مسیر

هر فایلی که «gen» در نامش داشت را از lint معاف می‌کرد — از جمله
`internal/adapter/system/idgen.go`، که یک غلط املایی داشت و کسی نمی‌دیدش.

با مسیرِ دقیقِ تازه (`sdk/go/notification`) آشکار شد و اصلاح شد. **این یک ایرادِ
موجود بود که تغییرِ من فقط نورش را انداخت رویش**، نه چیزی که ساخته باشمش.

## ۲ — `make format` دو تا `//nolint` را شکست

`golines` دو خطِ طولانی را wrap کرد و `//nolint:gosec` را از خطِ گزارش‌شده جدا
کرد:

```go
attempt := int(meta.NumDelivered) //nolint:gosec // …
        ↓ golines
attempt := int(
    meta.NumDelivered,
) //nolint:gosec // …          ←  دیگر روی خطِ درست نیست
```

در `internal/adapter/mq/nats/consumer.go` و
`internal/adapter/api/grpcsrv/mapper.go`. هر دو برگردانده شدند. **این ربطی به
این تغییر ندارد** و روی master هم اتفاق می‌افتد؛ در follow-ups ثبت شده.

`make format` در همان اجرا **۳۸ فایل** را بازفرمت کرد — `golines` که ظاهراً هرگز
روی این مخزن اجرا نشده بود. بیست‌ونه‌تای بی‌ربط برگردانده شدند. diff ــِ Go در این
commit دقیقاً شش خطِ import است و یک کلمه.

# تلهٔ CI که spec پیش‌بینی کرده بود

```
go test ./...   وارد ماژولِ تودرتو نمی‌شود
```

یعنی `make prepush` کلِ SDK را **بی‌صدا رد می‌کرد**. یک هدفِ `make sdk` اضافه شد که
build و vet و test و lint را داخلِ `sdk/go` اجرا می‌کند، و `prepush` به آن وابسته
است.

# Files Changed

- `sdk/go/go.mod` *(تازه)* و `sdk/go/notification/v1/*.pb.go` *(جابه‌جاشده)*
- `sdk/README.md` *(تازه — قاعده‌ای که هر زبان رعایت می‌کند)*
- `gen/` *(حذف)*
- `buf.gen.yaml`، `.golangci.yml`، `Makefile`
- `go.mod`، `go.sum` *(require + replace)*
- `internal/adapter/api/grpcsrv/*.go` *(شش خطِ import)*
- `internal/adapter/system/idgen.go` *(غلط املاییِ آشکارشده)*

# Tests Run

- `make prepush` — سبز، شاملِ `make sdk`
- `go test -tags=integration ./...` — سبز
- `golangci-lint run` — صفر ایراد، حالا بدونِ آن regex ــِ پهن
- gateway ــِ واقعی بالا آمد و هر سه سرویس روی سیم‌اند:

```
notification.v1.CredentialService
notification.v1.NotificationService
notification.v1.WebhookService
```

- ردِ وابستگی‌های ماژولِ SDK اندازه گرفته شد (بالا).

# Follow-ups / Risks

- **`make format` هنوز آن دو `//nolint` را می‌شکند.** هر بار که اجرا شود دوباره
  اتفاق می‌افتد. یا `golines` باید طولانی‌ترشان را تنها بگذارد، یا آن دو خط باید
  طوری بازنویسی شوند که کوتاه باشند. کارِ خودش است.
- **`golines` هرگز روی این مخزن اجرا نشده بود.** یک بازفرمتِ ۳۸ فایلی منتظر است.
  اگر قرار است بخشی از `make format` بماند، باید یک بار جدا اجرا و commit شود —
  نه لای یک تغییرِ دیگر.
- **`CONVENTIONS.md` هنوز قاعدهٔ `sdk/` را ندارد.** قدمِ چهارمِ spec است.
- **هنوز هیچ کدِ SDK ای نیست.** این عمدی است — قدمِ دو کلاینت را می‌آورد.

# Instruction

«یک برنچ بساز و شروع کن SDK را بزن» — و طبق «Order of work» ــِ spec، قدمِ اول
جابه‌جاییِ قرارداد است و بس.
