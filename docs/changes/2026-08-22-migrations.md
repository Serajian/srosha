Branch: `feat/schema`

# Summary

هفت migration، یکی به ازای هر جدول به‌علاوهٔ یک domain. تا امروز `migrations/`
اصلاً وجود نداشت و هر repository ای پشتش گیر بود.

## دامنهٔ ulid

اول `CHAR(26)` نوشته شد و اجرا نشانم داد چرا غلط است. در خروجی یک خطای یکتایی این
دیده شد:

```
Key (notification_id, channel, address)=(01N1                      , email, ...)
                                              ↑ فاصله‌های اضافه
```

`CHAR` مقدار کوتاه‌تر را با فاصله پر می‌کند. ULID های ما همیشه ۲۶ کاراکترند پس در
عمل پیش نمی‌آید، ولی یک نوشتن غلط بی‌صدا به مقداری تبدیل می‌شود که دیگر با فرم بدون
فاصله برابر نیست. خود مستندات postgres هم می‌گوید `CHAR` هیچ مزیتی بر `TEXT` ندارد.

پس یک `DOMAIN` جایش را گرفت که الفبای Crockford را هم چک می‌کند — کاری که `CHAR`
اصلاً نمی‌توانست:

```sql
CREATE DOMAIN ulid AS TEXT CHECK (VALUE ~ '^[0-9A-HJKMNP-TV-Z]{26}$');
```

## تصمیم‌هایی که در schema نشسته‌اند

**enum ها متن با `CHECK` اند**، نه enum بومی. اضافه کردن یک مقدار به enum بومی یک
migration قفل‌کننده است؛ با `CHECK` فقط قید عوض می‌شود. مقادیر از خود کد درآمدند نه
از حافظه: `PENDING/SENT/FAILED`، `EXPIRED/MAX_ATTEMPTS/PERMANENT/NO_SENDER`،
`NORMAL/HIGH/CRITICAL`، و چهار کانال.

**`sources.id` هم ULID است** و در Go ساخته می‌شود، مثل بقیه. گزینهٔ نام خوانا
(`acme`) و گزینهٔ ساختن در postgres هر دو مطرح و رد شدند.

**`api_keys` جدول جداست** و **`credentials.secret` خودتوصیف است** — هر دو طبق
ورودی‌های `docs/ARCHITECTURE.md`.

**`credentials.config` و `notifications.metadata` هر دو `jsonb`** اند تا domain
کاری با محتوایشان نداشته باشد و adapter دو بار get نزند.

**`deliveries.created_at`** اضافه شد در حالی که در `Snapshot` نیست: `updated_at`
به محض اولین حرکت ردیف دیگر نمی‌تواند بگوید این delivery چقدر منتظر مانده.

## کلید خارجی که عمداً CASCADE نیست

اجرا این را هم نشان داد. `api_keys` و `credentials` و `webhooks` با حذف source پاک
می‌شوند، ولی `notifications` جلوی حذف را می‌گیرد.

این عمدی است: تاریخچهٔ پیام‌های یک source یک سند است، و CASCADE یعنی پاک کردن یک
ردیف مشتری بی‌صدا کل سابقهٔ ارسال‌هایش را ببرد. حالا کامنتش هم در همان‌جا هست، چون
بدون آن شش ماه بعد شبیه یک فراموشی به نظر می‌رسد.

## چه چیزی واقعاً امتحان شد

فقط اینکه `goose up` سبز شود چیزی ثابت نمی‌کند. هر قیدی که در کامنت‌ها ادعا شده
مقابل postgres واقعی اجرا شد:

| | نتیجه |
| --- | --- |
| دو credential پیش‌فرض روی یک کانال | رد شد |
| دومی غیرپیش‌فرض روی همان کانال | پذیرفته شد |
| همان `idempotency_key` دو بار | رد شد |
| دو پیام بدون کلید | هر دو پذیرفته شدند |
| همان گیرنده دو بار در یک پیام | رد شد |
| `status='DELIVERED'` | رد شد |
| `failure_reason='BECAUSE'` | رد شد |
| ULID با حروف کوچک / کوتاه / حرف `I` | هر سه رد شدند |
| کوئری بازیابی | از `deliveries_status_updated_at_idx` استفاده می‌کند |

و `goose reset` هیچ چیز جز جدول خود goose باقی نمی‌گذارد — نه جدولی، نه دامنه‌ای.

# Files Changed

- `migrations/00001_create_ulid_domain.sql` *(تازه)*
- `migrations/00002_create_sources.sql` *(تازه)*
- `migrations/00003_create_api_keys.sql` *(تازه)*
- `migrations/00004_create_credentials.sql` *(تازه)*
- `migrations/00005_create_webhooks.sql` *(تازه)*
- `migrations/00006_create_notifications.sql` *(تازه)*
- `migrations/00007_create_deliveries.sql` *(تازه)*

# Tests Run

- `make migrate-up` → هر هفت‌تا
- `make migrate-reset` → همه برگشتند، فقط `goose_db_version` ماند
- `make migrate-up` دوباره → سبز
- جدول قیدها بالا، هر ردیفش مقابل postgres واقعی

# Follow-ups / Risks

- `Source.ID` در Go هنوز `string` است نه `shared.ID`، و `SourceID string` در چهار
  domain تکرار شده. حالا که ULID است، ارتقایش تایپ‌ایمنی می‌دهد — ولی یک refactor
  جداست و برای migration لازم نبود.
- `NOTIF_WEBHOOK_SECRETS` حالا با ULID کلید می‌خورد، پس ویرایش دستی‌اش سخت‌تر شد.
- هیچ repository ای هنوز نوشته نشده. قدم بعدی sqlc است.
- migration ها هنوز در هیچ CI ای اجرا نمی‌شوند.
- `sources.default_addresses` یک `jsonb` است که کانال را به یک رشته می‌برد، و تا
  وقتی هر ورودی **فقط یک مقدار** است همین درست است. روزی که یک ورودی به چیز دومی
  نیاز پیدا کند — یک سوئیچ خاموش/روشن، یک برچسب، اتصال به یک credential خاص —
  همان روز باید جدول شود. سه چیز آن‌وقت می‌شکند: نمی‌شود روی میدان دوم ایندکس
  گذاشت، نمی‌شود در دیتابیس اعتبارسنجی‌اش کرد، و مهم‌تر از هر دو، نوشتن کل map را
  بازمی‌نویسد — پس دو درخواست هم‌زمان که دو کانال متفاوت را عوض می‌کنند، یکی
  تغییر دیگری را بی‌صدا پاک می‌کند.
- نام `default_addresses` بحث شد. مفهومش «مقصد پیش‌فرض» است، ولی `address` واژهٔ
  مستقر این مخزن است (`Recipient.Address`، `ValidateAddress`،
  `allow_custom_address`، `deliveries.address`) و آوردن `destination` یعنی دو
  کلمه برای یک چیز. آنچه روزی عوض می‌شود شکل است نه اسم.

# Instruction

«از migration ها شروع کنیم، بعد برویم service ها و domain ها را بر اساس migration ها
تکمیل کنیم.» سه تصمیم قبلش گرفته شد: `notified_at` بماند، `sources.id` را Go به شکل
ULID بسازد، و یک migration به ازای هر جدول.
