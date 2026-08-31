Branch: `feat/admin-panel`

# Summary

مرور نهاییِ کلِ branch. نُه task جدا ساخته و جدا مرور شده بودند؛ چیزهایی که
اینجا درست شد فقط وقتی پیدا می‌شوند که هر نُه‌تا کنار هم بنشینند.

## دو مورد بحرانی

**۱ — `/audit` همان roster ای را نشان می‌داد که از `admin` پنهان کرده بودیم.**

`Gate.Do` روی هر عمل، actor را ثبت می‌کند و actor بیشترِ سطرها **خودِ مشتری**
است: `source.create`، `key.issue`، `key.revoke`، `source.update`. پس
`audit_log.actor_email` پر از آدرسِ مشتری‌هاست و `admin/audit.html` همان ستون را
چاپ می‌کند. `/people` عمداً پشت `super_admin` قفل شده بود تا یک `admin` نفهمد چه
کسی حساب دارد؛ `/audit` همان داده از درِ دیگر بود، و بدتر: owner ULID یک source
را -- که تنها چیزی است که `admin/source.html` نشان می‌دهد -- به یک آدم وصل می‌کرد.

`Operators.Audit` حالا `mayGovernPeople` می‌گیرد و route اش در `NewAdmin` به
گروهِ `top` رفت. لینکِ nav هم داخل `{{if .SuperAdmin}}` رفت تا به `admin` لینکی
نشان ندهیم که فقط redirect می‌شود. **دلیلش در کامنت است**، هم بالای
`Operators.Audit` و هم کنارِ گروهِ `top`، چون از خودِ صفحه پیدا نیست.

**۲ — تستی که ARCHITECTURE.md «اختیاری نیست» می‌نامد، هیچ‌چیز را تست نمی‌کرد.**

`TestNoAdminRouteAnswersOnThePortal` روی `{"/queue", "/people", "/audit"}`
حلقه می‌زد. `pathQueue` برابر `"/"` است، پس `/queue` روی **هیچ‌کدام** از دو surface
وجود ندارد؛ و هفت route ای که واقعاً شکلِ `/sources/:id/...` پرتال را دارند
اصلاً در لیست نبودند.

حالا لیست از **جدولِ route های خودِ engine ادمین** خوانده می‌شود، نه از literal:
`export_test.go` (پکیجِ `web`، فقط داخلِ test binary) `AdminRouteTable` را
می‌دهد، و `admin_test.go` هر route را منهایِ `AdminPathsSharedWithThePortal`
روی handler پرتال صدا می‌زند و ۴۰۴ می‌خواهد. سه مسیرِ `/`، `/sources` و
`/sources/:id` استثنا هستند چون پرتال هم آن‌ها را جواب می‌دهد -- همان رشته،
handler دیگر، engine دیگر، guard دیگر -- و دلیلش همان‌جا نوشته شده.
`AdminOnlyPaths` کفِ کار است: اگر روزی route ای از `NewAdmin` حذف شود، تست
به‌جای اینکه بی‌صدا کوچک شود، fail می‌کند.

خطِ «when web/admin exists» در `docs/ARCHITECTURE.md` هم درست شد. `web/admin`
هیچ‌وقت ساخته نمی‌شود و همین بخش چهار خط بالاتر خودش این را ثبت کرده بود.

## هشت مورد مهم

**۳ — هیچ‌چیز handler را به listener اش گره نمی‌زد.** `NewPortal` و `NewAdmin` هر
دو `http.Handler` می‌دادند و `PortalAddr`/`AdminAddr` هر دو `string` بودند؛ جابه‌جا
کردنشان در `bootstrap.Console` compile می‌شد و پنل ادمین روی پورت عمومی بالا
می‌آمد. حالا `web.PortalHandler` و `web.AdminHandler` دو **struct** جدا هستند
(نه دو interface هم‌شکل، که به هم assign می‌شوند)، `settings.PortalAddr` و
`settings.AdminAddr` دو نوعِ string جدا، و `servePortal`/`serveAdmin` تنها دو
راهِ رسیدن به listener. جابه‌جایی حالا چهار خطا از compiler می‌گیرد.

**۴ — `NOTIF_ADMIN_ADDR` default امن داشت و هیچ guard نداشت.** در production
حالا باید loopback باشد (`127.0.0.1`، `::1` یا `localhost`)، دقیقاً هم‌شکلِ
چکِ `SecureCookie` کنارش. کسی که `:8090` پرتال را کپی کند به `:8092` می‌رسد،
که یعنی همه‌ی interface ها.

