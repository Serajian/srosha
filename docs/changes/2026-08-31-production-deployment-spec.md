Branch: `feat/deployment-spec`

# Summary

spec ــِ deployment نوشته شد و بعد از تأییدِ مالک، دو plan هم کنارش. هیچ کدی
عوض نشده — این commit سه سند است و بس.

srosha تا امروز هیچ‌جا جز لپ‌تاپ اجرا نشده. سه باینری، هشت channel، یک SDK
منتشرشده، portal و پنلِ admin — و نه Dockerfile ای، نه compose ــِ deploy، نه
هیچ چیزی که این‌ها را روی سرور ببرد.

spec کار را به **دو قسمت** تقسیم می‌کند، و ترتیبشان اجباری است:

- **A** — surface ــِ admin از یک **پورت** به یک **host** منتقل می‌شود.
- **B** — image و stack: `Dockerfile`، `docker-compose.yml`، و یک سرویسِ
  یک‌بارمصرف برای migration.

دلیلِ ترتیب، تمیزی نیست. یک تناقضِ واقعی پیدا شد: `settings/console.go` در
production اصرار دارد که `NOTIF_ADMIN_ADDR` روی loopback باشد، و کامنتِ خودش
loopback را «ماشینی که رویش اجرا می‌شود» تعریف می‌کند. داخلِ container آن ماشین
خودِ container است، پس آن listener در یک network namespace تنها می‌ماند: نه
`ports:` به آن می‌رسد، نه container ــِ دیگری روی `srosha-net`، نه SSH tunnel.
یعنی B بدونِ A پنلی deploy می‌کند که هیچ‌کس بازش نمی‌کند — و چون تأییدِ source
فقط از همان پنل ممکن است، کلِ سرویس بالا می‌آید و هرگز چیزی نمی‌فرستد.

تصمیمی که مالک گرفت: پنل **public** باشد روی `admin.srosha.ir`، با cookie ــِ
جدا. استدلالش در spec کامل نوشته شده، چون یک تصمیمِ امنیتیِ مستند را عوض می‌کند.
خلاصه‌اش: جداکنندهٔ امروز پورت است و cookie پورت را نمی‌شناسد، پس session ــِ هر
مشتری به listener ــِ admin هم می‌رسد و تنها سدّ یک `if` است. cookie اما **host**
را می‌شناسد؛ با subdomain ــِ جدا آن session اصلاً فرستاده نمی‌شود.

چیزی که در spec به‌عنوانِ ریسک ثبت شد و پنهان نشده: operator هم با همان کدِ
ایمیلی وارد می‌شود و قفلِ دومی ندارد، پس public شدنِ پنل یعنی هرکس صندوقِ ایمیلِ
یک operator را بخواند srosha مالِ اوست. امروز آن حمله به SSH ــِ سرور هم نیاز
دارد. دو جوابِ ارزان دارد (IP allowlist روی Traefik، یا قفلِ دومِ operator) و
هیچ‌کدام در دامنهٔ این کار نیست.

تصمیم‌های دیگری که مالک گرفت و در spec نشست: دو subdomain جدا به‌جای یک domain
با path (`api.srosha.ir` و `panel.srosha.ir`)، migration به‌شکلِ یک service زیرِ
profile در همان compose و همان image، و **CI فعلاً نه**.

اعدادی که در spec آمده از کد خوانده شده نه از حافظه: محدودیت‌های کدِ ورود
(`logincode`: ۶ رقم، ۳ حدس، ۱۰ دقیقه؛ `usecase`: ۵ کد در ساعت)، اینکه هر surface
از قبل `sessions` ــِ خودش را می‌سازد، اینکه `sessions.begin` هیچ `Domain` ای ست
نمی‌کند (و همین است که cookie را host-only نگه می‌دارد)، و نسخه‌های واقعیِ
goose (`v3.27.3`) و Go (`1.26`).

# Files Changed

- `docs/superpowers/specs/2026-08-31-production-deployment-design.md` *(جدید)*
- `docs/superpowers/plans/2026-08-31-admin-on-its-own-host.md` *(جدید — قسمت A، سه task)*
- `docs/superpowers/plans/2026-08-31-deployment-stack.md` *(جدید — قسمت B، سه task)*

# Tests Run

- `make precommit` — pass *(سند است؛ برای اطمینان از اینکه چیزی نشکسته)*

# Follow-ups / Risks

- بندِ «one image, **both** binaries» در `docs/CONFIG.md` هنوز غلط است. عمداً در
  این commit اصلاح نشد: spec می‌گوید همراهِ قسمت B اصلاح شود، چون تا وقتی compose
  نوشته نشده معلوم نیست جمله‌اش دقیقاً چه باید باشد.
- `nats-server.conf` با Dokploy mount می‌شود و در مخزن نیست. یعنی تنها فایلی که
  کاربران و permission های broker را تعریف می‌کند، کنارِ کدی که به آن وابسته است
  نسخه‌بندی نمی‌شود. محتمل‌ترین منبعِ شکستِ اولین deploy.
- `srosha-net` دستی ساخته می‌شود. شبکه‌ای که چون یک بار کسی دستوری زده وجود دارد،
  روزی که سرور بازسازی شود وجود نخواهد داشت.
- هر دو plan در قدمِ آخرِ هر task می‌گویند «بایست»، نه «commit کن». این عمدی است:
  قانونِ مخزن می‌گوید بدونِ دستورِ صریحِ مالک commit ممنوع است، و template ــِ
  skill قدمِ commit دارد. قانونِ مخزن برنده است و در Global Constraints ــِ هر دو
  plan نوشته شده تا اجراکننده‌ای که فقط plan را می‌خواند هم بداند.
- plan ــِ B در task 2 می‌گوید stack را فقط تا `postgres` بالا بیاور و بنویس که چه
  چیزی **ثابت نشد**. بقیهٔ سرویس‌ها روی لپ‌تاپ بالا نمی‌آیند: `.env` ــِ واقعی و
  `nats-server.conf` ای که فقط Dokploy mount می‌کند را ندارند.

# Instruction

مالک گفت «برویم سراغ deployment». مسیر architectural تشخیص داده شد و در چهار
سؤالِ پشتِ‌هم شکل گرفت: (۱) دو subdomain جدا، (۲) پنلِ admin public باشد چون auth
داریم — با cookie ــِ جدا، (۳) migration به‌شکلِ سرویسِ یک‌بارمصرف در compose،
(۴) CI فعلاً نه. domain هم `srosha.ir` است. تقسیم‌بندی به دو قسمت تأیید شد و
دستور این بود که spec نوشته شود.
