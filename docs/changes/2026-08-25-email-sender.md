Branch: `feat/email-sender`

# Summary

**کانال سوم، و اولین کانالی که فقط یک POST نیست.** SMTP یک تکنولوژی است، پس مثل
هر تکنولوژی دیگری در `infra` نشست:

```
infra/httpclient  →  registry.SenderClient  →  adapter/sender/telegram
infra/smtp        →  registry.SMTPDialer    →  adapter/sender/email
```

## این را اول اشتباه ساختم

همه‌اش را داخل `adapter/sender/email` نوشتم — go-mail، TLS، auth، همه. غلط بود و
`CONVENTIONS.md` صریح می‌گوید چرا:

> *«`internal/registry/` تنها package ای است که یک تکنولوژی را باز می‌کند...
> بقیه آنچه را باز شده دریافت می‌کنند.»*

پس adapter کلاینت نمی‌سازد. **dialer را دریافت می‌کند.**

## چرا dialer و نه client

اینجا SMTP از HTTP جدا می‌شود:

```
http.Client   یکی است، بین همهٔ providerها مشترک
mail client   یک حساب روی یک سرور — و هر source ممکن است مالِ خودش را بیاورد
```

پس registry چیزی را باز می‌کند که **کلاینت می‌سازد**، نه یک کلاینت. و چیزی برای
بستن ثبت نمی‌شود، چون چیزی نگه داشته نمی‌شود: SMTP اتصالی ندارد که بین دو پیام
ارزش نگه‌داشتن داشته باشد — سرور خودش نشست بیکار را می‌بندد — پس هر ارسال یک بار
dial می‌کند، تحویل می‌دهد و قطع می‌کند.

## مرز بین دو لایه

```
infra/smtp             چطور گفت‌وگو انجام می‌شود
                       کدام پورت یعنی کدام رمزنگاری · کدام auth · چقدر وقت
                       و کد پاسخ را از متن بیرون می‌کشد

adapter/sender/email   آن کد یعنی چه
                       ۵xx دائمی · ۴xx موقت · بی‌کد یعنی هرگز نرسید
```

خواندنِ کد کارِ تکنولوژی است؛ تصمیم دربارهٔ retry یک حرف است دربارهٔ **اینکه
srosha چطور می‌فرستد**، نه دربارهٔ اینکه mail چطور کار می‌کند.

و adapter هیچ نوعی از infra قرض نمی‌گیرد تا کد را بخواند — یک interface کوچک
اعلام می‌کند:

```go
var coded interface{ ReplyCode() int }
errors.As(err, &coded)
```

پس یک transport دومِ mail می‌تواند همان سؤال را جواب بدهد بدون اینکه اینجا چیزی
عوض شود.

## چرا کتابخانه، نه `net/smtp`

```
Subject: استقرار تمام شد
```

باید طبق RFC 2047 به `=?UTF-8?B?...?=` تبدیل شود. `net/smtp` هیچ‌چیز برای MIME
ندارد — header ها دستی ساخته می‌شوند، و در این پروژه فارسی **حالت عادی است نه
استثنا**. یعنی هر subject یک فرصت برای خراب شدن بی‌صدا.

`go-mail` هزینهٔ ماژول تازه هم نداشت: `golang.org/x/crypto` و `golang.org/x/text`
هر دو از قبل در `go.mod` بودند.

## رمزنگاری اختیاری نیست

`TLSMandatory` همیشه، و ۴۶۵ یعنی TLS از اولین بایت. سروری که رمز نکند سروری است
که این رمز عبور به آن نمی‌رود.

تست‌ها این را دور نمی‌زنند — **STARTTLS واقعی** با گواهی‌ای که خودشان یک لحظه قبل
ساخته‌اند. و دلیلش عملی است: تستی که رمزنگاری را خاموش کند اصلاً به مسیر auth
نمی‌رسد، چون هیچ مکانیزمی رمز عبور را روی اتصال باز نمی‌فرستد. کتابخانه درست
رفتار می‌کند و تست باید همان مسیر را برود.

## `providerMessageID` = همان Message-ID

