Branch: `feat/dispatcher-wiring`

# Summary

**srosha کار می‌کند.** برای اولین بار یک پیام کل مسیر را رفت: از gRPC تا Telegram،
و نتیجه‌اش امضاشده برگشت.

```
gRPC → postgres → nats → consumer → credential → secret → telegram
                                                              ↓
                                          postgres(FAILED) → callback امضاشده
```

## dispatcher دو راه ورود دارد و هیچ درگاهی که کسی به آن وصل شود

```
broker      رویداد می‌آورد        →  Consumer         tier 3
scheduler   ردیفی پیدا می‌کند که  →  Recover          tier 3
            هیچ رویدادی برایش نیامد
health      تنها listener                              tier 3
```

هر سه در بالاترین tier اند و این تصادفی نیست: **کارِ ورودی** اند. باید قبل از
broker ای که به آن ack می‌دهند و pool ای که در آن می‌نویسند بایستند.

## `infra/scheduler` روی `go-co-op/gocron/v2`

چیزی که وجود نداشت، و مثل بقیهٔ dependency ها ساخته شد: یک package در
`internal/infra/` با `Config` خودش، و `registry` که settings را به آن ترجمه
می‌کند — همان شکل `httpserver` و `grpcserver`.

```
Config{Location, StopTimeout}   New → Add → Start → Shutdown
```

**چرا کتابخانه و نه یک ticker:** ticker هیچ محافظتی در برابر هم‌پوشانی ندارد.
`ListStale` ردیف‌ها را claim نمی‌کند، پس اگر یک sweep طولانی‌تر از بازه شود، دومی
روی اولی می‌افتد و یک delivery دو بار می‌رود. `WithSingletonMode(LimitModeReschedule)`
دقیقاً همین را می‌بندد.

**یک spec، سه شکل.** `CronJob(spec, false)` زیر پوسته `cron.ParseStandard` است،
پس هر سه با یک parser خوانده می‌شوند:

```
@every 5m        همان رفتار امروز
*/5 * * * *      cron واقعی
0 3 * * *        هر شب ساعت ۳
```

برای همین `NOTIF_RECONCILE_EVERY` (یک Duration) شد `NOTIF_RECONCILE_SCHEDULE`
(یک رشته). پیش‌فرضش همان `@every 5m` است، و deployment ای که از یک بازه فراتر
برود به تنظیم تازه‌ای نیاز ندارد. ثانیه فیلد نیست — کاری که به ثانیه نیاز دارد،
کاری است که صف می‌خواهد نه scheduler.

**UTC، و این ترجیح نیست:** `0 3 * * *` در هر منطقه یک لحظهٔ متفاوت است، و در
منطقه‌ای با ساعت تابستانی ساعتی را نام می‌برد که یک روز از سال دو بار اتفاق
می‌افتد و یک روز اصلاً نه.

**`*slog.Logger` بدون هیچ adapter ای `gocron.Logger` را برآورده می‌کند** — همان
چهار متد با همان امضا. پس پیام‌های خود کتابخانه زیر همان کلیدهای service و binary
می‌نشینند.

اولین اجرا **یک بازه بعد** است، نه بلافاصله: یک rolling deploy همهٔ replica ها را
در فاصلهٔ چند ثانیه بالا می‌آورد، و job ای که موقع boot اجرا شود یعنی همه‌شان یک
sweep را در یک لحظه می‌زنند.

و خطای یک sweep، sweep بعدی را متوقف نمی‌کند — آنچه نرسید هنوز آنجاست، که کل
دلیلِ scheduler داشتن است.

## باگی که فقط با اجرا پیدا شد

اولین اجرای واقعی این را نشان داد:

```
WARN  delivery not settled, asking again  attempt=1  err="NOT_FOUND: delivery not found"
WARN  delivery not settled, asking again  attempt=2  err="NOT_FOUND: delivery not found"
```

یک رویداد برای ردیفی که دیگر وجود نداشت، **پنج بار** برمی‌گشت. `Handle` این را
داشت:

```go
del, err := d.deliveries.Get(ctx, id)
if err != nil { return err }
if del == nil { ... return nil }     // ← هرگز اجرا نمی‌شود
```

repository برای ردیف نبوده `NotFound` برمی‌گرداند نه `nil`، پس آن شاخه **مرده**
بود و مسیر واقعی خطا برمی‌گرداند — یعنی nak، یعنی دوباره، تا سقف MaxDeliver.

همین اشتباه در `deliver` هم بود و آنجا بدتر:

```
پیام حذف شده → n == nil هرگز →  خطا برمی‌گردد  →  ردیف تا ابد PENDING می‌ماند
                                                   و هیچ نتیجه‌ای رویش نوشته نمی‌شود
```

**چرا تست‌ها نگرفته بودند:** fake ها دروغ می‌گفتند. `fakeDeliveries.ReadByID`
برای ردیف نبوده `(nil, nil)` می‌داد، یعنی دقیقاً همان شاخه‌ای را زنده نگه می‌داشت
که هیچ deployment ای به آن نمی‌رسد. حالا همان چیزی را می‌دهند که postgres می‌دهد.