**۵ — تستِ masking که spec خواسته بود وجود نداشت.** `memNotifications` و
`memDeliveries` هر دو `nil, nil` می‌دادند، پس `/sources/:id/log` همیشه خالی
render می‌شد. حالا هر دو fake سطرِ واقعی نگه می‌دارند -- با body و با آدرسِ
کامل -- و تست روی **صفحه‌ی render شده** ثابت می‌کند نه body آنجاست نه آدرسِ کامل.

**۶ — `/sources` فیلتر نداشت و دو کامنتِ SQL می‌گفتند دارد.** فیلتر اضافه شد:
query string `state` با چهار حالت (`waiting`, `sending`, `suspended`, `refused`)
و `inState` کنارِ `reviewHandler.list`. کامنتِ `ListAllSources` حالا درست است و
به همان تابع اشاره می‌کند؛ کامنتِ `ListUsers` که می‌گفت «مثل ListAllSources در
handler فیلتر می‌شود» درست شد -- `/people` فیلتر ندارد و دلیلش نوشته شد.

**۷ — guardِ `super_admin` سشنِ یک اپراتورِ سالم را پاک می‌کرد.** `guard` روی هر
رد شدنی cookie را می‌ریخت. دلیلِ آن کامنت -- نگفتنِ وجودِ صفحاتِ ادمین به مشتری --
فقط دربارهٔ قاعده‌ی `operator` است. حالا `may` دو فیلد دارد: `allows` و
`endsSession`. سشنِ مرده همیشه پاک می‌شود؛ نقشِ ناکافی فقط زیرِ `operator`.

**۸ — `Suspend` هیچ guard حالتی نداشت.** روی source ای که هیچ‌وقت تأیید نشده
`is_active=f, approved_at=null, reviewed_at=set, review_note=''` می‌ساخت --
بایت‌به‌بایت همان «ردِ بدونِ دلیل» که `review_note` برای جلوگیری از آن اضافه شد.
`Source.Suspend` حالا error برمی‌گرداند و source تأییدنشده را رد می‌کند.
`Source.Restore` هم همین فکر را گرفت: source ای که هیچ‌کس دربارهٔ آن تصمیم
نگرفته، restore نمی‌شود -- وگرنه اولین تصمیم زیرِ فعلِ `source.restore` ثبت
می‌شد که می‌گوید تصمیمِ دوم بوده.

**۹ — سه متدِ adapter که جانشین داشتند هنوز `is_active` می‌نوشتند.**
`SourceRepository.Update`، `.Deactivate` و `.Activate` هیچ caller ای در
production نداشتند. هر سه با SQL شان حذف شدند و `make sqlc` کد را دوباره زد.
`Activate` مخصوصاً `is_active` را بدون دست‌زدن به `approved_at`/`reviewed_at`
می‌نوشت، یعنی یک حالتِ پنجم. حالا یک source فقط از دو راه حرکت می‌کند:
`UpdateSettings` و `UpdateReview`.

**۱۰ — `audit_log` برای تنها خواننده‌اش index نداشت.** `ListAudit` روی
`at DESC` مرتب می‌کند و هر دو index موجود با `actor_id` و `target_type` شروع
می‌شوند. `CREATE INDEX audit_log_at_idx ON audit_log (at DESC);` به
migration `00011` اضافه شد، با دست روی دیتابیسِ dev اجرا شد، و با migrate کردنِ
یک دیتابیسِ scratch از روی فایل‌ها و diff گرفتن از `pg_indexes` ثابت شد drift
ندارد. scratch بعدش drop شد.

## موردهای کوچک

- **۱۲**: `memSources.ReadByID` pointer ذخیره‌شده را برمی‌گرداند، حالا کپی.
- **۱۳**: کامنتِ پکیج در `web.go` و `portal.go` که هنوز از `web/admin` به‌عنوان
  یک subpackage حرف می‌زدند، و `.env.console.example` که همان‌طور کهنه بود.
- **۱۴**: `pathAdminSources`/`pathAdminSource` و `pageAdminSources`/
  `pageAdminSource` حذف شدند و جایشان همان ثابت‌های پرتال. `pathQueue` حالا
  `= pathHome` تعریف می‌شود، نه رشته‌ای برابرِ آن.
- **۱۶**: `/sources/:id/log` اول خودِ source را می‌خواند، پس id ناموجود ۴۰۴ است.
- **۱۷**: خطای not-found و «دیتابیس جواب نمی‌دهد» دیگر یکی نیستند: اولی ۴۰۴،
  دومی ۵۰۰ با یک خط log.
- **۱۸**: `Operators.Deliveries` حالا `sourceID` هم می‌گیرد و پیامی که مالِ آن
  source نیست را رد می‌کند.

