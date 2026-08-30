Branch: `feat/portal-spec`

# Summary

دو حفره‌ای که در جوابِ «پورتال تمام شد؟» پیدا شده بودند بسته شدند. هر دو زنجیره
بودند با حلقه‌های گم‌شده، ولی **در جای متفاوتی پاره بودند** — و همان تفاوت،
اندازهٔ کار را تعیین کرد.

```
ویرایشِ source          credential
──────────────          ──────────
صفحه       ✗            صفحه       ✗   ← فقط اینجا
use case   ✗            use case   ✓
service    ✗            service    ✓
port       ✗ ← اینجا    port       ✓
adapter    ✓            adapter    ✓
SQL        ✓            SQL        ✓
```

`UpdateSource` و متدِ adapter از ۲۲ آگوست بودند و هیچ interface ای آشکارشان
نکرده بود. `Credentials.Deactivate` و `Activate` هم بودند و **هیچ صداکننده‌ای
نداشتند** — نه پورتال، نه gRPC.

## قفل در خودِ statement است، نه در use case

`UpdateSource` ــِ موجود `max_priority` و `allow_custom_address` را هم می‌نویسد.
اگر پورتال از همان استفاده می‌کرد، هر تغییرِ نام سقفِ مشتری را هم حمل می‌کرد —
درست، تا وقتی که use case ــِ بالایش یادش بماند مقدارِ فعلی را بخواند و
پس‌بفرستد، و غلط، اولین باری که کسی آن use case را ویرایش کند.

پس یک query جدا:

```sql
UPDATE sources
SET name = @name, description = @description, default_addresses = @default_addresses
```

statement ای که آن ستون‌ها را **نمی‌تواند نام ببرد**. این ضمانتِ ارزان‌تری است از
use case ای که باید یادش بماند.

همین منطق دو بار دیگر هم تکرار شده: `SourceSettings` فیلدی برای آن‌ها ندارد
(پس handler ای که تلاش کند کامپایل نمی‌شود)، و `Source.Reconfigure` پارامتری
برایشان ندارد.

## چیزی که وسطِ کار عوض شد

اول یک `Service.Reconfigure` نوشتم. بعد دیدم `Sources` مثلِ `Register` مستقیم با
repository کار می‌کند نه با service — یعنی آن متد **هیچ صداکننده‌ای نمی‌گرفت**.
دقیقاً همان چیزی که این تغییر برای رفعش نوشته شده بود.

پس برداشته شد. اعتبارسنجی روی خودِ entity ماند (`Source.Reconfigure`) و تست‌ها
هم به همان‌جا منتقل شدند.

## ویرایش یا همه‌اش یا هیچ‌کدام

`Reconfigure` قبل از نوشتنِ هر چیزی همه‌چیز را اعتبارسنجی می‌کند. اگر آدرس خراب
باشد، نام **عوض نشده** باقی می‌ماند — وگرنه مشتری آدرس را درست می‌کند و هرگز
نمی‌فهمد تغییرِ نام جداگانه رفته بوده.

## description در همان جدولی است که ساخته می‌شود

نبود و اضافه شد. اول به‌شکلِ یک migration جدا نوشتمش؛ مالک گفت لازم نیست، چون
سرویس هنوز بالا نرفته.

درست است: migration ای که کسی اجرایش نکرده، قدمی را در تاریخی نگه می‌دارد که
ناظری ندارد — و باعث می‌شود شکلِ جدول فقط با خواندنِ دو فایل معلوم شود. پس ستون
رفت داخلِ `00003_create_sources.sql`، کنارِ بقیه.

`NOT NULL DEFAULT ''` نه nullable: توصیفِ خالی و توصیفِ نبوده برای هر خواننده‌ای
یک چیزند.

## فرمِ ویرایش کانال‌ها را از خودِ سرویس می‌گیرد

