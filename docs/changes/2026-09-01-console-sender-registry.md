Branch: `feat/credential-trial`

# Summary

console حالا می‌تواند بفرستد — ولی فقط با هویتِ خودِ مشتری، و **هرگز** با هویتِ srosha.
task یک از چهار در plan ــِ `docs/superpowers/plans/2026-08-31-credential-trial.md`.

تا امروز console فقط صفحه سرو می‌کرد و ردیف می‌خواند. `usecase.Credentials` که پورتال
صدایش می‌زند هیچ‌وقت چیزی نفرستاده: ثبت می‌کند، لیست می‌دهد، خاموش و روشن می‌کند. هشت
sender در dispatcher ساخته می‌شوند و console به NATS هم وصل نیست، پس نمی‌تواند کار را به
dispatcher بدهد. یعنی «تست کن» یک handler تازه نیست — یک dependency تازه برای یک باینری
است.

## چرا این کار امن است

دو چیز که از قبل در کد بود این را کوچک‌تر از آنچه به‌نظر می‌رسد می‌کند.

**token factory ها هیچ رازی ندارند.** هر دو، مواد را به‌عنوان آرگومان می‌گیرند — ساختنشان
هیچ‌چیزی از خودِ srosha نمی‌خواهد.

**هویتِ خودِ source و هویتِ پیش‌فرضِ srosha دو مسیرِ جدا در کدند.** `Registry.For` وقتی
source چیزی ثبت کرده باشد `build` را صدا می‌زند، و `ours` را فقط وقتی هیچ ثبت نکرده باشد.
هر شاخهٔ `ours` اولین کارش پرسیدنِ `configured()` است.

پس console یک registry کامل با `Fallback` ــِ **خالی** می‌سازد. می‌تواند به‌جای مشتری
بفرستد؛ نمی‌تواند به‌جای srosha بفرستد، چون هر هشت شاخهٔ fallback به `noSender` می‌رسند.
این مرز را کدی که از قبل نوشته شده نگه می‌دارد، نه قانونی که کسی باید یادش بماند.

## یک انحراف از plan، و چرا

plan گفته بود تستِ مرزی یک registry را **کنارِ** `buildIdentityCore` بسازد، «دقیقاً همان‌طور
که آن می‌سازد». آن تست هرچقدر هم دقیق نوشته شود یک **کپی** را چک می‌کند: روزی که کسی
fallback ــِ واقعی را پر کند، کپی همچنان خالی است و تست سبز می‌ماند — یعنی دقیقاً همان روزی
که باید قرمز شود، نمی‌شود.

به‌جایش خودِ ساختن به یک تابع بیرون کشیده شد:

```go
func consoleSenders(...) (*sender.Registry, error) {
	return sender.NewRegistry(creds, secrets, providers, dialer, tokens, apple, sender.Fallback{})
}
```

و تست **همان تابعی را صدا می‌زند که باینری صدا می‌زند**. حالا پر کردنِ fallback تست را
قرمز می‌کند. این عملاً امتحان شد: با `Fallback{TelegramToken: "x"}` تست قرمز شد و گفت
`the console built a telegram sender with no credential: it can send as srosha`، و بعد
برگردانده شد.

## یک sentinel که نبود

`noSender` هیچ sentinel ای نداشت — فقط یک `errs.InvalidInputErr` با متن. plan این را
پیش‌بینی کرده بود و گفته بود در آن صورت تست فقط `err != nil` را چک کند. ولی آن تست ضعیف
است: اگر registry به دلیلِ کاملاً دیگری خطا بدهد هم سبز می‌ماند، و این مهم‌ترین تستِ کلِ
تغییر است.

پس `sender.ErrNoSender` اضافه شد و `noSender` با `WithErr` آن را حمل می‌کند. حالا تست
می‌گوید «به این دلیلِ مشخص رد شد»، نه «یک‌جوری رد شد» — همان چیزی که `docs/CONVENTIONS.md`
دربارهٔ خطاها می‌خواهد: هرگز با متنِ پیام شناسایی نکن.

# Files Changed

- `internal/adapter/sender/registry.go` *(sentinel ــِ `ErrNoSender`، و `noSender` که حملش می‌کند)*
- `internal/config/console.go` *(فیلدِ `HTTPClient`، بارگذاری‌اش، و doc comment ای که می‌گفت console هیچ sending credential ای ندارد)*
- `internal/bootstrap/console.go` *(`res` تا `buildIdentityCore` رسانده شد، تابعِ `consoleSenders`، و `senderRegistry` روی `consoleCore`)*
- `internal/bootstrap/console_test.go` *(تازه — مرزِ امنیتیِ کلِ feature)*

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass
- `make precommit` — pass (gofmt، golines، vet، arch-check، sqlc، buf lint)
- تستِ مرزی عمداً قرمز دیده شد و بعد برگردانده شد

# Follow-ups / Risks

- `NOTIF_HTTP_CLIENT_*` تا امروز فقط مالِ dispatcher بود و حالا console هم می‌خواندش.
  ثبتش در `docs/CONFIG.md` کارِ task چهار است، نه این.
- `core.senderRegistry` ساخته می‌شود و هنوز هیچ‌کس صدایش نمی‌زند. task دو (`usecase.Trials`)
  مصرف‌کننده‌اش است.

# Instruction

بساز آن دکمه‌ای را که در plan ــِ credential trial نوشته شده: یک تستِ واقعی از صفحهٔ
senders ــِ پورتال. این قدمِ اولش است — console دسترسیِ فرستادن بگیرد، و **فقط** به‌جای
مشتری، نه به‌جای srosha.
