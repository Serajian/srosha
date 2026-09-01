Branch: `docs/config-matches-the-code`

# Summary

`.env.example` **۵۵ کلید** کم داشت. حالا هر ۸۹ کلیدی که کد می‌خواند در آن هست.
**هیچ کدی عوض نشده.**

## این خیلی بزرگ‌تر از چیزی بود که گفتم

وقتی این را به‌عنوان follow-up ثبت کردم نوشتم «`.env.example` هیچ کلیدِ `NOTIF_CONSOLE_*`
ندارد» — انگار یک بخشِ جاافتاده است. نبود. مقایسهٔ کلیدهای فایل با کلیدهایی که
`internal/config/settings` واقعاً می‌خواند این را داد:

```
همهٔ NOTIF_SENDER_*        ۱۵ کلید   هر هشت کانالِ srosha
همهٔ NOTIF_CONSOLE_*       ۶ کلید    به‌علاوهٔ PORTAL_ADDR و ADMIN_ADDR
همهٔ NOTIF_ALERT_*         ۵ کلید
همهٔ NOTIF_RECONCILE_*     ۵ کلید    کلِ recovery
همهٔ NOTIF_DISPATCH_*      ۳ کلید
همهٔ NOTIF_RETENTION_*     ۳ کلید
NOTIF_GRPC_ADDR                     آدرسی که کلِ customer API رویش است
NOTIF_RATELIMIT_PER_MINUTE
NOTIF_AUTH_KEY_TOUCH_AFTER
NOTIF_MIGRATION_LOCK_TIMEOUT
NOTIF_WEBHOOK_TIMEOUT, _MAX_FAILURES
```

فایل حدودِ یک‌سومِ پیکربندی را پوشش می‌داد. `docs/CONVENTIONS.md` می‌گوید هر knob باید
«در `.env.example` و `docs/CONFIG.md`» باشد؛ نیمهٔ دوم رعایت شده بود و نیمهٔ اول نه.

عملاً یعنی کسی که فایل را کپی می‌کرد یک console داشت که **بالا نمی‌آمد** — چون
`NOTIF_CONSOLE_SMTP_HOST` اجباری است و اصلاً در فایل نبود.

## آنچه اضافه شد

فایل حالا بعد از بخشِ مشترک، بر اساس **اینکه کدام باینری می‌خواندش** بخش‌بندی شده — همان
تقسیمی که جدولِ `docs/CONFIG.md` دارد، و آن دو باید با هم بخوانند:

```
gateway               grpc، auth، ratelimit
dispatcher            dispatch، reconcile، webhook
dispatcher (senders)  هر هشت کانال، جدا، چون بلندترین بخش است
gateway + dispatcher  retention
console               دو surface، دو آدرس، smtp، سقفِ trial
هر سه                 alerts
migrate               فقط قفل
```

همه در یک فایل و نه چهارتا، عمداً: کلیدی که کسی پیدایش نکند کلیدی است که در جای غلط
ست می‌شود.

## سرصفحه هم غلط بود

می‌گفت «shared by both binaries» و «`.env.gateway` or `.env.dispatcher`». چهار باینری
هست. `internal/config/load.go` واقعاً `.env` و بعد `.env.<binary>` را می‌خواند، پس خودِ
مکانیزم درست بود — فقط تعداد در سرصفحه از وقتی console ساخته شد عوض نشده بود.

## مقدارها

پیش‌فرض‌ها مستقیم از خودِ `settings` برداشته شدند، نه از حافظه. رازها خالی‌اند و هیچ
مقدارِ واقعی‌ای در فایل نیست — همان قانونی که سرصفحه‌اش از قبل داشت.

دو چیز که موقعِ نوشتن ارزشِ توضیح داشتند و در فایل آمدند: `SENDER_APNS_KEY` و
`SENDER_FCM_SERVICE_ACCOUNT` هر دو **base64 ــِ خودِ فایل** اند (`decodeKey` بازشان
می‌کند)، و آن encoding فقط یک نگرانیِ محیطی است — source ای که هویتِ خودش را ثبت می‌کند
خودِ فایل را می‌فرستد.

# Files Changed

- `.env.example` *(۵۵ کلیدِ تازه در هفت بخشِ باینری‌محور، و سرصفحه‌ای که چهار باینری را می‌شناسد)*

# Tests Run

- `make precommit` — pass
- مقایسهٔ کلیدهای کد با کلیدهای فایل: هیچ کلیدی باقی نمانده
- هیچ کلیدِ تکراری در فایل نیست

# Follow-ups / Risks

- **هیچ‌چیز این دو را همگام نگه نمی‌دارد.** فایل دقیقاً به همین شکل عقب افتاد: کلید
  اضافه شد، `CONFIG.md` به‌روز شد، `.env.example` جا ماند و هیچ چک‌ای نگرفتش. یک هدف که
  کلیدهای `settings` را با فایل مقایسه کند این را از یک انضباط به یک مکانیزم تبدیل
  می‌کند — همان تفاوتی که `arch-check` برای معماری ساخت.
- بخشِ `sender` بلندترین بخشِ فایل است و با هر کانالِ تازه بلندتر می‌شود.

# Instruction

دومین follow-up از گزارشِ credential trial: کلیدهای جاافتادهٔ console را به
`.env.example` اضافه کن. موقعِ کار معلوم شد جاافتادگی خیلی بیشتر از console است، و کلِ
فایل با کد یکی شد.
