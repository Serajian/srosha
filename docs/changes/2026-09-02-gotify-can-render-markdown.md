Branch: `feat/gotify-can-render-markdown`

# Summary

Gotify حالا می‌تواند مارک‌داون نشان بدهد، و SDK بالاخره می‌گذارد قالبِ هر کانال از کد
تنظیم شود. **هیچ رفتارِ موجودی عوض نشده** — همه‌چیز پیش‌فرض همان متنِ خام است.

## چه چیزی پیدا شد

یک سرویسِ مصرف‌کننده درخواستِ یک فیلدِ `Format` روی `Message` داد. موقعِ بررسی معلوم
شد وضع از چیزی که هر دو طرف فکر می‌کردیم عجیب‌تر است:

```
telegram, bale   parse_mode     در سرور هست، در SDK نیست
email            content_type   در سرور هست، در SDK نیست
gotify           —              در هیچ‌کدام نیست
matrix           —              در هیچ‌کدام نیست
```

یعنی سه کانال از قبل قالب می‌فهمیدند و **از کد قابلِ تنظیم نبودند**: تایپ‌های SDK
فیلدش را نداشتند، پس مقدار اصلاً واردِ سندِ تنظیمات نمی‌شد. تنها راهش `RawCredential`
بود یا پورتال، دستی.

## Gotify

روی سیم فقط `title` و `message` می‌رفت. حالا وقتی `content_type` مارک‌داون باشد:

```json
{"title":"…","message":"…",
 "extras":{"client::display":{"contentType":"text/markdown"}}}
```

**خام هیچ `extras` ای نمی‌فرستد**، نه یک `text/plain` صریح. آن پیش‌فرضِ خودِ Gotify
است، و گفتنش یعنی روی هر پیامی که این سرویس تا امروز فرستاده یک کلیدِ اضافه بگذاریم
تا چیزی را بگوییم که از قبل درست بود.

## و یک تفاوت که ارزشِ نوشتن داشت

`ContentType` ــِ Gotify **بی‌خطر** است، به شکلی که `ParseMode` نیست:

```
telegram   نشانه‌گذاریِ خراب → 400 → SendPermanent، پیام هرگز نمی‌رسد
gotify     نشانه‌گذاریِ خراب → همان‌طور که هست نشان داده می‌شود
```

پس Gotify را می‌شود روی هویتی گذاشت که متنِ تایپ‌شدهٔ یک آدم را می‌برد، و Telegram را
نه. این در هر سه جا نوشته شد: تایپِ SDK، README، و کامنتِ `Config`.

## SDK

چهار فیلد، که هیچ‌کدام تغییری در سرور یا proto نمی‌خواهند چون سرور از قبل
می‌خواندشان:

```
TelegramCredential.ParseMode
BaleCredential.ParseMode
SMTPCredential.ContentType
GotifyCredential.ContentType
```

`Settings()` هرکدام وقتی خالی باشد آن کلید را **نمی‌فرستد** — نه رشتهٔ خالی. سرور
نبودنِ کلید را «خام» می‌فهمد و یک `""` مقداری است که کسی انتخابش نکرده.

# Files Changed

- `internal/adapter/sender/gotify/api.go` *(`extras`، با تایپِ نام‌دار نه map)*
- `internal/adapter/sender/gotify/config.go`، `const.go` *(`content_type` و اعتبارسنجی‌اش)*
- `internal/adapter/sender/gotify/sender.go`
- `internal/adapter/sender/gotify/sender_test.go` *(سه تست)*
- `sdk/go/srosha/credential_types.go` *(چهار فیلد)*
- `sdk/go/srosha/example_test.go`، `README.md`، `README.fa.md`، `public/guarded/portal/sdk.md`

# Tests Run

- خام: **هیچ `extras` ای روی سیم نیست** — با `Config{}` و با `text/plain`
- مارک‌داون: شکلِ دقیقی که Gotify مستند کرده، و بدنه دست‌نخورده (`**it is done**`)
- `content_type` ــِ ناشناخته: موقعِ **ثبت** رد می‌شود نه اولین ارسال
- یازده تستِ موجودِ gotify: همه سبز
- `make prepush` — pass

# Follow-ups / Risks

- **matrix تنها کانالی است که قالب می‌فهمد و ما نمی‌فرستیم.** `formatted_body` و
  `format: org.matrix.custom.html` می‌خواهد. همان قالب، یک adapter دیگر.
- **`parse_mode` ــِ Bale هنوز مقابلِ سرورِ واقعی امتحان نشده.** کد می‌فرستدش؛ اینکه
  Bale محترمش می‌شمارد یا نه معلوم نیست. دکمهٔ Send a test در پورتال سی ثانیه‌ای
  جوابش را می‌دهد.
- `Format` روی `Message` ساخته **نشد**، عمداً: یک فیلدِ واحد قول می‌دهد یک متن روی هشت
  کانال یک‌جور در بیاید، و MarkdownV2 متنی را رد می‌کند که CommonMark قبول دارد. اگر
  روزی لازم شد، سطحش `Route` است نه `Message`.

# Instruction

gotify را برای مارک‌داون درست کن و بفرست، بعد جوابِ آن سرویس را بنویس.
