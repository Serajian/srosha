Branch: `fix/nats-permissions-measured`

# Summary

`$JS.API.>` از دسترسیِ gateway و dispatcher برداشته شد و جایش دقیقاً همان subject هایی
نشست که هر کدام **واقعاً** صدا می‌زنند. هر پنج مسیرِ مخربی که هفتهٔ پیش تست شده بود
حالا بسته است و هیچ‌کدام از کارهای واقعی نشکسته. **هیچ کدِ Go ای عوض نشده.**

## ایراد

`$JS.API.>` کلِ سطحِ مدیریتیِ JetStream است. با آن، اعتبارنامهٔ gateway — یعنی همان
باینری‌ای که رو به اینترنت است — می‌توانست:

```
دومین consumer کنارِ dispatcher       رد -- WorkQueue خودش نمی‌گذارد
حذفِ consumer ــِ dispatcher           مجاز
بعد ساختِ consumer ــِ خودش            مجاز
خواندنِ متنِ هر پیام                    مجاز
purge ــِ stream                      مجاز
حذفِ کلِ stream                       مجاز
```

## چطور فهرست درآمد — و چرا این مهم‌ترین بخشِ کار است

**اندازه‌گیری، نه استنتاج.** فهرست از خواندنِ کد درنیامد. یک nats با `trace: true` بالا
آمد و دو برنامهٔ کوچک همان تماس‌هایی را زدند که باینری‌ها می‌زنند — `AccountInfo`،
`CreateOrUpdateStream`، `Publish` برای gateway؛ همان‌ها به‌علاوهٔ
`CreateOrUpdateConsumer`، `Consume` و `Ack` برای dispatcher — و log گفت دقیقاً چه
subject هایی رد شدند.

فهرستی که از روی کد حدس زده بودم **دو غلط داشت**:

- `$JS.API.STREAM.INFO.NOTIFY` را داشت که اصلاً صدا زده نمی‌شود
- `$JSC.CI.>` را **نداشت** — subject ای که ماشینِ `Consume` رویش subscribe می‌کند و
  نامش در هیچ‌جای این repository نیست

آن دومی همان چیزی است که به timeout ــِ مبهم ختم می‌شد؛ همان شکستی که سندِ خودمان
هشدارش را داده بود. یعنی فهرستی که کامل به‌نظر می‌رسد دلیلی بر کامل بودنش نیست.

## اندازه‌گیری‌ها

**gateway** — همیشه `$JS.API.INFO`، `$JS.API.STREAM.UPDATE.NOTIFY`، publish روی
`notify.>`، subscribe روی `_INBOX.>`. اولین اجرا `$JS.API.STREAM.CREATE.NOTIFY` را هم
دارد.

**dispatcher** — همان‌ها بدونِ `notify.>`، به‌علاوهٔ
`$JS.API.CONSUMER.CREATE.NOTIFY.dispatcher.>`،
`$JS.API.CONSUMER.MSG.NEXT.NOTIFY.dispatcher`، `$JS.ACK.NOTIFY.dispatcher.>`، و
subscribe روی `$JSC.CI.>`.

**و یک چیزِ اضافه که برداشته شد:** `subscribe: notify.>` ــِ dispatcher لازم نبود. pull
consumer روی inbox تحویل می‌گیرد و trace هیچ‌وقت subscribe ــِ مستقیم به subject ــِ
کاری نشان نداد.

## اسمِ stream حالا در دسترسی‌ها نشسته

`NOTIFY` است، پیش‌فرضِ `NOTIF_MQ_STREAM`. عوض کردنِ آن متغیر بدونِ عوض کردنِ این خطوط
JetStream را از هر دو باینری می‌گیرد، بی‌صدا. در خودِ فایل با حروفِ درشت نوشته شد.

# Files Changed

- `deployment/infra/nats/nats-server.conf` *(دو بلوکِ دسترسی، و توضیحِ اینکه چطور اندازه گرفته شد و چطور دوباره اندازه گرفته می‌شود)*
- `docs/reference/srosha-infra-deploy.md` *(§۲.۴ که تا حالا این را یک موردِ باز می‌نامید)*

# Tests Run

با **فایلِ واقعیِ repository**، نه کپیِ آزمایشی:

- gateway: `AccountInfo`، `CreateOrUpdateStream`، `Publish` — هر سه ok
- dispatcher: به‌علاوهٔ `CreateOrUpdateConsumer`، `Consume`، و یک پیامِ واقعی که
  دریافت و ack شد
- هر پنج مسیرِ مخرب از طرفِ gateway: **refused**
- dispatcher هم دیگر نمی‌تواند stream را پاک یا purge کند
- بعدش stream و consumer هر دو سرِ جایشان
- `make precommit` — pass

# Follow-ups / Risks

- **هنوز روی سرور اعمال نشده.** فایل باید در File Mount ــِ Dokploy جایگزین و سرویسِ
  nats دوباره deploy شود. برخلافِ تغییرهای قبلی، اگر اینجا چیزی جا افتاده باشد علامتش
  خطای روشن نیست — ارسالِ پیام کار نمی‌کند و در log ــِ nats خطِ
  `Permissions Violation` می‌آید. بعد از deploy یک ارسالِ واقعی امتحان کن.
- این فهرست به نسخهٔ `nats.go v1.53.1` گره خورده. ارتقای کتابخانه می‌تواند subject
  تازه‌ای بیاورد، و شکستش هم بی‌صدا خواهد بود. روشِ اندازه‌گیریِ دوباره در خودِ فایل
  نوشته شد.
- `admin` هنوز همه‌چیز دارد و باید داشته باشد. رمزش تنها رمزی است که هیچ باینری‌ای
  نگهش نمی‌دارد.

# Instruction

موردِ سوم از فهرست: `$JS.API.>` بیش از آنچه لازم است می‌دهد. تنگش کن، ولی با
اندازه‌گیری نه با حدس.
