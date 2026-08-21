Branch: `feat/infra-postgres`

# Summary

اولین قطعهٔ infra نوشته شد: پکیج `internal/infra/database` که pool اتصال به Postgres
را باز می‌کند، سلامتش را جواب می‌دهد و می‌بندد. این پکیج هیچ چیز از srosha نمی‌داند —
نه جدولی می‌شناسد نه query ای — فقط می‌داند چطور به Postgres وصل شود.

تایپ `Postgres` چهار کار می‌کند و بس. `New` فقط config را validate می‌کند و به هیچ
چیزی دست نمی‌زند، تا اشتباه در wiring قبل از اولین dial پیدا شود. `Connect` با
`pgxpool` استخر را باز می‌کند و تا وقتی یک query واقعی جواب نداده برنمی‌گردد.
`Ping` همان نوع query را می‌زند که خود سرویس می‌زند. `Close` استخر را می‌بندد و
دوباره صدا زدنش بی‌خطر است.

`Connect` یک حلقهٔ retry دارد: `ConnectAttempts` بار تلاش، با فاصلهٔ
`ConnectBackoff`. دلیلش فقط یک چیز است — کانتینر Postgres چند ثانیه بعد از بالا
آمدن connection قبول می‌کند. بعد از آن حلقه، خطا واقعی است و کرش کردن جواب صادقانه‌تر
از تلاش بی‌پایان است. حلقه به `ctx` گوش می‌دهد، پس در خاموشی سریع بیرون می‌آید.

`Ping` عمداً `select 1` می‌زند و نه `pool.Ping`. هر دو یک connection می‌گیرند و رفت و
برگشت می‌کنند، ولی `pool.Ping` از simple protocol رد می‌شود و هر query واقعی این
سرویس از extended protocol. پشت یک connection pooler این دو می‌توانند از هم جدا
شوند، و چکی که راه آسان‌تر را می‌رود ممکن است ready بگوید در حالی که اولین query
واقعی شکست بخورد. `Ping` همان چیزی است که readiness endpoint باید صدا بزند.

DSN رمز عبور را با خودش حمل می‌کند، پس هیچ خطایی از این پکیج نباید آن را چاپ کند.
متد خصوصی `redact` هر جای متن خطا که DSN دیده شود آن را برمی‌دارد، و یک تست همین را
تضمین می‌کند.

`Config` هیچ default ای ندارد. هر عدد یک تصمیم عملیاتی است، پس از config خوانده
می‌شود و در یک جا اسم دارد نه دو جا. `validate` همهٔ ایرادها را با هم برمی‌گرداند نه
اولی را. یک نکته: DSN خالی جدا رد می‌شود، چون `pgx` آن را به یک socket محلی پیش‌فرض
تبدیل می‌کند و بعد کل حلقهٔ retry صرف شکست خوردن روی چیزی می‌شود که کسی تنظیمش نکرده.

`settings.DB` هم‌شکل شد با آنچه این پکیج لازم دارد: `MaxLifetime` به
`MaxConnLifetime` تغییر نام داد و پنج کلید تازه اضافه شد — idle time، health check
period، و سه کلید حلقهٔ اتصال.

`maxPoolConns` در `const.go` ماند و به config نرفت. `MaxConns` یک knob است و اپراتور
تصمیمش می‌گیرد؛ `maxPoolConns` سقفِ همان knob است. اگر سقف هم قابل تنظیم بود،
اعتبارسنجی بی‌معنی می‌شد. دلیل فنی‌اش هم هست: میدان `pgxpool` از نوع `int32` است و
عدد بزرگ سرریز می‌کند.

# Files Changed

- `internal/infra/database/postgres.go` *(تازه — `Config`، `validate`، `Postgres` با `New`/`Connect`/`Ping`/`Close`/`Pool` و متدهای خصوصی `waitReady` و `redact`)*
- `internal/infra/database/const.go` *(تازه — `maxPoolConns`)*
- `internal/infra/database/postgres_test.go` *(تازه — ده تست: جدول اعتبارسنجی config، لو نرفتن DSN در خطا، ping قبل از connect، دوبار close، لغو ctx وسط حلقهٔ retry، و اینکه connect ناموفق pool ای جا نمی‌گذارد)*
- `internal/config/settings/db.go` *(پنج کلید تازه، تغییر نام `MaxLifetime`، دو `Check`)*
- `.env.example` *(همان کلیدها با توضیح)*
- `docs/CONFIG.md` *(ردیف db در جدول کلیدها به‌روز شد)*
- `go.mod`, `go.sum` *(`github.com/jackc/pgx/v5`)*
- `internal/core/usecase/submit_test.go` *(فقط قالب‌بندی، طول خط)*

# Tests Run

- `make prepush` — fmt-check، govet، arch-check، golangci-lint و `go test -race ./...` همه پاس

# Follow-ups / Risks

- هنوز هیچ‌کس این pool را نمی‌گیرد. `internal/adapter/db/postgres/` خالی است و
  `internal/bootstrap/` هم. تا وقتی لایهٔ registry نوشته نشود، این پکیج کامپایل می‌شود
  و تست می‌دهد ولی در هیچ باینری‌ای باز نمی‌شود.
- درس `pg_isready` — روی cluster واقعی healthy گزارش می‌داد در حالی که هر connection با
  `role "srosha" does not exist` شکست می‌خورد — جایش در مستندات compose و healthcheck
  کانتینر است، نه در این پکیج. هنوز جایی نوشته نشده.
- خط ۱۰۵ در `docs/CONFIG.md` هنوز می‌گوید config با viper خوانده می‌شود. این از زمان
  نوشتن config به‌صورت دستی غلط است و باید جدا اصلاح شود.
- migration ها هنوز نوشته نشده‌اند. index ها و constraint هایی که قبلاً یادداشت شدند
  — `(status, updated_at)`، `UNIQUE (notification_id, channel, address)` و partial
  unique برای credential پیش‌فرض — سر جای خودشان باقی‌اند.

# Instruction

«برویم infra را بنویسیم، یکی‌یکی، اول postgres.» شرط‌هایی که سر آن توافق شد: این پکیج
باید `Connect`، `Close` و `Ping` داشته باشد، و هیچ عددی نباید داخل کد ثابت باشد — همه
باید از config خوانده شود.