`shared.AllChannels()` را در view model می‌گذارد، نه هفت `<option>` دستی. کانالی
که به shared اضافه شود و به یک فهرستِ دستی نه، کانالی است که مشتری نمی‌تواند
پیکربندی‌اش کند و هیچ‌چیز هم نمی‌گوید.

# Files Changed

- `migrations/00003_create_sources.sql` *(ستونِ `description`)*
- `internal/core/domain/source/entity.go` *(`Description`، `Reconfigure`)*, `const.go`, `port.go`
- `internal/core/domain/source/service_test.go`
- `internal/adapter/db/postgres/queries/source.sql` *(`UpdateSourceSettings`)*, `source.go`
- `internal/adapter/db/postgres/source_test.go`
- `internal/core/usecase/source.go` *(`SourceSettings`، `Update`)*, `const.go`, `source_test.go`, `fakes_test.go`
- `internal/adapter/api/web/portal_source.go`, `portal_identity.go`, `portal_const.go`, `portal.go`, `portal_test.go`
- `public/templates/portal/source_edit.html` *(new)*, `source.html`, `senders.html`
- `docs/superpowers/specs/2026-08-28-customer-portal-design.md`
- `docs/superpowers/plans/2026-08-30-source-settings.md` *(new)*

# Tests Run

- `make prepush` — pass
- یک دیتابیسِ خالی از صفر migrate شد و ستون‌های `sources` با دیتابیسِ dev مقایسه
  شد: یکسان. چون dev ستون را از تلاشِ قبلی داشت و فقط نشانگرِ goose عقب برده شد،
  این تنها راهِ مطمئن شدن از نبودِ drift بود.
- روی دیتابیسِ واقعی: `TestUpdateSettingsCannotCarryTheCeiling` یک source با سقفِ
  بالابرده‌شده **در حافظه** به statement می‌دهد و ردیف را برمی‌خواند — سقف تکان
  نخورده، پرچم فعال بودن هم.
- تستِ سقف را عمداً شکستم (`src.IsActive = true` در use case). هر دو لایه قرمز
  شدند: `TestPostingWhatIsOursChangesNothing` و `TestAChangeCannotTouchWhatIsOurs`.
- `TestPostingWhatIsOursChangesNothing` عمداً `max_priority`،
  `allow_custom_address`، `is_active`، `approved_at` و `owner_user_id` را در فرم
  post می‌کند. هیچ‌کدام خوانده نمی‌شوند.
- صفحهٔ ویرایش با `whole()` چک می‌شود — همان تستی که بعد از باگِ nav نوشته شد.

# Follow-ups / Risks

- **spec یک تناقض داشت که همین‌جا برداشته شد.** فهرستِ «Not in phase 1» هنوز
  می‌گفت «Approval. Removed» در حالی که خطِ بعدی‌اش صفحهٔ تأیید را فاز ۲
  می‌دانست. خطِ اول از قبلِ برگشتِ تصمیم مانده بود.
- **`default_addresses` یکجا نوشته می‌شود.** دو ویرایشِ همزمان روی دو کانالِ
  مختلف، یکی دیگری را پاک می‌کند. همان معامله‌ای که `UpdateSource` هم دارد.
- **حذفِ credential هنوز نیست و عمدی است.** خاموش‌کردن جایش را می‌گیرد و تاریخ را
  نگه می‌دارد؛ ردیفی که پاک شود به سؤالِ «کِی برداشته شد» جواب نمی‌دهد.
- **انتقالِ مالکیتِ source همچنان نیست.** `owner_user_id` در هیچ‌کدام از دو
  statement نیست، و این تصمیم است نه فراموشی.

# Instruction

«برای Update کردن Source، مشتری فقط باید name، description و default_address را
بتواند تغییر دهد» با فهرستِ صریحِ آنچه نباید — `id`، `owner_user_id`،
`is_active`، `approved_at`، `created_at` — و «Credential، Webhook و API Key را
جزو Update Source نکن» و «تغییرات audit شوند» و «در plan/spec هم اعمال کن».
بعدش: «credential هم هرچی مونده انجام بده».
