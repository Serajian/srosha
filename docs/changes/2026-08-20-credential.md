Branch: `refactor/domain-layer`

# Summary

`domain/credential` نوشته شد، و `Delivery` یک فیلد `SenderName` گرفت تا بشود گفت با
کدام هویت فرستاده شود.

## مسئله

یک source ممکن است بخواهد با بات خودش بفرستد نه بات srosha، یا با چند هویت مختلف
روی یک کانال — `noreply@acme.com` برای تراکنش‌ها و `news@acme.com` برای خبرنامه.

## چه چیزی domain است و چه چیزی نیست

اول فکر می‌کردم credential اصلاً domain نیست و در config می‌نشیند. مالک پرسید اگر در
دیتابیس باشد چطور می‌تواند domain نداشته باشد — و حق داشت. چیزی که در دیتابیس چرخهٔ
عمر دارد (ساخته می‌شود، چرخانده می‌شود، غیرفعال می‌شود) و قاعده دارد، entity است.

ولی **راز داخلش نمی‌رود**:

```
domain/credential   کدام هویت، مال کی، برای کدام کانال، فعال هست
adapter             توکن و تنظیمات provider
```

دلیلش عمدتاً امنیت نیست — راز با یک تایپ که در `String()` خودش را redact کند مهار
می‌شود. دلیل اصلی `config` است: برای ایمیل یعنی `host` و `port`، برای واتس‌اپ یعنی
`phone_number_id`. اگر این داخل هسته بنشیند، **هسته می‌فهمد SMTP چیست** و
اضافه‌کردن کانال پنجم یعنی دست‌زدن به domain. قانون `docs/CONVENTIONS.md` می‌گوید
دانش provider فقط در adapter آن provider زندگی می‌کند.

نتیجهٔ عملی‌اش هم هست: gateway این را cache می‌کند، و اگر توکن‌ها داخلش باشند هر
پروسهٔ gateway توکن همهٔ مشتری‌ها را در حافظه دارد، برای کاری که هرگز انجام نمی‌دهد.

تایپ Go خودش گارد است: چون فیلدی برای راز ندارد، حتی `SELECT *` هم نمی‌تواند چیزی
داخلش بگذارد. یک تست هم همین را نگه می‌دارد.

## دو فیلدی که مخفی‌اند

`isDefault` و `isActive`، چون بینشان یک invariant واقعی هست: **credential غیرفعال
نمی‌تواند پیش‌فرض باشد**. اگر باشد، هر ارسال روی آن کانال شکست می‌خورد بدون اینکه
چیز آشکاری برای درست‌کردن وجود داشته باشد. `Deactivate` پرچم پیش‌فرض را هم پاک
می‌کند و `MakeDefault` روی غیرفعال رد می‌کند.

## `Pick`

تنها تصمیمی که هسته دربارهٔ credential می‌گیرد: اسم داده شده → همان، باید فعال باشد؛
اسم نداده → پیش‌فرضِ فعال؛ هیچ‌کدام → رد. سه sentinel جدا دارد تا gateway بتواند
بگوید کدام اتفاق افتاده.

مقدار برمی‌گرداند نه اشاره‌گر: `nil` یک راه دوم برای گفتن «هیچی» می‌شد و دلیلش را
هم نمی‌گفت، و اشاره‌گر به داخل slice صدازننده یعنی او می‌تواند از راه دور عنصرش را
عوض کند.

## `Delivery.SenderName`

روی delivery نشست نه روی notification، چون dispatcher روی یک delivery کار می‌کند و
آن یک کانال دارد — پس یک ستون `TEXT` در همان ردیفی که به هر حال خوانده می‌شود، نه
یک JSONB که باید parse شود و داخلش دنبال کانال گشت.

و مثل `Recipient` موقع ساخت مهر می‌خورد، پس تغییر تنظیمات source بعداً نمی‌تواند
delivery های در انتظار را جابه‌جا کند.

`Senders` عمداً به `notification.Request` اضافه **نشد**: `notification` هیچ استفاده‌ای
از آن ندارد، همان‌طور که `Recipients` را به همین دلیل از آن برداشتیم. map از adapter
به service می‌رود و مستقیم به `NewSet`.

## بدون رفت‌وبرگشت دوم

نگرانی درستی مطرح شد: اگر تایپ domain راز نداشته باشد، adapter مجبور است دو بار
query بزند. لازم نیست — adapter حق دارد بیشتر از آنچه به هسته می‌دهد نگه دارد. یک
`SELECT` کل ردیف را می‌آورد، نیمهٔ اول به تایپ domain تبدیل می‌شود و نیمهٔ دوم پیش
خود adapter می‌ماند. شکل port هم این را طبیعی می‌کند: هسته می‌گوید «برای این source
روی این کانال یک sender بده» و adapter همه‌چیز را حل می‌کند.

# Files Changed

- `internal/core/domain/credential/entity.go` *(جدید — `Credential`، `New`، `Restore`، `Pick`)*
- `internal/core/domain/credential/errors.go` *(جدید)*
- `internal/core/domain/credential/entity_test.go` *(جدید — ۲۳ تست)*
- `internal/core/domain/delivery/entity.go` *(`SenderName`، امضای `NewSet`)*
- `internal/core/domain/delivery/types.go` *(`Snapshot.SenderName`)*
- `internal/core/domain/delivery/entity_test.go` *(دو تست تازه)*

# Tests Run

- `make prepush` — سبز: fmt، vet، arch-check، golangci-lint (`0 issues`)، `go test -race ./...`

# Follow-ups / Risks

- اعتبارسنجی نام هویت موقع submit هنوز نیست. بدون آن یک غلط املایی (`marketting`)
  پذیرفته می‌شود و ساعت‌ها بعد به‌صورت `FAILED` با `NO_SENDER` ظاهر می‌شود.
  `delivery` نباید `credential` را import کند، پس جایش service است.
- قاعدهٔ «فقط یک پیش‌فرض به ازای هر `(source, channel)`» در سطح یک entity قابل
  اعمال نیست. جایش یک partial unique index است به‌علاوهٔ یک عملیات در service که
  موقع عوض‌کردن پیش‌فرض، قبلی را پاک کند.
- `NOTIF_CRYPTO_KEY` هنوز در config نیست، و تصمیمی دربارهٔ چرخاندن کلید گرفته نشده.
  توکن بات باید **رمزنگاری** شود نه hash — hash یک‌طرفه است و برای فراخوانی تلگرام
  به مقدار اصلی نیاز داریم. (کلید API خودِ source برعکس: hash می‌شود.)
- `Source.AllowedFrom` لازم نشد. آدرس `From` جزئی از خود credential است، پس source
  هیچ‌وقت یک آدرس تایپ نمی‌کند و جعل ساختاراً ناممکن است.

# Instruction

مالک خواست بعد از entity ی source برویم سراغ credential. طراحی مرحله‌به‌مرحله در
گفت‌وگو شکل گرفت: کجا بنشیند، چرا راز داخلش نرود، چطور بدون query دوم، و کجا فیلد
انتخاب هویت اضافه شود.
