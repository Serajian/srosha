Branch: `feat/operator-alerts`

# Summary

هر باینری حالا خودش از خودش می‌پرسد که dependency هایش سرِ جایشان هستند یا نه،
و **فقط روی تغییر** خبر می‌دهد. task سه از پنج.

# مسئله‌ای که این حل می‌کند

readiness تا امروز فقط **پرسیده** می‌شد. `/readyz` وقتی جواب می‌دهد که کسی
صدایش بزند، و هیچ‌چیز داخلِ پروسه صدایش نمی‌زد. یعنی باینری لحظه‌ای که
dependency اش می‌افتد **می‌داند** و به کسی نمی‌گوید.

دیروز هزینه‌اش را دیدیم: سه سرویس یک روز `healthy` بودند روی دیتابیسی که یک
جدول نداشت.

# روی تغییر، نه روی وضعیت

`watcher` وضعیتِ قبلیِ هر dependency را نگه می‌دارد و فقط وقتی خبر می‌دهد که
عوض شده باشد. دیتابیسی که ده دقیقه پایین است **یک** پیام است، نه یکی هر سی
ثانیه — و نوعِ دوم دقیقاً همان چیزی است که operator را یاد می‌دهد اعلان‌ها را
نادیده بگیرد.

**اولین نگاه عمداً ساکت است.** باینری‌ای که با dependency ــِ افتاده بالا
می‌آید، همان را در اعلانِ startup گفته؛ تکرارش اینجا یعنی دو پیام برای هر
restart.

هر dependency جدا دیده می‌شود، پس افتادنِ postgres برگشتنِ nats را پنهان
نمی‌کند.

# اسمی که مجبور شد عوض شود

`notifier` نمی‌شد، چون `internal/adapter/notifier` پکیجی است که dispatcher
import می‌کند و کامپایلر یکی‌شان را باید کنار می‌گذاشت. شد `teller` — به نامِ
کاری که می‌کند، نه چیزی که هست.

# آزمونِ واقعی، نه با fake

plan می‌گفت با کشیدنِ یک dependency از زیرش امتحان شود. dispatcher را با
`NOTIF_ALERT_READY_EVERY=2s` بالا آوردم و postgres را متوقف کردم:

```
dependency went down  name=postgres  error="dial tcp 127.0.0.1:7001: connection refused"
dependency went down  name=schema    error="database schema is behind this build: …"
```

**دو dependency، دو خبر، هرکدام با نامِ خودش.** بعد بیست ثانیه پایین ماند —
یعنی ده بار پرسیده شد — و شمارشِ خبرها **همان دو** ماند. بعد برگرداندمش:

```
dependency recovered  name=postgres
dependency recovered  name=schema
down: 2   back: 2
```

اگر روی وضعیت خبر می‌داد، بیست پیام می‌شد.

تحویلِ خودِ Gotify اینجا امتحان نشد: کدِ ما `https` اجبار می‌کند و Gotify ــِ
محلی روی http بود. آن نیمه در task دو مقابلِ سرورِ واقعی ثابت شده بود.

# Files Changed

- `internal/bootstrap/watch.go` *(جدید)*
- `internal/bootstrap/watch_test.go` *(جدید — سه تست)*
- `internal/bootstrap/app.go` *(`App` حلقه را با `Run` اجرا می‌کند)*
- `internal/bootstrap/gateway.go`, `dispatcher.go`, `console.go` *(هر سه watcher می‌گیرند)*

# Tests Run

- `go test -count=1 ./internal/bootstrap/` — سه تست، pass
- `go test -count=1 ./...` — بدون شکست
- `make prepush` — pass
- **مقابلِ postgres واقعی**: افتادن، بیست ثانیه سکوت، برگشتن — `down: 2  back: 2`

# Follow-ups / Risks

- حلقه با `Run` شروع می‌شود نه با ساختِ `App`، پس پروسه‌ای که به اجرا نرسید
  چیزی را poll نمی‌کند.
- `NOTIF_ALERT_READY_EVERY` پیش‌فرضش ۳۰ ثانیه است. اگر یک dependency در فاصلهٔ
  دو پرسش بیفتد و برگردد، هیچ‌کس خبردار نمی‌شود — که برای یک قطعیِ لحظه‌ای
  درست است، نه اشکال.
- هنوز رویدادهای audit خبر نمی‌دهند. task چهار.
