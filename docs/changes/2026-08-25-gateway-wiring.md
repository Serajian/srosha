Branch: `feat/gateway-wiring`

# Summary

**gateway کار می‌کند.** برای اولین بار یک پیام واقعی را می‌پذیرد، در postgres
می‌نویسد، روی nats منتشر می‌کند، و با SIGTERM به ترتیب درست خاموش می‌شود.

## ساختار: داستان بالا، جزئیات پایین

`Gateway()` حالا ۴۵ خط است و فقط داستان را می‌گوید:

```
logger → registry → Postgres → NATS → buildGatewayCore → gatewayGRPC → httpServer
```

و `buildGatewayCore` چهار بلوک بصری دارد که هر کدام یک لایه‌اند:

```
what the machine knows  →  clock، ids، rate limiter
the broker              →  stream، EnsureStream، publisher
the rows                →  ۶ repository + UnitOfWork
the rules over them     →  ۵ domain service  →  ۳ use case
```

**سیم‌کشی مشترک بین دو باینری نوشته نشد**، و این عمدی است. قاعدهٔ خودِ
`gateway.go` می‌گوید «هیچ credential فرستنده و هیچ راز callback ندارد، و نباید
داده شود» — یک سازندهٔ مشترک یا این را می‌شکند یا به dispatcher چیزهایی می‌دهد که
لازم ندارد. هر باینری فایل خودش را دارد، پس این قاعده ساختاراً برقرار می‌ماند.

## `delivery.Service` شکسته شد

قطع مصرف کاملاً تمیز بود — **هیچ متد مشترکی نداشتند**:

```
Service  Create · Publish · ListForNotification   →  repo، pub، newID، now
Tracker  Get · ListAll · ListStale · Record*      →  repo، now
```

dispatcher دیگر یک publisher نمی‌گیرد که هرگز صدا نمی‌زند، و یک `newID` که هرگز
شناسه‌ای نمی‌سازد. همان الگویی که با `source.Service` و `source.Authenticator`
داشتیم: یک پکیج، دو سرویس، دو دستهٔ وابستگی.

## `App` حالا چند listener را می‌بیند

قبلاً یک `*httpserver.Server` بود و `Run` روی `server.Err()` منتظر می‌ماند. gateway
دو listener دارد و اگر هرکدام **تنها** بمیرد، پروسه باید بفهمد — وگرنه سالم به نظر
می‌رسد و نصف چیزی را که ادعا می‌کند جواب می‌دهد.

`watch(...)` کانال‌ها را در یکی جمع می‌کند. چیزی بسته نمی‌شود: یک خاموشی عادی
شکست نیست و نباید `Run` را بیدار کند.

## شکافی در `settings.Webhook`

`webhook.Service.Register` یک `URLPolicy` می‌خواهد، ولی آن دو پرچم کنار `Secrets`
نشسته بودند — و آن گروه dispatcher-only است.

```
قبل   Webhook{ Secrets, Timeout, MaxFailures, AllowInsecureURL, AllowPrivateURL }
             ↑ gateway برای اینکه به دو پرچم برسد، باید رازها را هم می‌گرفت

بعد   WebhookPolicy{ AllowInsecureURL, AllowPrivateURL }   ← هر دو باینری
      Webhook{ WebhookPolicy, Secrets, Timeout, MaxFailures }  ← فقط dispatcher
```

gateway آدرس را موقع ثبت اعتبارسنجی می‌کند و dispatcher بعد از DNS دوباره —
نامی که از چک اول رد شود هنوز می‌تواند روی شبکهٔ خودمان resolve شود. پس هر دو
policy را لازم دارند و هیچ‌کدام نیمهٔ دیگری را.

`CONFIG.md` هم یک اشتباه کهنه داشت که سر راه اصلاح شد: `NOTIF_WEBHOOK_MAX_ATTEMPTS`
نوشته بود، در حالی که کد `NOTIF_WEBHOOK_MAX_FAILURES` می‌خواند.

## آنچه واقعاً آزموده شد

روی postgres و nats واقعی، با یک source و یک کلید که موقتاً seed شد:

```
بدون کلید            →  Unauthenticated: invalid credentials
با کلید              →  {"id":"01M0WBYR...","effectivePriority":"PRIORITY_HIGH"}
                            ↓
postgres  notifications + deliveries(PENDING, email, a@acme.test)
nats      notify.email.high
          {"delivery_id":"...","source_id":"...","channel":"email","priority":"HIGH"}
          stream: WorkQueue، File، dedup=1h، maxage=24h
                            ↓
Get       پیام + delivery اش
Register  http://localhost:9000 →  InvalidArgument: callback url must use https
          https://acme.com/...  →  ثبت شد
                            ↓
SIGTERM   http server(3) → grpc server(3) → nats(1) → postgres(0)
```

آن ترتیب آخر دقیقاً همان tierهاست: **اول از پذیرفتن می‌ایستد، آخر استخر دیتابیس
بسته می‌شود.** دادهٔ تستی بعدش پاک شد.

## reflection: همه‌جا جز production

`grpcurl` مجبور بود فایل‌های proto را بگیرد، چون سرور reflection ثبت نشده بود.
ثبتش یک خط است، ولی یک تلهٔ واقعی دارد:

```
Submit / Get / Register  →  unary   →  از Auth رد می‌شود  ✓
ServerReflectionInfo     →  stream  →  از Auth رد نمی‌شود ✗
```

interceptorهای ما unary اند و reflection یک متد streaming است. یعنی با آن روشن،
**هرکسی که به پورت برسد کل سطح API را می‌خواند** — هر سرویس، هر متد، هر پیام، هر
فیلد — بدون کلید و بدون اینکه در لاگ چیزی شبیه یک درخواست بگذارد.

پس `Deps.Reflection` است و `Gateway()` آن را از `!cfg.App.IsProduction()` می‌دهد.
همان الگویی که `LoadWebhookPolicy` دارد: بیرون از production راحت، روی production
سخت‌گیرانه.

روی سرویس زنده آزموده شد: بدون هیچ فایل proto ای `list` و `describe` کار کردند، و
`Submit` بدون کلید همچنان `Unauthenticated` گرفت.

# Files Changed

- `internal/bootstrap/gateway.go` *(داستان + `buildGatewayCore` + `gatewayGRPC`)*
- `internal/adapter/api/grpcsrv/register.go` *(`Deps.Reflection`)*
- `internal/adapter/api/grpcsrv/errors_test.go` *(تست reflection)*
- `internal/bootstrap/app.go` *(`failed` به‌جای `server`، و `watch`)*
- `internal/bootstrap/dispatcher.go` *(`App` تازه)*
- `internal/core/domain/delivery/service.go` *(فقط پذیرش و انتشار)*
- `internal/core/domain/delivery/tracker.go` *(تازه — خواندن و ثبت نتیجه)*
- `internal/core/usecase/dispatch.go` *(حالا `*delivery.Tracker` می‌گیرد)*
- `internal/core/usecase/dispatch_test.go`
- `internal/config/settings/webhook.go` *(`WebhookPolicy` جدا شد)*
- `internal/config/gateway.go` *(`WebhookPolicy`)*
- `docs/CONFIG.md` *(ردیف policy، و `MAX_ATTEMPTS` → `MAX_FAILURES`)*
- `.env.example` / `.env.dispatcher.example` *(دو پرچم policy مشترک شدند)*

# Tests Run

- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، از انتها تا انتها با postgres و nats واقعی — بالا

# Follow-ups / Risks

- `dispatcher.go` هنوز سیم‌کشی نشده. `Tracker` برایش آماده است، ولی senderها،
  notifier و consumer هنوز stub اند.
- تست خودکاری برای خودِ سیم‌کشی نیست. آنچه شد دستی بود؛ یک تست یکپارچگی که
  `Gateway()` را بالا بیاورد و یک rpc بزند ارزش دارد.

# Instruction

«برویم سیم‌کشی، اول ساختار را پیشنهاد بده» — با سه تصمیم: ساختار **ب** (داستان
بالا، سازنده‌های نام‌دار پایین)، `delivery.Service` کامل برای gateway، و **ج**
برای dispatcher (سرویس جدا با وابستگی‌های حداقلی).
