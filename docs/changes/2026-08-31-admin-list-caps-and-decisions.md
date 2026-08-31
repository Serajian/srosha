Branch: `feat/admin-panel`

# Summary

دو تغییر روی پنل ادمین، هر دو حول یک ایده: چیزی که نصفه نشان داده می‌شود باید
بگوید که نصفه است.

## یک — هر پنجِ لیستِ اپراتور، از یک کلید کانفیگ

پنل پنج خواندنِ لیستی دارد: `Queue`، `AllSources`، `Messages`، `People` و
`Audit`. دوتاشان (`Messages`، `Audit`) سقفِ ثابتِ `MaxOperatorMessages` و
`MaxOperatorAudit` را در کد داشتند؛ سه‌تای دیگر هیچ سقفی نداشتند --
`ListAllSources` مثلاً `SELECT * FROM sources ORDER BY created_at DESC` بدون
`LIMIT` بود، روی همان connection pool که پورتال مشتری هم استفاده می‌کند.

طبق قاعده‌ی STRICT در `docs/CONVENTIONS.md` («اگر تغییرِ این عدد یک تصمیمِ
عملیاتی است، این عدد کانفیگ است»)، هر پنج تا الان از یک کلید می‌خوانند:
`NOTIF_ADMIN_LIST_LIMIT`، پیش‌فرض ۲۰۰، کنار `NOTIF_ADMIN_ADDR` در گروهِ
`NOTIF_ADMIN_*`. هر دو ثابتِ قدیمی حذف شدند. جایش:

- `settings.Console.AdminListLimit int32`، در `internal/config/settings/console.go`،
  با `r.Check(c.AdminListLimit > 0, ...)` -- بارگذاری با سقفِ صفر یا منفی رد
  می‌شود، تا سقفِ صفر هرگز بی‌سروصدا به معنیِ «هیچ ردیف» نشود.
- `usecase.Operators` یک فیلدِ تازه گرفت (`listLimit int32`) و `NewOperators`
  یک پارامترِ تازه، از انتها.
- `Queue`، `AllSources`، `Messages`، `People` و `Audit` همه یک امضای تازه
  دارند: `(rows, truncated bool, error)`. هر کدام از repository اش
  `listLimit+1` ردیف می‌خواهد؛ گرفتنِ بیش از `listLimit` ردیف خودش جوابِ
  «نصفه بود یا نه» است -- تابعِ کمکیِ `truncate[T any]` در `operator.go`.
  ارزان‌تر از یک `COUNT` جدا، و هیچ‌وقت با چیزی که واقعاً نشان داده می‌شود
  ناهم‌خوان نیست، چون همان slice که برش می‌خورد همان چیزی است که رندر می‌شود.
- سمتِ adapter: `source.Repository.ListForReview`/`ListAll`،
  `user.Repository.List` هرکدام یک پارامترِ `limit int32` گرفتند؛ کوئری‌های
  متناظر در `queries/source.sql` و `queries/user.sql` یک `LIMIT @row_limit`
  گرفتند؛ `make sqlc` کد را دوباره زد.
- صفحه‌ای که سقف را رد کرده باشد، این را می‌گوید: یک partial مشترک
  (`{{define "truncated"}}`) در `layout.html`، صدا زده‌شده از `queue.html`،
  `sources.html`، `log.html`، `people.html` و `audit.html` -- «چیزی که اینجا
  می‌بینید همه‌اش نیست، و اگر بقیه‌اش را لازم دارید باید مستقیم از دیتابیس
  خوانده شود» -- بدون pagination، چون این کار بزرگ‌تر است و قرار نبود اینجا
  ساخته شود.

## دو — تاریخچه‌ی تصمیم‌های یک source، روی صفحه‌ی خودش

`audit_log` هر تصمیمِ اپراتور را (`source.approve/refuse/suspend/restore`)
ثبت می‌کند، اما هیچ‌جا به‌ازای یک source خوانده نمی‌شد -- فقط فید سراسریِ
`/audit` بود، که هم `super_admin`-only است، هم همه‌ی مشتری‌ها را قاطی نشان
می‌دهد.

`/sources/:id` حالا زیرِ تصمیم‌های امروزش («Decisions») این تاریخچه را دارد.