SMTP چیز قابل اتکایی برنمی‌گرداند: بعضی سرورها یک queue id در خط ۲۵۰ می‌گذارند و
بعضی نه، و شکلش استاندارد نیست. `Message-ID` تنها دستگیره‌ای است که **هر دو طرف**
دارند، چون header خودِ ایمیل است.

# Files Changed

- `internal/infra/smtp/{smtp,errors}.go` *(تازه — Dialer · Client · Identity · Message)*
- `internal/infra/smtp/{smtp,server}_test.go` *(تازه — سرور SMTP کوچک با STARTTLS واقعی)*
- `internal/registry/smtp.go` *(تازه — settings → DialerConfig)*
- `internal/adapter/sender/email/{sender,config,errors,const}.go` *(تازه)*
- `internal/adapter/sender/email/sender_test.go` *(تازه)*
- `internal/adapter/sender/registry.go` *(`Fallback.SMTP`، شاخهٔ email، dialer)*
- `internal/adapter/sender/registry_test.go` *(هر سه کانال از یک جدول)*
- `internal/bootstrap/dispatcher.go` *(dialer در داستان)*
- `go.mod` *(`wneessen/go-mail`)*

هیچ کلید تازه‌ای در کانفیگ نیست: `NOTIF_SENDER_SMTP_*` از روز اول بود.

# Tests Run

- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

`infra/smtp` روی یک سرور SMTP واقعی آزموده می‌شود که تست خودش بالا می‌آورد —
STARTTLS، auth، و هر کد پاسخی که در تولید نمی‌شود ساخت:

```
۵۵۰ · ۵۵۴ · ۵۵۲ · ۵۳۵      کد درست بیرون می‌آید
۴۵۰ · ۴۲۱ · ۴۵۲            همین‌طور
سروری که STARTTLS ندارد    رد می‌شود، نه اینکه باز حرف بزند
سروری که نیست              بدون کد
relay بدون حساب            AUTH اصلاً نمی‌فرستد
subject فارسی              می‌رود
```

و روی سرویس زنده، با یک SMTP واقعی محلی:

```
Register  CHANNEL_EMAIL با host/port/username/from  →  سربسته ذخیره شد
Submit    subject فارسی، route: email
                    ↓
dispatcher  consume → credential → Material → dialer → TLS
                    ↓
گواهی قابل تأیید نبود  →  بدون کد  →  transient  →  ردیف PENDING ماند و دوباره تلاش شد
```

که دقیقاً رفتار درست است: به سروری که تأیید نمی‌شود پیام داده نشد، و شکستش دائمی
حساب نشد. **ارسال موفق زنده آزموده نشد** — گواهی محلی خودامضا بود و trust کردنش
روی این ماشین کار خودش را می‌خواست. مسیر موفق در تست‌های `infra/smtp` روی یک
گفت‌وگوی SMTP واقعی پوشش دارد.

دادهٔ تستی و کانتینر sink بعدش پاک شدند.

# Follow-ups / Risks

- **ارسال موفق فقط در تست آزموده شده، نه روی سرویس زنده.** برای آن یک SMTP با
  گواهی معتبر لازم است.
- `Sender` هنگام ساخته‌شدن یک بار `Open` می‌کند تا هویت را بسنجد. چون dialer
  چیزی باز نمی‌کند این ارزان است، ولی اگر روزی dialer وصل شود، این هزینه‌دار
  می‌شود.
- `whatsapp` تنها کانال باقی‌مانده است.
- SMTP هیچ اتصالی بین پیام‌ها نگه نمی‌دارد. برای حجم بالا روی یک سرور، یک کلاینت
  زنده ارزش دارد — ولی آن‌وقت باید نشستی را که سرور خودش می‌بندد مدیریت کند.

# Instruction

«برو email» — با سه تصمیم: کتابخانه به‌جای `net/smtp`، `Message-ID` خودمان
به‌عنوان `providerMessageID`، و هر دو `text/plain` و `text/html`. و بعد تصحیح
مهم‌تر: **اول در `infra` پیاده شود، مثل بقیه، و از آنجا registry.**
