Branch: `feat/nats-publisher`

# Summary

`delivery.Publisher` از قبل تعریف شده بود و `delivery.Service.Publish` صدایش
می‌زد؛ هیچ پیاده‌سازی‌ای نداشت. حالا دارد، به‌علاوهٔ stream ای که رویش می‌نشیند.

مرز از قبل در کامنت خودِ infra نوشته شده بود و همان رعایت شد:

> stream، subjectهایش و consumerهایش **واژگان این سرویس‌اند** و adapter
> می‌سازدشان، نه اینجا.

## `Nats-Msg-Id` = `DeliveryID`

مسئله‌ای که حل می‌کند:

```
gateway:  js.Publish(...)
              ↓
broker:   ذخیره کرد ✓
              ↓
          ack در راه برگشت گم شد ✗
              ↓
gateway:  خطا دید → دوباره publish کرد
              ↓
broker:   دو پیام → dispatcher دو بار می‌فرستد → مشتری دو پیامک می‌گیرد
```

با شناسه، publish دوم را broker می‌شناسد، ذخیره نمی‌کند و ack می‌دهد.

`DeliveryID` انتخاب شد چون **یک delivery = یک دستور ارسال**. دو رویداد با یک
`DeliveryID` دقیقاً یک چیز می‌گویند.

`TestTheSameDeliveryPublishedTwiceIsStoredOnce` روی broker واقعی سه بار publish
می‌کند و می‌شمارد: یک پیام.

**و یک وابستگی که باید ثبت شود:** این فقط تا وقتی امن است که **هیچ‌کس دیگری یک
delivery را دوباره publish نکند**. ARCHITECTURE می‌گوید recovery *می‌فرستد* نه
اینکه دوباره publish کند. روزی که آن عوض شود، این شناسه تبدیل به تله می‌شود — یک
publish عمدی بی‌صدا دور انداخته و موفقیت خوانده می‌شود. در کامنت `Publish` نوشته
شده.

## subject: یک تایپ، نه یک ثابت

اولین نسخه `notify` را مستقیم داخل تابع نوشته بود. یعنی روزی که stream دوم لازم
شود، ریشه در دو جا نوشته می‌شد — یک بار برای ساختن subject و یک بار برای wildcard
که stream را می‌گیرد — و آن دو می‌توانستند با هم اختلاف پیدا کنند.

حالا یک تایپ است که ریشه را نگه می‌دارد:

```go
type Subjects struct{ root string }

NewSubjects(root)  →  ریشه را اعتبارسنجی می‌کند
Wildcard()         →  root + ".>"      ← عمومی
ForDispatch(e)     →  root.channel.priority  ← مخصوص این رویداد
```

stream دوم یعنی یک `Subjects` دوم با ریشهٔ خودش و متد `For` خودش. هر چیزی که عمومی
است اینجاست؛ فقط `ForDispatch` می‌داند dispatch event چیست.

`NewSubjects` ریشه‌ای را که **یک token واحد نیست** رد می‌کند — نقطه، wildcard،
فاصله. هیچ‌کدام موقع publish با صدا شکست نمی‌خورند؛ بی‌سروصدا عوض می‌کنند که stream
چه چیزی را می‌گیرد. و مقدار صفر (`Subjects{}`) که `".>"` می‌ساخت و **کل broker** را
می‌گرفت، در `NewPublisher`، `EnsureStream` و `ForDispatch` رد می‌شود.

`StreamConfig` هم `Subjects` را می‌گیرد نه اینکه از `Name` بسازد: اسم stream و
subjectهایی که می‌گیرد دو تصمیم جدا هستند، و گره زدنشان یعنی تغییر نام یکی، بی‌صدا
آن یکی را جابه‌جا کند.

## شکل subject: `<root>.<channel>.<priority>`

کامنت `DispatchEvent` از قبل گفته بود چرا این دو فیلد آنجا هستند: «چون subject از
رویشان ساخته می‌شود». حالا واقعاً ساخته می‌شود:

```
notify.email.*      →  یک worker pool برای SMTP، یکی دیگر برای Telegram
notify.*.critical   →  اگر روزی صف جدا برای فوری‌ها خواستیم
notify.>            →  subject خودِ stream
```

