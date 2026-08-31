Branch: `docs/config-admin-surface-shipped`

# Summary

`docs/CONFIG.md` هنوز طوری نوشته بود که انگار admin surface ساخته نشده. سه جا:

- در جدولِ پورت‌ها، کنارِ ۸۰۹۲ نوشته بود `*(phase 2)*`. آن فاز تمام شده و پورت
  امروز واقعاً سرو می‌شود. برچسب برداشته شد؛ بقیهٔ آن سطر — «never published» —
  درست است و دست نخورد.
- در جدولِ `public/`، دو سطر می‌گفت «today `static/portal/`» و «today
  `templates/portal/`». امروز `static/admin/` و `templates/admin/` هم هستند.
  سطرِ stylesheet و سطرِ logo هم فقط portal را می‌شناختند.
- در پاراگرافِ بعدش نوشته بود «`web.browserFiles` subs into `static/portal`
  first, and the admin surface will do the same into its own». این اتفاق افتاده:
  امضای تابع `browserFiles(surface string)` است و هر دو surface از همان می‌آیند.
  جمله به زمانِ حال آمد.

ضمناً عددِ «~250 lines» از سطرِ stylesheet حذف شد و جایش نامِ دو فایل نشست.
`portal.css` الان ۴۶۲ خط است، یعنی آن عدد از قبل غلط بود. عددی که با هر commit
عوض می‌شود در سندی که باید درست بماند جا ندارد.

یک سطرِ تازه هم اضافه شد برای `public/templates/email/`، که وجود دارد و در آن
جدول نبود — با این توضیح که surface نیست: رندر می‌شود ولی هرگز سرو نمی‌شود.

هر چهار ادعا قبل از نوشتن از روی درخت تأیید شد: `ls public/static public/templates`،
امضای `browserFiles` در `internal/adapter/api/web/assets.go`، و `shasum` روی دو
`crane.svg` که نشان داد واقعاً یک فایلِ کپی‌شده‌اند نه دو فایلِ متفاوت.

# Files Changed

- `docs/CONFIG.md` *(سه اصلاح: برچسبِ phase 2، جدولِ `public/`، و جملهٔ browserFiles)*

# Tests Run

- `make precommit` — pass *(هیچ کدی عوض نشده؛ برای اطمینان)*

# Follow-ups / Risks

- `docs/ARCHITECTURE.md` خطِ ۴۳۱ همین مشکل را دارد: «`internal/adapter/api/web`
  is built on gin. The admin surface **will be**…» و پاراگرافِ بعدش دلیلِ
  انتخابِ gin را روی «the shape of what is coming» بنا کرده. آن حالا رسیده. دست
  نزدم چون برخلافِ CONFIG، این یکی را قبلاً به مالک گزارش نکرده بودم و تغییرش
  یک بازنویسیِ استدلال است نه اصلاحِ یک داده. تصمیمش با اوست.

# Instruction

کاربر گفت کارهایی را که می‌خواستم انجام دهم انجام بدهم. تنها موردِ باز و در
دستِ من، همان چیزی بود که در گزارشِ قبلی (`2026-08-31-sdk-readme-key-flow`) زیرِ
Follow-ups نوشته شده بود: برچسبِ phase 2 در CONFIG.md. موقعِ باز کردنِ فایل دو
کهنگیِ دیگر از همان خانواده هم پیدا شد و با همان اصلاح شد.
