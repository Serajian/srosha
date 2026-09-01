Branch: `feat/operator-alerts`

# Summary

کلیدهای اعلان در `docs/CONFIG.md` ثبت شدند. task پنج از پنج — آخرین قدم.

پنج کلید، هر سه باینری می‌خوانندشان:

```
NOTIF_ALERT_GOTIFY_SERVER_URL
NOTIF_ALERT_GOTIFY_TOKEN
NOTIF_ALERT_QUEUE          پیش‌فرض ۶۴
NOTIF_ALERT_TIMEOUT        پیش‌فرض ۱۰ ثانیه
NOTIF_ALERT_READY_EVERY    پیش‌فرض ۳۰ ثانیه
```

# سه چیزی که عمداً نوشته شدند

**«از pipeline ــِ خودِ srosha نمی‌رود.»** دلیلِ کلِ طراحی، در جایی که کسی
موقعِ تنظیم کردنش می‌خواند نه در یک spec که کسی باز نمی‌کند.

**«کلیدِ application id وجود ندارد و نباید داشته باشد»**، با شرحِ آزمایش.
وگرنه روزی کسی می‌بیند sender یک آدرس می‌گیرد و فکر می‌کند یادمان رفته کلیدش را
اضافه کنیم و «درستش» می‌کند.

**«هرکس آن token را دارد ایمیلِ مشتری‌ها را می‌بیند.»** تصمیمِ مالک بود، و جایی
که operator آن token را می‌سازد باید بداند چه چیزی دستش است — همان دسترسی‌ای که
`/audit` دارد و به‌خاطرش `super_admin` شد.

# Files Changed

- `docs/CONFIG.md` *(سطرِ alerts در جدولِ پیکربندی، و بخشِ Operator alerts)*

# Tests Run

- `make prepush` — pass *(سند است)*

# Follow-ups / Risks

- **سه جا هنوز دربارهٔ Gotify چیزِ غلطی می‌گویند**، و این کشفِ امروز است نه یک
  اشکالِ تازه: `sdk/go/README.md` و `README.fa.md` به مشتری می‌گویند آدرسِ
  Gotify همان application id است، و کامنتِ بالای `(*Sender).endpoint` هنوز آن
  را یک حدسِ باز می‌داند. هیچ‌کدام مالِ این branch نیست — دربارهٔ کانالِ مشتری
  است نه اعلانِ operator — ولی هر سه باید اصلاح شوند.
- `NOTIF_ALERT_READY_EVERY` پیش‌فرضش ۳۰ ثانیه است. dependency ای که بینِ دو
  پرسش بیفتد و برگردد دیده نمی‌شود، که برای یک قطعیِ لحظه‌ای درست است.

# Instruction

قدمِ آخرِ plan ــِ اعلان‌ها: کلیدها در `CONFIG.md` نوشته شوند و diff قبل از
ثبت به مالک نشان داده شود.
