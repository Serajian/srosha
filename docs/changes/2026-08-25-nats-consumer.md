Branch: `feat/nats-consumer`

# Summary

**سمت خواندنِ صف نوشته شد.** publisher قبلاً پیام را روی stream می‌گذاشت و هیچ‌کس
برنمی‌داشت؛ حالا `nats.Consumer` آن را برمی‌دارد و به core می‌دهد.

## adapter ای که هیچ port ای پیاده نمی‌کند

`Consumer` یک **driving adapter** است — صدا می‌زند، نه اینکه صدا زده شود. دقیقاً
مثل سرور gRPC، پس چیزی برای وارونه‌کردن ندارد و interface ای پیاده نمی‌کند:

```
broker  →  Consumer.handle  →  Dispatcher.Handle(id, attempt)  →  core
```

و `Dispatcher` همین‌جا اعلام شده، نه import از usecase:

```go
type Dispatcher interface {
    Handle(ctx context.Context, id shared.ID, attempt int) error
}
```

یک متد لازم دارد، پس interface مالِ صداکننده است. `usecase.Dispatcher` بدون
اینکه بداند، آن را برآورده می‌کند.

## سه راه خروج، و هیچ راه چهارمی

هر پیام دقیقاً یکی از سه پاسخ را می‌گیرد. پیامی که هیچ‌کدام را نگیرد تا انقضای
`AckWait` می‌نشیند و دوباره تحویل می‌شود — که مثل پیامی به نظر می‌رسد که کسی
نتوانست انجامش دهد، نه پیامی که کسی جوابش را نداد:

```
metadata خراب    →  Term   دفعهٔ بعد هم jetstream نمی‌شود
decode نشد       →  Term   با نسخه‌ای نوشته شده که دیگر نمی‌خوانیم
Handle خطا داد   →  Nak    با تأخیر
موفق             →  Ack
```

## تأخیر را خودمان حمل می‌کنیم

این ظرافتِ اصلی این commit است. `BackOff` روی consumer فقط برای پیام‌هایی اعمال
می‌شود که **هرگز پاسخ نگرفتند**. پیامی که صریحاً `Nak` شود کل جدول را رد می‌کند و
فوراً دوباره می‌آید:

```
بدون تأخیر  →  provider مرده  →  حلقه به سرعت چرخش CPU می‌چرخد
با تأخیر    →  NakWithDelay(backoff[attempt-1])
```

پس همان جدول در `const.go` روی هر دو مسیر استفاده می‌شود — یکی را broker خودش
اعمال می‌کند، دیگری را ما با پیام می‌فرستیم:

```
5s → 30s → 2m → 10m      آخرین فاصله برای باقیِ تلاش‌ها تکرار می‌شود
```

و پیش از ساخت consumer به `MaxDeliver` بریده می‌شود، چون broker یک consumer با
فاصله‌های بیشتر از تعداد تلاش‌ها را رد می‌کند.

## `MaxAttempts` عمداً یک عدد است

سقف broker و سقف core **همان عدد** اند، و این تصادفی نیست:

```
broker زودتر تسلیم شود  →  ردیف PENDING می‌ماند و هیچ نتیجه‌ای رویش نوشته نمی‌شود
broker دیرتر تسلیم شود  →  delivery ای که FAILED شده باز پیشنهاد می‌شود
```

## pull، و اینکه چطور گفته می‌شود

سؤالی که وسط کار پرسیده شد: کجا set شده pull باشد؟ **هیچ‌جا** — یک consumer فقط
وقتی push است که `DeliverSubject` داشته باشد، پس این انتخاب با **نبودِ** یک فیلد
بیان می‌شود، که برای تصمیمی به این وزن روش بدی است.

دو چیز آن سکوت را پر کرد: یک بلوک کامنت که می‌گوید pull است و چرا، و
`TestTheConsumerIsPullNotPush` که از خودِ broker می‌پرسد.

و یک اشتباه مفهومی هم صاف شد: **pull با WorkQueue یکی نیست.**