# Files Changed

- `internal/core/usecase/operator_read.go` *(`Audit` به `mayGovernPeople`؛
  `Deliveries` به source محدود شد)*
- `internal/core/usecase/operator_people.go` *(کامنتِ `mayGovernPeople`)*
- `internal/core/usecase/operator.go` *(`Suspend`/`Restore` خطای domain را
  قبل از gate چک می‌کنند)*
- `internal/core/domain/source/entity.go` *(`Suspend` و `Restore` حالا error
  برمی‌گردانند)*
- `internal/core/domain/source/errors.go` *(`ErrNotApproved`, `ErrNotReviewed`)*
- `internal/adapter/api/web/admin.go` *(`pathAudit` به گروهِ `top`؛
  `AdminHandler`)*
- `internal/adapter/api/web/admin_audit.go` *(کامنتِ دلیلِ super_admin)*
- `internal/adapter/api/web/admin_const.go` *(ثابت‌های تکراری حذف؛
  `pathQueue = pathHome`؛ `fieldState` و چهار حالت)*
- `internal/adapter/api/web/admin_review.go` *(فیلترِ حالت؛ `refuseRead`؛
  خواندنِ source در `messages`)*
- `internal/adapter/api/web/session.go` *(`may` دو فیلدی)*
- `internal/adapter/api/web/web.go` *(`PortalHandler`/`AdminHandler`؛ کامنتِ
  پکیج)*
- `internal/adapter/api/web/portal.go` *(کامنتِ فایل؛ `PortalHandler`)*
- `internal/adapter/api/web/export_test.go` *(تازه -- `AdminRouteTable` و دو
  لیست، برای تستِ مرزی)*
- `internal/config/settings/console.go` *(`PortalAddr`/`AdminAddr` نوع‌دار؛
  چکِ loopback در production)*
- `internal/bootstrap/console.go` *(`servePortal`/`serveAdmin`)*
- `internal/adapter/db/postgres/source.go` *(`Update`, `Deactivate`,
  `Activate` حذف)*
- `internal/adapter/db/postgres/queries/source.sql` *(سه statement حذف؛
  کامنتِ `ListAllSources` درست شد)*
- `internal/adapter/db/postgres/queries/user.sql` *(کامنتِ `ListUsers` درست شد)*
- `internal/adapter/db/postgres/gen/*` *(`make sqlc`)*
- `migrations/00011_create_audit_log.sql` *(`audit_log_at_idx`)*
- `public/templates/admin/layout.html` *(لینکِ Audit زیرِ `SuperAdmin`)*
- `public/templates/admin/sources.html` *(لینک‌های فیلتر)*
- `docs/ARCHITECTURE.md`, `docs/CONFIG.md`, `.env.console.example`,
  `docs/superpowers/specs/2026-08-30-admin-panel-design.md` *(متن)*
- تست‌ها: `admin_test.go`, `session_test.go`, `portal_test.go`,
  `operator_test.go`, `operator_read_test.go`, `service_test.go`,
  `config_test.go`, `source_test.go`, `apikey_test.go`

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass
- `go test -count=1 -tags=integration ./internal/adapter/db/postgres/` — pass
- `make prepush` — pass

هر Critical و Important عمداً شکسته شد و تستِ مربوطه fail شد، بعد برگردانده شد.
جزئیاتش در `.superpowers/sdd/2026-08-30-admin-panel/final-fix-report.md`.

# Follow-ups / Risks

- `MaxOperatorMessages` و `MaxOperatorAudit` دست نخوردند؛ تصمیمِ جداگانه‌ای است
  که منتظرِ صاحبِ repo است.
- `Deliveries` حالا یک `ReadByID` اضافه روی `notifications` می‌زند تا مالکیت را
  چک کند. یک query بیشتر روی صفحه‌ای که اپراتور دستی باز می‌کند.
- فیلترِ `/sources` در handler است نه در SQL. تا وقتی تعدادِ source ها را یک
  نفر با چشم می‌خواند درست است؛ کامنتِ `ListAllSources` می‌گوید روزی که نباشد
  کجا باید عوض شود.

# Instruction

موجِ آخرِ اصلاح بعد از مرورِ کلِ branch: فهرستِ یافته‌ها به ترتیبِ اولویت
(دو Critical، هشت Important، بعد Minor ها) یکی‌یکی درست شود، هر کدام بلافاصله
تأیید شود، و برای هر Critical و Important عمداً کد شکسته شود تا دیده شود تستِ
مربوطه واقعاً fail می‌کند. commit ممنوع؛ همه‌چیز در working tree می‌ماند.