نکته‌ی اصلی این تکه، فیلترِ verb است -- و این فیلتر یک مرزِ حریمِ خصوصی است، نه
یک راحتی. `/audit` فقط برای `super_admin` است چون `actor_email` روی یک ردیفِ
`source.create` یا `source.update` آدرسِ **مشتری** است. اما آن چهار verb
(approve/refuse/suspend/restore) همیشه actor‌شان یک **اپراتور** است -- یک
همکار، نه مشتری. همین است که اجازه می‌دهد `SourceHistory` زیرِ `mayOperate`
باشد، نه `mayGovernPeople`، و روی صفحه‌ای که یک `admin` هم می‌بیند.

پیاده‌سازی:

- `usecase.AuditLog` (در `gate.go`) یک متدِ تازه گرفت:
  `ListByTarget(ctx, targetType, targetID string, verbs []string, limit int32)`.
- `usecase.sourceDecisionVerbs` در `const.go` -- یک `var []string` ساخته‌شده
  از همان چهار ثابتِ `ActSource*`، نه رشته‌های تازه، با یک کامنتِ بلند دقیقاً
  همان‌جا: این چهار تا عمداً named-included هستند، نه «همه منهای create/update»
  -- چون verb بعدی که به این فایل اضافه شود (مثلاً `key.issue`، که actor اش
  هم مشتری است) باید صریحاً رد شود، نه این‌که پیش‌فرض راه پیدا کند.
- `Operators.SourceHistory` (در `operator_read.go`) همان الگوی سقف/truncate
  بالا را دارد، `mayOperate` را چک می‌کند، و `sourceDecisionVerbs` را به
  `ListByTarget` می‌دهد.
- سمتِ adapter: `queries/audit.sql` یک `ListAuditByTarget` گرفت
  (`WHERE target_type = ... AND target_id = ... AND verb = ANY(@verbs::text[])
  ORDER BY at DESC LIMIT @row_limit`) -- روی همان ایندکسِ موجودِ
  `audit_log_target_idx (target_type, target_id, at DESC)`، بدون ایندکسِ
  تازه. `AuditRepository.ListByTarget` در `audit.go` نگاشتش می‌کند.
- `admin_review.go` -> `reviewHandler.show`/`showWith` حالا `History` و
  `HistoryTruncated` را روی `adminSourcePage` می‌گذارند؛ `source.html` یک
  بخشِ «Decisions» زیرِ فرم‌های approve/refuse/suspend/restore دارد -- جدول
  اگر تاریخچه‌ای هست، جمله‌ی «هنوز تصمیمی ثبت نشده» اگر نیست.

# Files Changed

- `internal/config/settings/console.go` *(`AdminListLimit`، چکِ بالای صفر)*
- `.env.console.example`, `docs/CONFIG.md` *(کلیدِ `NOTIF_ADMIN_LIST_LIMIT`)*
- `internal/core/usecase/const.go` *(حذفِ دو ثابتِ قدیمی، `sourceDecisionVerbs`)*
- `internal/core/usecase/gate.go` *(`AuditLog.ListByTarget`)*
- `internal/core/usecase/operator.go` *(`listLimit`، `truncate[T]`، `Queue`/`AllSources`)*
- `internal/core/usecase/operator_read.go` *(`Messages`، `Audit`، `SourceHistory`)*
- `internal/core/usecase/operator_people.go` *(`People`)*
- `internal/core/domain/source/port.go`, `internal/core/domain/user/port.go` *(پارامترِ `limit`)*
- `internal/adapter/db/postgres/{source,user,audit}.go` و
  `queries/{source,user,audit}.sql` و `gen/*.sql.go` *(`LIMIT`، `ListAuditByTarget`، `make sqlc`)*
- `internal/bootstrap/console.go` *(`cfg.Console.AdminListLimit` به `NewOperators`)*
- `internal/adapter/api/web/admin.go` *(امضای تازه‌ی `Operators`، `SourceHistory`)*
- `internal/adapter/api/web/admin_review.go`, `admin_people.go`, `admin_audit.go` *(`Truncated`، `History`)*
- `public/templates/admin/{layout,queue,sources,log,people,audit,source}.html` *(اعلانِ truncation، بخشِ Decisions)*
- تست‌ها: `internal/core/usecase/{fakes_test,gate_test,operator_test,operator_people_test,operator_read_test,operator_listlimit_test}.go`,
  `internal/adapter/api/web/{portal_test,admin_test}.go`,
  `internal/adapter/db/postgres/{source_test,user_test,audit_test}.go`,
  `internal/core/domain/source/service_test.go`, `internal/config/config_test.go`

# Tests Run