```
STREAM   → Retention   پیام کِی پاک می‌شود      WorkQueue / Limits / Interest
CONSUMER → Pull|Push   کی شروع‌کننده است        ما می‌خواهیم / سرور می‌فرستد
```

مستقل‌اند و هر چهار ترکیب ممکن است. ولی یک تماس واقعی دارند: WorkQueue فقط یک
consumer روی هر subject می‌پذیرد، و pull باعث می‌شود چند instance به همان یکی وصل
شوند و broker خودش کار را تقسیم کند — scale out یعنی یک container دیگر، بدون هیچ
کانفیگ تازه‌ای.

## خاموشی: Drain نه Stop

```
Drain  پیامی که در دستِ کار است تمام می‌کند و ack می‌دهد
Stop   وسط کار می‌بُرد  →  بعد از restart دوباره تحویل داده می‌شود
```

context مهلت آن انتظار را می‌بندد، و لغو `c.running` به خودِ کار می‌گوید وانمود
نکند وقت دارد. `Stop` دو بار صدا زدن امن است، چون مسیرهای خاموشی هم را قطع
می‌کنند.

هر پیام هم با `AckWait` بسته می‌شود: بعد از آن، broker همان پیام را به کس دیگری
داده و ادامه‌دادن یعنی ارسال دوم از یک delivery.

## `registry.Consumer` در tierServer

بالاترین tier، هرچند socket نیست: **کار ورودی** است و باید پیش از broker ای که به
آن ack می‌دهد و pool ای که در آن می‌نویسد بایستد — دقیقاً مثل listener ای که اول
از پذیرفتن می‌ایستد.

# Files Changed

- `internal/adapter/mq/nats/consumer.go` *(تازه — `Consumer`، `ConsumerConfig`، `Dispatcher`)*
- `internal/adapter/mq/nats/consumer_test.go` *(تازه — integration، روی broker واقعی)*
- `internal/adapter/mq/nats/codec.go` *(`decode`، که DeliveryID صفر را رد می‌کند)*
- `internal/adapter/mq/nats/const.go` *(`dispatchConsumer`، جدول `backoff`)*
- `internal/adapter/mq/nats/subjects_test.go` *(پوشش بیشتر)*
- `internal/registry/consumer.go` *(تازه — settings → ConsumerConfig، و ثبت stop)*
- `internal/config/settings/dispatch.go` *(`AckWait`، `MaxInFlight` با guard)*
- `docs/CONFIG.md` *(ردیف `dispatch`)*
- `.env.dispatcher.example` *(`ACK_WAIT`، `MAX_IN_FLIGHT`)*

# Tests Run

- `go test -tags=integration ./internal/adapter/mq/nats/` — پاس (~۲۵ ثانیه، broker واقعی)
- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد

پنج تست، و هر پنج‌تا خاصیتی را می‌سنجند که فقط broker واقعی می‌تواند اثبات کند:

```
APublishedEventReachesTheCore       پیام از publisher تا Handle می‌رسد
AnErrorAsksAgainAfterWaiting        فاصلهٔ دو تلاش ≥ backoff[0]  →  NakWithDelay کار می‌کند
TheBrokerStopsAtTheLimit            MaxDeliver واقعاً سقف است
AnUndecodableMessageIsNotAskedFor…  Term، نه چرخیدن
TheConsumerIsPullNotPush            DeliverSubject خالی، durable، AckExplicit
```

# Follow-ups / Risks

- `dispatcher.go` هنوز سیم‌کشی نشده. consumer آماده است ولی هیچ باینری صدایش
  نمی‌زند؛ `usecase.Dispatcher` هم هنوز senderها را ندارد.
- `AckWait` سقف کار هر پیام است. اگر روزی یک provider کندتر از ۶۰ ثانیه شود،
  تنظیمش کافی نیست — باید با `RECONCILE_AFTER` هم خوانده شود.

# Instruction

«برویم consumer» — یک pull consumer دائمی روی stream موجود، با همان سقف تلاش که
core دارد، تأخیر پلکانی بین تلاش‌ها، و خاموشی‌ای که کار در جریان را تمام می‌کند.