کامنت بالای همان فایل از قبل این را گفته بود: *«اگر نوشتن یکی از این‌ها سخت است،
چیزی که باید درست شود پورتی است که پیاده می‌کند.»* اینجا سخت نبود — فقط غلط بود.

# Files Changed

- `internal/infra/scheduler/{scheduler,const}.go` *(تازه — روی `gocron/v2`)*
- `internal/infra/scheduler/scheduler_test.go` *(تازه — ۱۲ تست)*
- `internal/registry/scheduler.go` *(تازه — settings → Config، و ثبت stop)*
- `internal/config/settings/dispatch.go` *(`ReconcileEvery` → `ReconcileSchedule`)*
- `go.mod` *(`go-co-op/gocron/v2`)*
- `internal/bootstrap/dispatcher.go` *(سیم‌کشی کامل + `buildDispatcherCore`)*
- `internal/core/usecase/dispatch.go` *(دو شاخهٔ مرده با sentinel واقعی جایگزین شد)*
- `internal/core/usecase/dispatch_test.go` *(دو تست تازه)*
- `internal/core/usecase/fakes_test.go` *(fake ها همان چیزی می‌گویند که postgres می‌گوید)*
- `.env.dispatcher.example` *(`RECONCILE_SCHEDULE`)*
- `docs/CONFIG.md`
- `.env` *(فقط توسعه: callback محلی)*

# Tests Run

- `make prepush` — سبز
- `golangci-lint run ./...` — صفر ایراد
- دستی، هر دو باینری روی postgres و nats واقعی، با تماس واقعی به `api.telegram.org`:

```
RegisterCredential (توکن جعلی ولی خوش‌فرم)  →  ردیف: v1.1.<nonce>.<ciphertext>
RegisterWebhook http://127.0.0.1:9099/hooks →  ثبت شد
Submit  telegram/high                       →  01M0WY5GQPWGV480PDA1YSMWBH
                            ↓
dispatcher  consume → credential → Material → telegram → 401
                            ↓
postgres    FAILED · PERMANENT · attempts=1
                            ↓
callback    signature=VALID   timestamp=1787677565
            {"batch_id":…,"results":[{"status":"FAILED","reason":"PERMANENT",…}]}
```

گیرنده HMAC را خودش از نو ساخت و برابر بود. توکن جعلی بود، پس ۴۰۱ گرفت، پس
permanent شد، پس **یک** تلاش — که دقیقاً رفتار درست است.

رویداد شبح هم جدا آزموده شد: یک پیام برای delivery ای که ردیف ندارد، مستقیم روی
stream گذاشته شد. یک خط لاگ، و تمام:

```
ERROR  event for a delivery that does not exist  delivery_id=01K0GHOST0000000000000000
```

و **recovery** — مسیری که هرگز اجرا نشده بود. یک delivery سرگردان مستقیم در
دیتابیس گذاشته شد، ده دقیقه کهنه، و scheduler با `@every 3s` بالا آمد:

```
+3s   sweep  →  ListStale آن را دید
                هیچ credential ای برای آن source، و fallback هم توکنی نداشت
                            ↓
      FAILED · NO_SENDER · attempts=1
```

که هم recovery را ثابت می‌کند و هم قاعدهٔ fallback را، زنده.

و خاموشی، به همان ترتیبی که اعلام شده:

```
http server(3) → recovery(3) → dispatch consumer(3)
   → sender client(2) → webhook client(2) → nats(1) → postgres(0)
```

`scheduler` هم دقیقاً همان‌جاست که consumer است — کارِ ورودی، بالاترین tier:

```
http server(3) → scheduler(3) → dispatch consumer(3) → …
```

دادهٔ تستی بعدش پاک شد.

# Follow-ups / Risks

- **هیچ ارسال موفقی آزموده نشده.** توکن واقعی Telegram لازم دارد. مسیر موفق فقط
  در unit test اجرا شده؛ همه‌چیز تا خودِ تماس با API واقعی است.
- **`ListStale` ردیف‌ها را claim نمی‌کند.** singleton mode هم‌پوشانی را **داخل
  یک پروسه** می‌بندد، نه بین چند replica. با چند replica همه یک sweep را می‌بینند
  و یک delivery می‌تواند دوبار برود. راه‌حلش یک `FOR UPDATE SKIP LOCKED` است، نه
  یک گزینهٔ scheduler.
- `email`، `bale` و `whatsapp` هنوز `NO_SENDER` می‌دهند.
- REST (grpc-gateway) و استقرار هنوز نوشته نشده‌اند.

# Instruction

«ادامه بده» بعد از notifier — یعنی سیم‌کشی dispatcher: consumer، scheduler ای که
`Recover` را می‌چرخاند، و هستهٔ کاملی که senderها و notifier به آن وصل باشند.
سپس: «برای scheduler می‌خواهم از go cron استفاده کنم» و «مثل بقیهٔ dependency ها
پیاده‌سازی کن» — یعنی `internal/infra/`، و `go-co-op/gocron/v2`.
