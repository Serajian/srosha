Branch: `feat/credential-trial`

# Summary

دو سندی که هنوز console را باینری‌ای توصیف می‌کردند که فقط صفحه سرو می‌کند و ردیف
می‌خواند، با آنچه سه commit قبل ساخته شد یکی شدند. **هیچ کدی عوض نشده.** task چهار از
چهار.

## `docs/CONFIG.md`

سه جا:

- در جدولِ config، سطرِ `http client` ستونِ console از `—` به `✅`. تا امروز فقط
  dispatcher می‌خواندش.
- کلیدِ `NOTIF_CONSOLE_TRIAL_PER_MINUTE` به سطرِ `console` اضافه شد.
- یک پاراگراف زیرِ همان‌جایی که می‌گفت console «فقط policy را می‌خواند و هرگز خودِ
  callback را نمی‌زند»: حالا می‌گوید چرا http client دارد، و مهم‌تر — که **هیچ‌کدام از
  `NOTIF_SENDER_*` را نمی‌خواند**، و آن نخواندن خودِ مرز است نه یک قلم‌افتادگی.

سقف هم آنجا نوشته شد، با همان جمله‌ای که در کد است: سهمیهٔ فرستادنِ source **نیست** و
نمی‌تواند باشد، چون limiter ــِ gateway سطلِ دیگری در process ــِ دیگری است.

## `docs/ARCHITECTURE.md`

یک زیربخشِ تازه در انتهای «Two surfaces in one binary»:
**The console can send, and only as the customer.**

می‌گوید چه چیزی عوض شد، و بیشترِ حجمش دربارهٔ چیزی است که **نمی‌تواند** انجام دهد:
`Fallback` ــِ خالی، اینکه `Registry.ours` هر شاخه‌اش اول `configured()` را می‌پرسد، و
اینکه چرا `consoleSenders` یک تابعِ جدا است — تا تست همان کدی را صدا بزند که باینری صدا
می‌زند. نامِ تست در متن آمده، چون بدون آن این بخش یک کامنت است نه یک خاصیت.

و دو جملهٔ آخر که راحت فراموش می‌شوند: trial با **نام** resolve می‌شود نه با id، و
**هیچ notification و هیچ delivery ای نمی‌سازد** — یک diagnostic است، و اگر در message
log بنشیند آن log ادعا می‌کند source چیزی فرستاده که نفرستاده.

# Files Changed

- `docs/CONFIG.md` *(دو خانهٔ جدول، و یک پاراگرافِ تازه)*
- `docs/ARCHITECTURE.md` *(زیربخشِ «The console can send, and only as the customer»)*

# Tests Run

- `make precommit` — pass

# Follow-ups / Risks

- **`docs/CONFIG.md` یک چیزِ غلط دارد که مالِ این کار نیست.** خطوطِ ۳۲۷ تا ۳۳۱ هنوز
  می‌گویند «In production the console refuses to start unless `NOTIF_ADMIN_ADDR` binds
  loopback». آن check در `2026-08-31-admin-listener-guard-removed.md` حذف شد و
  `bindsLoopback` دیگر در کد نیست. پاراگرافِ بالای آن هم می‌گوید بسته ماندنِ پورتِ admin
  «a property of the process» است، که همان استدلالِ باطل‌شده است. یک تغییرِ جدا با گزارشِ
  خودش می‌خواهد.
- **`.env.example` هیچ کلیدِ `NOTIF_CONSOLE_*` ندارد** — نه SMTP، نه cookie، نه هیچ‌کدام.
  از وقتی باینریِ سوم ساخته شد عقب مانده. `NOTIF_CONSOLE_TRIAL_PER_MINUTE` عمداً تنها به
  آن اضافه نشد: تنها کلیدِ console در فایلی که بخشِ console ندارد، وضع را گیج‌تر می‌کند
  نه بهتر. آن هم یک تغییرِ جدا است.

# Instruction

قدمِ چهارمِ credential trial: دو سند را با آنچه ساخته شد یکی کن — چه چیزی به config
اضافه شد، و console حالا چه می‌تواند و مهم‌تر، چه **نمی‌تواند**.
