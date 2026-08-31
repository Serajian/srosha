Branch: `feat/admin-panel`

# Summary

Task 8 از طرحِ admin panel: سومین listener. تا اینجا `web.NewAdmin` فقط از
`admin_test.go` صدا زده می‌شد و هیچ‌کس آن را روی یک پورت سرو نمی‌کرد -- این
تغییر همان چیزی است که یک پکیج کد را به پنلی تبدیل می‌کند که کسی می‌تواند بازش
کند.

`internal/bootstrap/console.go` حالا `usecase.Operators` را می‌سازد --
`consoleCore` یک فیلدِ `operators *usecase.Operators` گرفت، و `buildConsoleCore`
بعد از `buildIdentityCore` آن را با پنج repository (`source`, `user`,
`notification`, `delivery`, `credential`)، همان `auditRows` و همان `gate` ای که
از قبل برای Sign-in/Sources/Keys ساخته شده بودند، و همان `now` می‌سازد -- هیچ‌کدام
تازه باز نشدند، فقط یک نمونه‌ی تازه از یک wrapper رویِ همان pool، همان‌طور که
`sourceRows` از قبل دو بار در همین فایل ساخته می‌شود.

`Console` سه listener راه می‌اندازد به‌جای دو تا: `portal` (پابلیک)، `health`
(پرایوت) و حالا `admin` روی `cfg.Console.AdminAddr` -- که پیشِ‌فرضش
`127.0.0.1:8092` است، نه `:8092`. این فرق مهم است: `docs/ARCHITECTURE.md` فقط
یک چیز درباره‌ی این پورت گفته -- هیچ‌وقت publish نمی‌شود -- و bind کردنش روی
loopback باعث می‌شود این یک fact درباره‌ی خودِ process باشد، نه فقط یک تصمیمِ
deploy که یک خط جا‌به‌جایی می‌تواند بی‌سروصدا خرابش کند. `web.AdminDeps.validate`
از Task 7 آماده بود (رد می‌کند اگر `Operators` نیل باشد)، پس اینجا کارِ تازه‌ای
لازم نبود.

کلیدِ محیطیِ تازه `NOTIF_ADMIN_ADDR` است، نه چیزی که brief پیشنهاد داده بود.
`docs/CONFIG.md` از قبل (در تصمیمِ معماریِ همین طرح) نوشته بود
«`NOTIF_ADMIN_ADDR` joins the second group in phase 2» -- یعنی کنارِ
`NOTIF_PORTAL_ADDR` در گروهِ «آدرسِ یک surface»، نه کنارِ `NOTIF_CONSOLE_*` که
گروهِ خودِ باینری است. همین قاعده دنبال شد: یک ردیفِ تازه‌ی `admin` در جدولِ
Application configuration، و پروزِ همان بخش به‌روز شد چون دیگر «phase 2» نیست.
پورت خودش (۸۰۹۲) از قبل در جدولِ Services and ports بود و دوباره اضافه نشد.

# Files Changed

- `internal/config/settings/console.go` *(فیلدِ `AdminAddr`، خواندنش با
  `r.Str("ADMIN_ADDR", "127.0.0.1:8092")`)*
- `internal/bootstrap/console.go` *(`consoleCore.operators`، ساختنش در
  `buildConsoleCore`، `web.NewAdmin`، listenerِ سوم با `registry.HTTPServer`،
  اضافه‌شدنش به `watch(...)`)*
- `.env.console.example` *(`NOTIF_ADMIN_ADDR=127.0.0.1:8092` کنارِ
  `NOTIF_HTTP_ADDR`)*
- `docs/CONFIG.md` *(ردیفِ `admin | NOTIF_ADMIN_ADDR` در جدولِ Application
  configuration، پروزِ زیرِ آن به‌روز شد، یک ردیفِ `console admin` در جدولِ
  Local development)*

# Tests Run

- `go build ./...` -- سبز
- `go test -count=1 ./...` -- سبز، همه‌ی پکیج‌ها
- `make prepush` -- سبز (fmt، vet، arch-check، sqlc-check، buf lint،
  golangci-lint، race tests، `sdk`)
- `make run-console` بالا آمد و لاگ نشان داد هر سه listener گوش می‌دهند:
  `[::]:8090`، `[::]:8091`، `127.0.0.1:8092`.
- `curl http://127.0.0.1:8092/signin` -- ۲۰۰، صفحه‌ی sign-in همان admin.
- `curl http://127.0.0.1:8090/queue` -- ۴۰۴ (پرتال این route را نمی‌شناسد).
- `curl http://127.0.0.1:8090/signin` و `http://127.0.0.1:8091/healthz` -- ۲۰۰،
  sanity.
- `lsof -nP -iTCP -sTCP:LISTEN` -- `8090`/`8091` روی `*` (همه‌ی interfaceها)،
  `8092` فقط روی `127.0.0.1`.
- `curl http://<IPِ لن>:8092/signin` -- connection refused (exit 7)، از همان
  ماشین امتحان شد چون بایندینگِ loopback خودش باعث می‌شود از بیرون هم قابلِ
  دسترسی نباشد؛ تست از یک ماشینِ دیگر روی شبکه انجام نشد.
- باینری بعد از تست با `kill` متوقف شد.

# Follow-ups / Risks

- `MaxOperatorMessages` و `MaxOperatorAudit` در `internal/core/usecase/const.go`
  دست نخوردند، طبقِ خواسته -- این یک تصمیمِ config جداست که owner هنوز تاییدش
  نکرده.
- هیچ تغییری در `internal/bootstrap/const.go` لازم نبود: default آدرس مثلِ
  `PortalAddr` همان‌جا در `settings/console.go` inline نوشته شد، نه یک ثابتِ
  جدا -- همان الگویی که `PortalAddr` از قبل داشت.

# Instruction

اجرای Task 8 از طرحِ admin panel طبقِ
`.superpowers/sdd/2026-08-30-admin-panel/task-8-brief.md`، با یک تصحیح پیش از
شروع: پورتِ ۸۰۹۲ از قبل در `docs/CONFIG.md` بود و دوباره اضافه نشد؛ فقط کلیدِ
محیطی‌ای که آن را می‌خواند نوشته شد. `internal/bootstrap/console.go` باید
`usecase.Operators` را بسازد -- با استفاده‌ی دوباره از repositoryها و
gate/audit/clock ای که برای پرتال از قبل ساخته شده بودند -- و یک listenerِ
سوم برای `web.NewAdmin` راه بیندازد، bind شده روی `127.0.0.1` نه `0.0.0.0`.
دست‌زدن به `internal/core/usecase/const.go` و دو ثابتِ عملیاتیِ آن ممنوع بود.
در پایان باید `go build`، `go test -count=1 ./...` و `make prepush` سبز باشند،
و `make run-console` واقعاً بالا بیاید و با `curl`/`lsof` تایید شود که `:8092`
جواب می‌دهد، `:8090` رویِ یک مسیرِ ادمین ۴۰۴ می‌دهد، و `:8092` فقط رویِ
loopback گوش می‌دهد.
