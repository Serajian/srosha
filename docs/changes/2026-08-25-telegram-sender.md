Branch: `feat/senders`

# Summary

**اولین کانال واقعی.** یک پیام حالا می‌تواند از srosha بیرون برود — و
`secret.Keeper.Material` برای اولین بار در تولید صدا زده می‌شود، نه فقط در تست.

```
usecase.deliver
      ↓
sender.Registry.For(sourceID, channel, name)
      ↓
   credential.Resolve      کدام هویت؟
   secrets.Material        باز کردن + reseal
   telegram.New(client, token, config)
      ↓
   Send(Message) → message_id
```

## سه جواب که یکی نیستند

این ظریف‌ترین قسمت این commit است. وعدهٔ `.env.dispatcher.example` این بود:
*«اعتبارنامهٔ خودِ srosha، وقتی source مال خودش را نیاورده»* — ولی «نیاورده» سه
معنی داشت که یکی نبودند:

```
هیچ چیزی ثبت نکرده           →  مالِ ما
یکی ثبت کرده و خاموشش کرده   →  NO_SENDER
اسمی خواسته که وجود ندارد    →  NO_SENDER
```

دو مورد آخر عمداً به fallback نمی‌رسند:

- پیامی که گفته «از bot خودمان» و از bot ما برود، **بدتر از پیامی است که شکست
  بخورد** — چون شکست را می‌بینند و این را نمی‌بینند.
- خاموش‌کردن یک هویت یک **تصمیم** بود. جانشین‌شدن به‌جایش آن تصمیم را بی‌صدا لغو
  می‌کند.

برای اینکه adapter بتواند «هیچی ثبت نشده» را از «default فعالی نیست» تشخیص دهد،
`credential.Pick` حالا دو sentinel جدا برمی‌گرداند:

```
ErrNoCredentials   لیست خالی است           →  می‌شود جانشین گذاشت
ErrNoDefault       چیزی هست، هیچ‌کدام آماده نیست  →  نمی‌شود
```

هر دو `InvalidInput` اند، پس رفتار پیش‌فرض عوض نشده — فقط حالا قابل تفکیک است.

## متن ساده پیش‌فرض است، و این یک تصمیم است

Telegram یک فیلد `text` دارد و یک `parse_mode`. اگر پیش‌فرض را `HTML` بگذاریم،
**هر پیامی که `<` یا `&` داشته باشد ۴۰۰ می‌گیرد** — یعنی یک permanent failure
به‌خاطر علامت‌گذاری خودِ متن.

```
parse_mode خالی   →  هیچ کاراکتری معنی خاص ندارد        ← پیش‌فرض
parse_mode=HTML   →  متن دست‌نخورده می‌رود، escape با خودِ source
```

و اگر source نشانه‌گذاری خواسته، متنش را **escape نمی‌کنیم** — چون escape کردن
دقیقاً همان نشانه‌گذاری‌ای را می‌شکند که خواسته بود.

عنوان هم هیچ قالبی نمی‌گیرد: در متن ساده قالبی نیست، و در حالت markup هرچه
اینجا اضافه شود باید در برابر بدنه‌ای escape شود که عمداً دست نزده‌ایم.

## یک نشتی که تست پیدا کرد

`TestTheTokenNeverReachesAnError` اول **رد شد**، و درست رد شد:

```
telegram call: Post "http://127.0.0.1:59510/bot123456:AAH-not-a-real-token/sendMessage":
                        └─ توکن کامل، در یک خطای معمولیِ dial
```

Bot API اعتبارنامه را در **path** می‌گذارد نه در header، پس url این sender
**خودش راز است** — و `net/http` کل url را در خطای خودش نقل می‌کند. یعنی هر
شکست شبکه یک توکن کارآمد را در لاگ می‌گذاشت.

دو لایه جوابش شد: `*url.Error` باز می‌شود و فقط خطای داخلی می‌ماند، و بعد هر
چیزی که از آن رد شود با `redact` جایگزین می‌شود.

## و همان path یک در دیگر هم بود

`gosec` روی `client.Do` علامت SSRF زد و حق داشت: توکن از دیتابیس می‌آید و در
path می‌نشیند. `usableToken` حالا آن را به الفبایی محدود می‌کند که نمی‌تواند از
path بیرون بزند:

```
1:a/../../x   1:a b   1:a?x=1   1:a#f   →  رد
```

host هم ثابت است و از config خوانده نمی‌شود — آدرسی که بشود جای دیگری نشانه‌اش
گرفت، آدرسی است که می‌شود به سمت کسی که توکن جمع می‌کند نشانه گرفت.

## طبقه‌بندی خطا: تنها سؤالی که core می‌پرسد

```
429            transient + RetryAfter از خودِ API (یا ۳۰ ثانیه)
5xx            transient          مالِ آن‌هاست
4xx            permanent          chat not found · blocked · bad token
ok:false + 200 transient          مستند نیست، پس نهایی اعلامش نمی‌کنیم
شبکه           transient          هیچ‌چیزش دربارهٔ این پیام نیست
طولانی‌تر از ۴۰۹۶  permanent      و اصلاً فرستاده نمی‌شود
```

اشتباه در هر دو جهت گران است: یک شکست دائمی که transient حساب شود تا آخر
MaxDeliver صف را اشغال می‌کند، و یک شکست موقت که permanent حساب شود پیامی را
دور می‌اندازد که یک دقیقه بعد می‌رفت.

# Files Changed

- `internal/adapter/sender/telegram/{sender,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/telegram/{sender,export}_test.go` *(تازه — ۱۷ تست)*
- `internal/adapter/sender/registry.go` *(تازه — `delivery.SenderRegistry`)*
- `internal/adapter/sender/registry_test.go` *(تازه — ۸ تست)*
- `internal/core/domain/credential/errors.go` *(`ErrNoCredentials`)*
- `internal/core/domain/credential/entity.go` *(`Pick` دو حالت خالی را جدا می‌کند)*
- `internal/core/domain/credential/entity_test.go`

# Tests Run

- `make prepush` — سبز
- `golangci-lint run ./internal/... ./pkg/...` — صفر ایراد

هیچ تستی به Telegram واقعی نمی‌زند: `httptest` جای Bot API را می‌گیرد و هر
شکل جوابی را می‌سازد که در تولید نمی‌شود ساخت — ۴۲۹ با و بدون `retry_after`،
`ok:false` با ۲۰۰، سروری که اصلاً جواب نمی‌دهد.

# Follow-ups / Risks

- **هر ارسال دو query است** — یکی برای انتخاب هویت، یکی برای باز کردنش. cache
  عمداً نوشته نشد؛ لازم شود، به یک لایهٔ invalidation نیاز دارد که بفهمد کِی یک
  credential خاموش یا عوض شده.
- **هنوز هیچ‌کس registry را صدا نمی‌زند.** `dispatcher.go` سیم‌کشی نشده و
  `webhook.Notifier` هنوز stub است. بعد از notifier می‌شود کل dispatcher را وصل کرد.
- `bale` تقریباً یک کپی است (همان Bot API)، `email` گران‌ترین.
- `maxTextLen` به rune می‌شمارد و Telegram به UTF-16 — یک emoji اینجا یکی و
  آنجا دو تا. حالت عادی را می‌گیرد؛ وقتی اشتباه باشد ۴۰۰ می‌آید که permanent است.

# Instruction

«برویم senderها» با سه تصمیم: **telegram اول** (بعد bale، بعد email)، **بدون
cache** فعلاً، و **هر provider خودش json خودش را parse کند**.
