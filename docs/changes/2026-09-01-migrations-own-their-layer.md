Branch: `feat/migrate-with-lock`

# Summary

migration از یک دستورِ دستی به دروازه‌ای تبدیل شد که هر deploy از آن می‌گذرد،
با قفل — و بعد از سؤالِ مالک، کد به لایه‌ای که مالِ آن است منتقل شد و readiness
هم یاد گرفت schema را ببیند.

# آنچه امروز روی سرور اتفاق افتاد و این کار از آن آمد

سه سرویس `healthy` بودند روی دیتابیسی که **یک جدول نداشت**. migration اجرا
نشده بود و هیچ‌چیز نگفت. `Ping` فقط `select 1` می‌زند: ثابت می‌کند اتصال و نقش
و اجازهٔ query سالم‌اند، و دربارهٔ schema هیچ نمی‌گوید.

# ۱. دروازه

`profiles: ["migrate"]` رفت و جایش:

```yaml
depends_on:
  migrate:
    condition: service_completed_successfully
```

هر deploy می‌شود **build → migrate → بالا آمدن**. و اگر migration بشکند،
container های در حالِ اجرا **جایگزین نمی‌شوند** — یعنی release ــِ خراب رد
می‌شود و نسخهٔ قبلی سرو می‌کند. قبلاً برعکس بود.

# ۲. قفل، و چرا یک باینری لازم شد

مالک قفل خواست. goose ــِ CLI قفل ندارد — فهرستِ پرچم‌هایش را از خودِ باینری
پرسیدم و `-lock-mode` در آن نیست. قفلِ advisory فقط در **کتابخانه‌ی** goose
هست.

پس زنجیره این بود: قفل → CLI نمی‌تواند → goose باید کتابخانه شود → یک باینری.

`cmd/migrate` با ۳۸ خط. و چیزی را پس داد که با حذفِ CLI از دست رفته بود:
`migrate status`.

**قفل واقعاً تست شد:** یک session با `pg_advisory_lock(4097083626)` قفل را
گرفت، migrate **ده ثانیه صبر کرد** تا رها شود، بعد اعمال کرد. اولین تلاشم
عددِ غلطی قفل کرده بود و صفر ثانیه طول کشید — یعنی آن تست هیچ چیزی ثابت نمی‌کرد
تا وقتی `DefaultLockID` را از کدِ goose خواندم. و goose وقتی چیزی برای اعمال
نیست اصلاً قفل نمی‌گیرد، پس تست به یک migration ــِ واقعی نیاز داشت.

# ۳. سؤالِ مالک، که دو چیز را عوض کرد

> «چرا یک باینریِ جدا؟ postgres بدونِ migration که بالا بیاید چه معنی دارد؟»

**جای کد غلط بود.** `goose.NewProvider` را در `bootstrap` گذاشته بودم. bootstrap
جای سیم‌کشی است نه جای دانستنِ اینکه goose چیست. ولی خانه‌اش `postgres.go` هم
نبود — خطِ اولِ همان فایل می‌گوید «knows nothing about what this service stores
there»، و migration دقیقاً همان است. پکیجِ سوم ساخته شد:

```
migrations/                    فایل‌های sql، با go:embed
internal/infra/migrations/     goose. یک *sql.DB و یک fs.FS می‌گیرد
internal/registry/             بازش می‌کند
internal/bootstrap/            فقط وصلش می‌کند
```

**sql حالا داخلِ باینری است.** `go:embed`، مثلِ `public/`. دیگر از Dockerfile
کپی نمی‌شود و `NOTIF_MIGRATION_DIR` حذف شد. نسخه‌ای که یک باینری انتظار دارد،
حقیقتی دربارهٔ خودِ باینری است نه دربارهٔ پوشه‌ای که کسی یادش بود کپی کند.

