Branch: `refactor/admin-on-its-own-host`

# Summary

چکی که در production اصرار داشت `NOTIF_ADMIN_ADDR` روی loopback باشد حذف شد.
task دو از سه در plan ــِ `2026-08-31-admin-on-its-own-host.md`.

**چه چیزی حذف شد و چرا:** متدِ `AdminAddr.bindsLoopback` و این `r.Check`:

```go
r.Check(!production || c.AdminAddr.bindsLoopback(),
	"NOTIF_ADMIN_ADDR must bind the loopback interface in production ...")
```

کامنتِ خودِ آن متد loopback را «آدرسی که فقط به ماشینی که رویش اجرا می‌شود
می‌رسد» تعریف کرده بود. آن موقع درست بود، چون باینری روی host اجرا می‌شد. داخلِ
container آن «ماشین» خودِ container است، پس listener در یک network namespace
تنها می‌ماند و **هیچ‌چیز** به آن نمی‌رسد: نه `ports:`، نه container ــِ دیگری روی
`srosha-net`، نه SSH tunnel. یعنی این guard در هر deployment ــِ container ای
پنل را نه امن، بلکه غیرقابلِ استفاده می‌کرد — و چون تأییدِ source فقط از همان
پنل ممکن است، کلِ سرویس بالا می‌آمد و هرگز چیزی نمی‌فرستاد.

**چه چیزی جایش را گرفت:** commit قبلی، `46b89db`. cookie حالا به ازای هر surface
جداست و با **host** اسکوپ می‌شود، پس session ــِ یک مشتری روی
`admin.srosha.ir` رد نمی‌شود — اصلاً فرستاده نمی‌شود. این از guardِ حذف‌شده
قوی‌تر است: آن یکی پنل را سخت‌دسترس می‌کرد، این یکی presentation را ناممکن.

دو تستی که آن guard را می‌سنجیدند با خودش رفتند
(`TestProductionRefusesAnAdminAddressOnEveryInterface` و
`TestProductionAcceptsALoopbackAdminAddress`) و جایشان یکی آمد که ثابت می‌کند
production حالا آدرسی را می‌پذیرد که یک container واقعاً می‌تواند استفاده کند:
`TestProductionAcceptsAnAdminAddressAContainerCanUse`.

تست‌های `SecureCookie` دست نخوردند. آن guard سرِ جایش است و هنوز درست است.

# دو کامنت که بعد از حذف دروغ شده بودند

plan این‌ها را ندیده بود و موقعِ خواندنِ فایل پیدا شدند:

۱. کامنتِ بالای `PortalAddr` و `AdminAddr` می‌گفت این دو «به دو listener داده
می‌شوند، یکی روی اینترنت و یکی روی loopback». نیمهٔ دومش دیگر درست نیست. خودِ
استدلال — دو تایپِ جدا تا جابه‌جا شدنشان خطای کامپایل باشد — هنوز کاملاً معتبر
است و ماند؛ فقط آن توصیفِ غلط برداشته شد.

۲. مقدارِ پیش‌فرضِ `127.0.0.1:8092` ماند، ولی حالا بدونِ guard یک خواننده
می‌پرسد چرا. یک کامنتِ کوتاه اضافه شد: برای لپ‌تاپ درست است و فقط یک پیش‌فرض
است؛ در deployment این surface مثل هر سرویسِ دیگری گوش می‌دهد.

# Files Changed

- `internal/config/settings/console.go` *(متد و چک حذف؛ import ــِ `net` افتاد؛ دو کامنت اصلاح)*
- `internal/config/config_test.go` *(دو تست حذف، یکی اضافه)*

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass
- `make format-core` — clean
- `make precommit` — pass

# Follow-ups / Risks

- **بعد از این commit هیچ‌چیز در کد جلوی روی اینترنت رفتنِ پنل را نمی‌گیرد**، چون
  قرار است روی اینترنت باشد. تنها چیزی که آن را نگه می‌دارد cookie ــِ جدا و چکِ
  زندهٔ نقش است. اگر روزی کسی cookie را دوباره یکی کند، این commit است که آن را
  خطرناک می‌کند.
- `docs/ARCHITECTURE.md` هنوز می‌گوید «The admin port is never published». task 3
  آن را درست می‌کند و تا آن موقع سند و کد اختلاف دارند.
- operator هنوز قفلِ دومی ندارد. در spec به‌عنوانِ ریسکِ پذیرفته‌شده ثبت است.

# Instruction

ادامهٔ همان plan که مالک گفت شروع شود. task دو: guardِ loopback برداشته شود تا
پنل در container قابلِ سرو باشد — و طبقِ plan فقط بعد از task یک، که سدّ
جایگزین را ساخت.
