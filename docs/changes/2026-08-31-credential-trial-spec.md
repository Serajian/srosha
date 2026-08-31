Branch: `feat/credential-trial-spec`

# Summary

spec و plan برای «تستِ یک sending identity» نوشته شدند. هیچ کدی عوض نشده.

- `docs/superpowers/specs/2026-08-31-credential-trial-design.md`
- `docs/superpowers/plans/2026-08-31-credential-trial.md`

مسئله: یک source توکنِ Telegram یا رمزِ SMTP را ثبت می‌کند و هیچ راهی ندارد
بفهمد کار می‌کند یا نه. اولین اثبات ساعت‌ها بعد می‌آید، در یک delivery row که
کسی نگاهش نمی‌کند.

# چیزی که این را از یک handler به یک تصمیمِ معماری تبدیل کرد

**کنسول نمی‌تواند بفرستد.** `usecase.Credentials` که portal صدا می‌زند فقط با
دیتابیس کار می‌کند؛ هشت adapter و `sender.NewRegistry` فقط در dispatcher ساخته
می‌شوند و کلیدهای `NOTIF_SENDER_*` هم مالِ اوست. کنسول به NATS هم وصل نیست، پس
حتی نمی‌تواند کار را بسپارد.

ولی دو چیز در کدِ موجود این را کوچک‌تر از آنچه به‌نظر می‌رسد کرد:

۱. `GoogleTokens` و `AppleTokens` **کارخانه‌اند**: مادهٔ خام را آرگومان می‌گیرند،
   پس ساختنشان هیچ رازی از srosha لازم ندارد.
۲. مسیرِ credential ــِ خودِ source (`build`) از مسیرِ هویتِ srosha (`ours`)
   جداست، و هر شاخهٔ `ours` اول `configured()` را می‌پرسد.

پس کنسول یک registry با `Fallback{}` ــِ **خالی** می‌سازد: می‌تواند به‌جای مشتری
بفرستد و **ساختاراً نمی‌تواند** به‌جای srosha بفرستد. این مرز را کدِ موجود نگه
می‌دارد نه انضباط، و task یک یک تست برایش دارد.

# تصمیم‌هایی که مالک گرفت

- تست یعنی **یک پیامِ واقعی**، نه چکِ اعتبارِ توکن. دومی متدِ تازه‌ای روی
  `delivery.Sender` و هشت پیاده‌سازی می‌خواست، و برای APNs و WhatsApp اصلاً
  endpoint ــِ تمیزی ندارد.
- **کنسول، هم‌زمان.** مشتری کلیک می‌کند و همان‌جا خطای خودِ provider را می‌بیند.
- آدرس: **`DefaultAddresses[channel]`** و نه فیلدی که مشتری تایپ کند.

# سه چیزی که موقعِ نوشتن قطعی شد

**تست روی `Credentials` نمی‌نشیند.** آن type را دو باینری می‌سازند — کنسول و
gateway که همان را روی gRPC به SDK می‌دهد. افزودنِ یک `SenderRegistry` ــِ
اجباری به سازنده‌اش gateway را مجبور می‌کرد یکی بسازد، و gateway هیچ adapter ــِ
فرستنده‌ای ندارد. پس `usecase.Trials` ــِ جدا، که فقط کنسول می‌سازدش — همان
الگویی که برای `Operators` در برابرِ `Sources` استفاده شد.

**تست با نام resolve می‌شود، نه با id.** یعنی از همان مسیری می‌رود که یک ارسالِ
واقعی. نتیجه‌اش: credential ــِ خاموش همان‌جا رد می‌شود، با همان خطا. تستی که
جورِ دیگری resolve کند چیزِ دیگری را ثابت می‌کند.

**سقف، سهمیهٔ ارسال نیست و نمی‌تواند باشد.** limiter ــِ gateway سطلی در پروسهٔ
دیگری است. `NOTIF_CONSOLE_TRIAL_PER_MINUTE` با پیش‌فرضِ ۳، و در spec صریح نوشته
شد که این سهمیه را خرج نمی‌کند — وانمود کردن به عکسش دروغی است که یک روز کسی
رویش حساب می‌کند.

# Files Changed

- `docs/superpowers/specs/2026-08-31-credential-trial-design.md` *(جدید)*
- `docs/superpowers/plans/2026-08-31-credential-trial.md` *(جدید — چهار task)*

# Tests Run

- `make precommit` — pass *(سند است)*

# Follow-ups / Risks

- `credential.test` به `sourceDecisionVerbs` اضافه **نمی‌شود**، و plan این را
  به‌عنوانِ یک قدمِ صریح نوشته تا اجراکننده وسوسه نشود. actor ــِ آن مشتری است و
  آن فهرست یک مرزِ حریمِ خصوصی است.
- کامنتِ `config.Console` می‌گوید کنسول «no sending credentials» دارد و «serves
  pages and reads rows». نیمهٔ دومش با این کار غلط می‌شود؛ task یک اصلاحش
  می‌کند.
- اصلاحِ کامنتِ `AdminAddr` در stash است و منتظرِ دستورِ commit روی
  `refactor/admin-on-its-own-host`.

# Instruction

مالک گفت روی portal برای credential ها test connection نوشته شود: یک source که
ثبت و تأیید شده بتواند channel هایش را تست کند. مسیر architectural تشخیص داده
شد، سه سؤال پرسیده شد (معنیِ تست، کدام باینری، کدام آدرس)، طراحی تأیید شد و
دستور این بود که spec و بعد plan نوشته شوند.