- `go build ./...` -- سبز
- `go vet ./...` و `go vet -tags=integration ./...` -- سبز
- `go test -count=1 ./...` -- سبز
- `go test -count=1 -tags=integration ./...` (با `make dev-up` از قبل بالا) -- سبز
- `go test -count=1 -tags=integration ./internal/adapter/db/postgres/` -- سبز
- `make prepush` -- سبز (fmt، vet، arch-check، sqlc-check، buf lint، golangci-lint، race tests، sdk)

## شکستن‌های عمدی، برای اثباتِ این‌که گاردها واقعی‌اند

- **فیلترِ verb**: `sourceDecisionVerbs` را موقتاً به `ActSourceCreate` هم
  عریض کردم. `TestSourceHistoryFiltersToOperatorVerbsAndNeverLeaksACustomerAddress`
  قرمز شد: `got 2 rows, want 1 (the approve, not the customer's create)` --
  آدرسِ مشتری (`customer@acme.test`) واقعاً روی نتیجه ظاهر شد. برگرداندم،
  دوباره سبز شد.
- **سقف‌ها**: `truncate` را طوری شکستم که همیشه `false` برگرداند (بدونِ برش).
  پنج تست (`TestQueueAndAllSourcesSayWhenTheyAreTruncated`،
  `TestMessagesSaysWhenItIsTruncated`، `TestPeopleSaysWhenItIsTruncated`،
  `TestAuditSaysWhenItIsTruncated`، `TestSourceHistorySaysWhenItIsTruncated`)
  همه قرمز شدند -- هم تعدادِ ردیف‌ها بیش از سقف بود، هم `truncated` دروغ
  می‌گفت. برگرداندم، سبز شدند.
- **کانفیگ**: چکِ `r.Check(c.AdminListLimit > 0, ...)` را موقتاً برداشتم.
  `TestAdminListLimitMustBeAboveZero` قرمز شد -- کنسول با
  `NOTIF_ADMIN_LIST_LIMIT=0` بی‌سروصدا بالا آمد. برگرداندم، سبز شد. این همان
  چیزی است که «سقفِ صفر که هیچ ردیفی برنگرداند یک شکستِ خیلی ساکت است» در
  دستور اصلی می‌گفت.
- علاوه بر این‌ها، سطحِ web هم یک تستِ end-to-end دارد
  (`TestASourcesOwnHistoryNeverShowsACustomerAddress` و
  `TestATruncatedListingSaysSoOnThePage` در `admin_test.go`) که همان دو گارد
  را از پشتِ HTTP، روی صفحه‌ی رندرشده، چک می‌کند -- نه فقط روی usecase.

# Follow-ups / Risks

- **Pagination ساخته نشد، عمداً.** `shared.Pagination` هست و جوابِ درستِ
  بلندمدت است، اما این تسک آن را نمی‌خواست؛ سقفی که صادقانه می‌گوید سقف است
  کوچک و کافی بود.
- **پیامِ truncation یک متنِ عمومی است**، نه شامل عددِ دقیقِ سقف یا این‌که چند
  ردیفِ دیگر هست -- چون آن عدد را لایه‌ی web امروز جداگانه نمی‌داند (فقط
  `bool` را از usecase می‌گیرد) و اضافه‌کردنش کارِ کوچکی است اگر لازم شد.
- **هیچ ایندکسِ تازه‌ای اضافه نشد.** `ListAuditByTarget` از
  `audit_log_target_idx` موجود استفاده می‌کند؛ اگر verb selectivity کم شد
  (خیلی از سطرهای یک source این چهار verb را داشته باشند) این جای بازبینی
  است، اما امروز نیست.

# Instruction

دو تغییرِ مستقل روی پنل ادمین، به‌عنوانِ یک دستور: (۱) هر پنج خواندنِ لیستیِ
اپراتور (Queue، AllSources، Messages، People، Audit) از یک کلیدِ کانفیگِ
واحد (`NOTIF_ADMIN_LIST_LIMIT`، پیش‌فرض ۲۰۰، کنارِ `NOTIF_ADMIN_ADDR`) سقف
بگیرند و وقتی سقف را رد کردند رویِ صفحه بگویند که نصفه‌اند -- بدون
pagination. (۲) تاریخچه‌ی تصمیم‌های یک source از `audit_log` زیرِ
`/sources/:id` نشان داده شود، فیلترشده روی چهار verbِ عملیاتی
(approve/refuse/suspend/restore) چون actor آن‌ها همیشه اپراتور است نه
مشتری -- همان مرزی که `/audit` را `super_admin`-only نگه می‌دارد، اینجا هم
باید رعایت شود، با کامنتی که این استدلال را کنارِ خودِ فیلتر نگه دارد.