channel اول است چون تقسیم محتمل‌تر همان است: providerها محدودیت نرخ کاملاً
متفاوتی دارند، در حالی که ترتیب اولویت برای همه یکی است.

`ForDispatch` یک channel یا priority ناشناخته را **رد می‌کند**. بدون آن، رویداد به
`notify..normal` می‌رفت: broker قبولش می‌کرد و هیچ‌کس نمی‌خواندش — بدترین حالت،
چون نه خطایی هست نه ارسالی.

## stream یک هویت است، نه یک اسم

`Stream` اسم و namespace را با هم نگه می‌دارد:

```go
type Stream struct {
	Name     string
	Subjects Subjects
}
```

چون این دو همیشه با هم سفر می‌کنند و جدا از هم بی‌معنی‌اند — اسم نمی‌گوید چه چیزی
داخلش می‌افتد، و namespace نمی‌گوید کدام stream نگهش می‌دارد. اگر جدا گرفته می‌شدند،
می‌شد اسم یک stream را با subjectهای یکی دیگر به کسی داد.

`DispatchStream(name)` تنها جایی است که `DispatchRoot` نوشته می‌شود: **اسم پیکربندی
می‌شود** چون اپراتور broker خودش را نام‌گذاری می‌کند، **namespace نمی‌شود** چون
پروتکل این سرویس است و هر دو باینری باید بدون گفتن سرش توافق داشته باشند.

## publisher: نصف عمومی، نصف مخصوص

`Publish` هر دو را در یک تابع داشت. یک publisher دوم برای هر رویداد دیگری باید کل
فایل را کپی می‌کرد تا یک خط را عوض کند:

```
subject از ForDispatch          ← مخصوص dispatch
msg id از DeliveryID            ← مخصوص dispatch
─────────────────────────────
encode                          ← عمومی
publish با msg id + expect stream + انتظار ack   ← عمومی
```

حالا جدا شده‌اند: `DispatchPublisher.Publish` فقط دو تصمیم مخصوص را می‌گیرد و به
`send` می‌دهد، و `send` هیچ چیز دربارهٔ اینکه چه چیزی منتشر می‌شود نمی‌داند. `encode`
هم `any` می‌گیرد.

اسم تایپ هم صریح شد: `DispatchPublisher` نه `Publisher` — چون خودِ port
(`delivery.Publisher`) یک `DispatchEvent` می‌گیرد و این publisher به‌حکم قرارداد
مخصوص همان است.

## و publish حالا stream را نام می‌برد

یک publish به یک **subject** فرستاده می‌شود و broker تصمیم می‌گیرد کدام stream
بگیردش. همان تصمیم خطر است:

```
stream دومی روی همان namespace ساخته شود
        ↓
رویدادهای ما را می‌بلعد
        ↓
publish ack می‌گیرد، هیچ چیز غلط به نظر نمی‌رسد
        ↓
و هیچ consumer ما هیچ‌وقت آن پیام را نمی‌بیند
```

`jetstream.WithExpectStream(name)` این را به یک رد صریح تبدیل می‌کند.
`TestAPublishIntoTheWrongStreamIsRefused` همین را می‌سازد: subject درست، اسم
stream غلط — broker ردش می‌کند و stream خالی می‌ماند.

## stream: WorkQueue، File، ۲۴ ساعت

**WorkQueue** — پیام بعد از ack حذف می‌شود، و broker فقط یک consumer روی هر
subject می‌پذیرد. یعنی «یک delivery، یک worker» از یک امید به یک تضمین تبدیل
می‌شود. هزینه‌اش: consumer دوم (مثلاً audit) بدون رفتن به Interest ممکن نیست.
چیزی هم لازمش ندارد — جدول `deliveries` منبع حقیقت است و stream فقط تحویل کار.

**File** — رویداد وجود دارد چون ردیف‌ها از قبل نوشته شده‌اند و کسی باید خبردار
شود. broker ای که با restart فراموششان کند، همه را برای recovery باقی می‌گذارد.

