Branch: `feat/infra-layer`

# Summary

چهارمین و آخرین قطعهٔ infra: پکیج `internal/infra/telemetry` که logger سرویس را
می‌سازد.

`NewLogger(cfg Config, out io.Writer) (*slog.Logger, error)` — یک تابع، بدون تایپ
چرخهٔ عمر. `out` از بیرون گرفته می‌شود نه اینکه `os.Stderr` داخلش سفت شود: انتخاب
مقصد log واقعاً کار صدا زننده است، و بدون این پکیج قابل تست نبود.

## چرا از registry رد نمی‌شود

`registry.New(log)` یک logger می‌گیرد. یعنی logger باید قبل از `Resources` وجود
داشته باشد و نمی‌تواند یکی از `step` های همان `Resources` باشد.

و لازم هم نیست: logger نه `Connect` دارد نه `Ping` نه `Close`. پس
`internal/registry/telemetry.go` اصلاً ساخته نشد، و در `bootstrap` این‌طور در
می‌آید:

```
log := telemetry.NewLogger(...)
res := registry.New(log)
db  := registry.Postgres(ctx, cfg.DB, res)
```

telemetry تنها infra ای است که از registry رد نمی‌شود، و دلیلش ترتیب است: بقیه به
آن نیاز دارند.

## slog، نه zerolog

فایل stub آن پوشه `zerolog.go` نام داشت. ولی هر پکیجی که تا الان نوشته شده —
`database`، `messagequeue`، `usecase/submit`، `usecase/dispatch`، `registry` — همه
`*slog.Logger` می‌گیرند. آوردن zerolog یعنی امضای همهٔ آن‌ها عوض شود، برای چیزی که
کتابخانهٔ استاندارد از Go 1.21 دارد. `zerolog.go` حذف شد.

## metrics در این commit نیست

`prometheus.go` دست‌نخورده سر جایش ماند. در `docs/CONVENTIONS.md` نوشته‌ایم «شمردن →
یک metric، نه یک خط log»، پس metrics را می‌خواهیم — ولی یک endpoint می‌خواهد که روی
سرورهایی می‌نشیند که هنوز ننوشته‌ایم، و تصمیم «چه چیزی را بشماریم» هم هنوز گرفته
نشده. registry متریکی که به هیچ endpoint وصل نیست، کدی است که تست نمی‌شود.

## خروجی

سه تصمیم کوچک:

- **قالب** از config: `json` در production که collector پارسش می‌کند، `text` روی
  ترمینال که آدم می‌خواندش.
- **دو صفت روی هر خط**: `service` و `binary`. هر دو باینری به یک collector می‌ریزند
  و بدون `binary` هیچ چیز آنجا از هم جدایشان نمی‌کند.
- **`slog.SetDefault`** صدا زده می‌شود. کتابخانهٔ ثالثی که مستقیم `slog` سراسری را
  صدا می‌زند، یا کدی از خودمان که فراموش کرده logger بگیرد، به همان جا و با همان
  شکل می‌رود — نه به جریان دومی که کسی نگاهش نمی‌کند. یک تست همین را می‌بندد.

به‌علاوهٔ `Source` که فایل و شمارهٔ خط را اضافه می‌کند: موقع دیباگ عالی، در حجم بالا
هزینه‌دار، پس تصمیم اپراتور است نه ما.

## اعتبارسنجی دو بار

`settings.LoadTelemetry` سطح و قالب را چک می‌کند تا پیام خوانا به اپراتور بدهد.
`telemetry.Config.validate` دوباره چکشان می‌کند چون به صدا زننده‌اش اعتماد ندارد —
همان قاعده‌ای که در `docs/CONVENTIONS.md` نوشته شده. مقادیر مجاز در `const.go` یک
بار نام‌گذاری شده‌اند.

# Files Changed

- `internal/infra/telemetry/logger.go` *(تازه — `Config`، `validate`، `NewLogger`، `parseLevel`)*
- `internal/infra/telemetry/const.go` *(تازه — نام سطح‌ها و قالب‌ها)*
- `internal/infra/telemetry/logger_test.go` *(تازه — هفت تست)*
- `internal/infra/telemetry/zerolog.go` *(حذف)*
- `internal/config/settings/telemetry.go` *(دو کلید تازه و اعتبارسنجی قالب)*
- `.env.example`, `docs/CONFIG.md` *(همان کلیدها)*

# Tests Run

- `make prepush` — fmt-check، govet، arch-check، golangci-lint و `go test -race ./...` همه پاس

# Follow-ups / Risks

- `slog.SetDefault` حالت سراسری است. تست‌های این پکیج موازی نیستند و مسئله‌ای پیش
  نمی‌آید، ولی اگر روزی موازی شدند اولین چیزی است که می‌شکند.
- metrics هنوز نوشته نشده. `prometheus.go` سر جایش است و وقتی سرورها آمدند برمی‌گردیم
  سراغش: هم endpoint اش، هم اینکه چه چیزی شمرده شود.
- trace id در context هنوز وجود ندارد، در حالی که `docs/CONVENTIONS.md` می‌گوید
  «تشخیص یک درخواست بین لایه‌ها → trace id در context». آن یک تغییر جداست و به
  interceptor های gRPC وابسته است.
- `Binary` باید از `bootstrap` پر شود، چون تنها جایی است که می‌داند کدام باینری
  اجرا می‌شود. `settings.App` چنین میدانی ندارد و لازم هم ندارد.

# Instruction

«برویم telemetry را بنویسیم» — با چهار شرطی که قبل از نوشتن مطرح و تأیید شد: از
registry رد نشود چون registry خودش به logger نیاز دارد؛ slog به جای zerolog؛ metrics
در این commit نباشد چون endpoint اش هنوز وجود ندارد؛ و خروجی شامل قالب از config،
دو صفت `service` و `binary`، و `slog.SetDefault` باشد.
