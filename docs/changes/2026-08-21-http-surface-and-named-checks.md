Branch: `feat/bootstrap`

# Summary

دو تغییر که به هم گره خورده‌اند: جای handler های HTTP، و اینکه `/readyz` چه چیزی
می‌گوید.

## ساختار

`internal/adapter/api/health/` غلط بود. کنارش `api/grpc/` است که یک **transport**
است؛ `health` یک transport نیست، یک endpoint است. دو چیز هم‌سطح گذاشته شده بودند که
هم‌سطح نیستند، و روزی که endpoint دوم HTTP می‌آمد (`/metrics`) پوشهٔ سومی می‌گرفت و
`api/` پر می‌شد از چیزهایی که هیچ‌کدام transport نیستند.

حالا `internal/adapter/api/http/` است، موازی `api/grpc/`:

```
api/http/router.go    New(Deps) -- هر route اینجا mount می‌شود
api/http/health.go    mountHealth
```

`New` یک نقطهٔ ورود است: کسی که می‌خواهد بداند این سرویس چه چیزی جواب می‌دهد، همان
یک تابع را می‌خواند. هر endpoint بعدی یک `mountX` می‌شود و یک خط آنجا.

نام پکیج `http` با `net/http` تداخل دارد، پس کتابخانهٔ استاندارد `nethttp` صدا زده
می‌شود. عمدی است: `api/grpc` هم دقیقاً همین کار را با `google.golang.org/grpc`
می‌کند و یکدستی مهم‌تر از آن alias است.

Gin نیامد. سطح HTTP اینجا سه route است و REST آینده از grpc-gateway تولید می‌شود که
خودش `http.Handler` است و روی هر mux سوار می‌شود. `gin.Context` هم یک تایپ context
دوم است که به امضای handler ها نشت می‌کند. اگر روزی سطح دستی‌نوشته از حدود ده route
با binding واقعی رد شد، عوض کردنش یک پکیج را لمس می‌کند و بس.

## `/readyz` حالا می‌گوید کدام

قبلاً body فقط `not ready` بود. اپراتور هیچ چیز مفیدی نمی‌گرفت و مجبور بود لاگ را باز
کند.

ریشه‌اش در `Resources.Ready` بود که یک `error` با `errors.Join` برمی‌گرداند. handler
نمی‌توانست تفکیک کند کدام خراب است **مگر با parse کردن متن خطا** — که در
`docs/CONVENTIONS.md` صراحتاً ممنوع است.

پس امضایش عوض شد:

```
Ready(ctx) []Check      // Check{Name string; Err error}
```

و خروجی واقعی حالا این است:

```
{"binary":"dispatcher","status":"not ready","checks":[{"name":"postgres","status":"down"},{"name":"nats","status":"up"}]}
```

`Err` هنوز در `Check` هست ولی **از process بیرون نمی‌رود**: در body فقط `up`/`down`
می‌آید و متن کامل — که آدرس‌های داخلی ما را حمل می‌کند — در log می‌نشیند. یک تست
دنبال `10.0.0.5` و `dial tcp` در body می‌گردد.

`binary` در body آمد نه در path. دو باینری روی دو آدرس گوش می‌دهند، ولی `/healthz` و
`/readyz` نام‌های استانداردی هستند که compose و k8s با همان‌ها تنظیم می‌شوند؛ مسیر
متفاوت یعنی پیکربندی استقرار برای هر سرویس فرق کند بدون اینکه چیزی به دست بیاید.

## یک نکتهٔ لایه‌بندی

`api/http` نمی‌تواند `registry.Check` را import کند — همان guard که خودمان نوشتیم
جلویش را می‌گیرد. پس تایپ خودش را اعلام می‌کند و `bootstrap` — تنها جایی که هر دو را
می‌بیند — ترجمه می‌کند. همان الگوی port، یک لایه بالاتر.

# Files Changed

- `internal/adapter/api/http/router.go` *(تازه — `Deps`، `validate`، `Check`، `New`)*
- `internal/adapter/api/http/health.go` *(تازه — `mountHealth` و شکل پاسخ)*
- `internal/adapter/api/http/health_test.go` *(تازه — هفت تست)*
- `internal/adapter/api/health/` *(حذف)*
- `internal/registry/registry.go` *(`Ready` حالا `[]Check` می‌دهد)*
- `internal/registry/registry_test.go` *(سه تست برای شکل تازهٔ `Ready`)*
- `internal/bootstrap/app.go` *(`httpServer` و `checks` که ترجمه می‌کند)*
- `internal/bootstrap/gateway.go`, `dispatcher.go` *(نام تابع)*

# Tests Run

- `make prepush` — همه پاس
- اجرای واقعی: با postgres خاموش، `/readyz` جواب `503` با `postgres: down، nats: up` داد، و `/healthz` همان لحظه `200`

# Follow-ups / Risks

- `api/http` امروز فقط health دارد. `/metrics` و REST از grpc-gateway هر دو
  `mountX` خودشان را می‌گیرند.
- کامنت بالای `validateURL` در `webhook/entity.go` هنوز می‌گوید چک دوم SSRF «باید»
  جایی انجام شود. حالا در `httpclient` انجام می‌شود ولی متن به‌روز نشده.

# Instruction

«به‌جای `api/health` باید `api/http` داشته باشی و همهٔ handler های http را آنجا پیاده
کنی» — و جداگانه: «`/readyz` باید بگوید کدام‌شان سالم است کدام نه، یا همه روی یک path
ولی اگر خطا داشت بگوید کدام».