**و readiness حالا می‌پرسد.** `migrations.EnsureCurrent` بیشترین
`version_id` ــِ اعمال‌شده را با آنچه این build انتظار دارد مقایسه می‌کند، و در
`/readyz` یک checkـِ **جدا** با نامِ خودش است:

```json
{"name":"postgres","status":"up"},
{"name":"schema","status":"down"}
```

دو نام، عمداً: «می‌رسم» و «جدول‌ها هستند» دو سؤال‌اند و امروز جوابشان فرق داشت.

**حداقل، نه دقیقاً برابر.** دیتابیسی که جلوتر از باینری است رد نمی‌شود — آن
حالتِ عادیِ وسطِ یک انتشار است و رد کردنش یک deploy عادی را به قطعی تبدیل
می‌کرد.

# Files Changed

- `migrations/embed.go`, `migrations/embed_test.go` *(جدید — embed و `Latest()`)*
- `internal/infra/migrations/migrations.go` *(جدید — مالکِ goose)*
- `internal/infra/migrations/schema.go` *(جدید — `EnsureCurrent`)*
- `internal/infra/database/migrations.go` *(جدید — `OpenSQL`، تک‌اتصالی)*
- `internal/registry/migrate.go` *(جدید)*، `internal/registry/db.go` *(چکِ schema)*
- `internal/bootstrap/migrate.go` *(جدید)*، `internal/bootstrap/const.go`
- `internal/config/migrate.go`, `internal/config/settings/migration.go` *(جدید)*
- `cmd/migrate/main.go` *(جدید)*
- `deployment/app/Dockerfile` *(باینریِ چهارم؛ goose ــِ CLI و کپیِ sql حذف)*
- `deployment/app/docker-compose.yml` *(دروازه)*
- `Makefile` *(`migrate-server` و `migrate-server-status`)*
- `go.mod`, `go.sum` *(goose به‌عنوانِ وابستگیِ runtime)*

# Tests Run

- `go build ./...`, `go test -count=1 ./...` — بدون شکست
- `make precommit` — pass، شاملِ arch-check
- **قفل**، مقابلِ postgres ــِ واقعی: با قفلِ گرفته‌شده ۱۰ ثانیه صبر کرد؛ بدونش صفر
- **`status`** روی دیتابیسِ dev: یازده خطِ `applied` با تاریخِ واقعی
- **چکِ schema**: یک دیتابیسِ خالی ساختم، dispatcher را به آن وصل کردم:
  `HTTP 503` با `postgres: up` و `schema: down`. بعد پاکش کردم و تأیید کردم
  دیتابیسِ dev دست‌نخورده است.
- `docker build` و اجرای `/app/migrate status` داخلِ image

# Follow-ups / Risks

- **`OpenSQL` تک‌اتصالی است و باید بماند.** قفلِ session مالِ همان اتصالی است
  که گرفتش؛ با یک pool، goose می‌تواند روی یک اتصال قفل بگیرد و روی اتصالِ
  دیگری migration بزند — قفلی که از هیچ محافظت نمی‌کند.
- goose حالا وابستگیِ **runtime** است نه ابزار. `make setup-dev` هنوز CLI را
  برای کارِ محلی نصب می‌کند و آن `@latest` است؛ دو نسخهٔ متفاوت می‌توانند از هم
  جدا بیفتند. هر دو همان جدولِ `goose_db_version` را می‌خوانند، پس خطرش کم است.
- `docs/CONFIG.md` هنوز مکانیزمِ profile و pin ــِ goose را ثبت کرده و باید
  به‌روز شود. در همین branch انجام می‌شود.

# Instruction

مالک گفت migration نباید هر بار دستی زده شود؛ گزینهٔ A (دروازه در compose) را
با قفل خواست. بعد پرسید چرا باینریِ جدا و چرا connect شدنِ postgres بدونِ
migration معنی دارد — که به انتقالِ کد به `internal/infra/migrations` و افزودنِ
چکِ schema به readiness رسید.