**MaxAge ۲۴ ساعت** — فقط ترمز پشتیبان است، نه قانون درستی. فکر می‌کردم پیام کهنه
خطرناک است (dispatcher بیدار شود و ردیفی را بفرستد که recovery نیم ساعت پیش
`FAILED` کرده) ولی `Handle` از قبل بسته است:

```go
if del.IsSettled() {
    return nil
}
```

پس MaxAge فقط جلوی رشد بی‌انتهای stream ای را می‌گیرد که کسی خالی‌اش نمی‌کند. و
`LoadMQ` چک می‌کند که از پنجرهٔ تکراری بلندتر باشد — کوتاه‌تر بودنش یعنی پیام حذف
شود در حالی که broker هنوز از ذخیرهٔ دوباره‌اش امتناع می‌کند.

## publish منتظر ack می‌ماند

fire-and-forget سریع‌تر بود، ولی شکست publish **عمداً** غیرکشنده است: ردیف
`PENDING` رکورد این است که باید فرستاده شود و recovery پیدایش می‌کند. این فقط وقتی
درست است که شکست **واقعاً گزارش شود**. یک `Publish` که nil برگرداند و پیام را گم
کند، ردیف را تا دقایق بعد بی‌صاحب می‌گذارد.

## دو کامنت غلط که سر راه پیدا شد

`.env.example` می‌گفت:

> Recovery republishes a delivery it is not sure about, and this window is what
> makes that safe.

و `settings/mq.go` چیزی شبیه همان. هر دو با ARCHITECTURE در تناقض بودند — recovery
**نمی‌فرستد به صف**، خودش می‌فرستد. هر دو اصلاح شدند.

# Files Changed

- `internal/adapter/mq/nats/subjects.go` *(تازه — `Subjects`، `NewSubjects`، `Wildcard`، `ForDispatch`)*
- `internal/adapter/mq/nats/codec.go` *(تازه — `encode`، عمومی روی `any`)*
- `internal/adapter/mq/nats/stream.go` *(تازه — `Stream`، `NewStream`، `DispatchStream`، `EnsureStream`)*
- `internal/adapter/mq/nats/dispatch_publisher.go` *(تازه — `DispatchPublisher` و `send` عمومی)*
- `internal/adapter/mq/nats/const.go` *(تازه — `DispatchRoot` و token های wildcard)*
- `internal/adapter/mq/nats/{subjects,dispatch_publisher}_test.go` *(تازه — ۹ واحد، ۶ یکپارچگی)*
- `internal/config/settings/mq.go` *(`MaxAge` + چک، و کامنت اصلاح‌شده)*
- `docs/CONFIG.md` و `.env.example` *(`NOTIF_MQ_MAX_AGE`، پیش‌فرض `24h`)*
- `Makefile` *(متن راهنمای `test-integration`: دیتابیس **و** broker)*

# Tests Run

- `go test ./internal/adapter/mq/nats/` — ۹ تست واحد، سبز
- `go test -tags integration ./internal/adapter/mq/nats/` — ۶ تست روی broker
  واقعی، سبز (dedup با یک fake ثابت نمی‌شود؛ آن dedup مالِ broker است)
- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

# Follow-ups / Risks

- `decode` نوشته نشده. صدازننده‌اش `consumer.go` است که هنوز stub است؛ فرمت سیم
  فعلاً با تستی که دقیقاً همان json را ادعا می‌کند بسته شده.
- هیچ‌کس هنوز `EnsureStream` یا `NewDispatchPublisher` را صدا نمی‌زند. سیم‌کشی در
  bootstrap کار بعدی است.
- `Subscriber` هنوز port ندارد؛ با consumer می‌آید.
- WorkQueue یعنی consumer دوم بدون رفتن به Interest ممکن نیست.

# Instruction

«برویم nats publisher» — با چهار تصمیم: `Nats-Msg-Id` برابر `DeliveryID`،
subject به شکل `notify.<channel>.<priority>`، retention برابر WorkQueue، و
`MaxAge` برابر ۲۴ ساعت. به‌علاوهٔ تأیید `CreateOrUpdateStream` برای اینکه هر دو
باینری stream را idempotent بسازند.
