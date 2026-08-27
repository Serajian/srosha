Branch: `feat/list-window`

# Summary

**`List` دیگر نمی‌تواند چیزی را بپرسد که این سرویس ندارد.**

تا امروز `List` دو زمانِ دلخواه می‌گرفت — `from` و `until` — و هیچ ربطی به
`RETENTION_AGE` نداشت. یعنی این ممکن بود:

```
درخواست:  ۹۰ روز گذشته
srosha:   ۳۰ روز دارم، بقیه را پاک کرده‌ام
جواب:     ۳۰ روز  ←  بدون هیچ اشاره‌ای
```

# مسئله: جوابِ خالی دو معنی داشت

```
خالی  =  چیزی نفرستادی
خالی  =  فرستادی، ولی ما دیگر نداریمش
```

و از هم قابلِ تشخیص نبودند. مشتری معنیِ اول را می‌فهمید.

# جواب: واژگانی که نمی‌شود از آن بیرون زد

```proto
enum Window {
  WINDOW_UNSPECIFIED = 0;   // تا هرجا که این استقرار نگه داشته
  WINDOW_LAST_HOUR   = 1;
  WINDOW_LAST_DAY    = 2;
  WINDOW_LAST_WEEK   = 3;
  WINDOW_LAST_MONTH  = 4;
}
```

`from` و `until` حذف شدند. «۹۰ روز» را دیگر **نمی‌شود نوشت**.

## ولی enum به‌تنهایی کافی نبود

`RETENTION_AGE` قابلِ تنظیم است. روی استقراری که ۷ روز نگه می‌دارد،
`LAST_MONTH` باز هم ۷ روز برمی‌گرداند — همان دروغ، فقط کوچک‌تر.

پس دو چیزِ دیگر:

**۱ — سرور پنجرهٔ بلندتر از نگه‌داشتِ خودش را رد می‌کند**، و حد را می‌گوید:

```
this service keeps messages for 7 days
```

حد در **message** است نه فقط در reason، چون تنها چیزی است که caller برای
پرسیدنِ دوباره لازم دارد.

**۲ — `UNSPECIFIED` یعنی «تا هرجا که نگه داشته‌ایم».** عددی از خودش ادعا نمی‌کند،
پس هر تنظیمی که باشد درست است. و صفرِ enum است: کسی که چیزی نگفته، وسیع‌ترین
جوابِ صادقانه را می‌گیرد نه یک جوابِ خالی.

# قاعده کجا نشست

```
domain     Window.Length / Since / Valid ، و ردِ پنجرهٔ بلند در Service.Page
postgres   یک time.Time می‌گیرد، نه Window
SQL        دست نخورد
```

repository نه ساعت دارد نه `RETENTION_AGE` — و نباید داشته باشد. «هفتهٔ گذشته
یعنی چه» یک قاعده است، و قاعده بالای statement ای می‌نشیند که سطر می‌خواند.

`until` هم کلاً رفت: هر پنجره از **الان** به عقب می‌رسد، پس سرِ دیگری برای بستن
نیست.

# gateway حالا `RETENTION_AGE` را می‌خواند

تا امروز فقط dispatcher می‌خواندش، ولی `List` را gateway جواب می‌دهد. یک
`LoadRetentionAge` مشترک ساخته شد تا دو loader از یک کلید جدا نیفتند —
`NOTIF_CRYPTO_KEYS` از قبل چنین است.

# آنچه از دست رفت

کامنتِ قبلیِ proto از این دفاع می‌کرد:

> "since yesterday" and "that week in March" are both real questions

**«آن هفته در اسفند» دیگر ممکن نیست.** با نگه‌داشتِ ۳۰ روزه ارزشش کم بود، ولی
صفر نبود. این معامله‌ای است که آگاهانه انجام شد.

# Files Changed

- `api/proto/notification/v1/common.proto` *(`Window`)* + `notification.proto` *(`from`/`until` → `window`)* + `gen/`
- `internal/core/domain/notification/types.go` *(`Window` از struct به enum)*
- `internal/core/domain/notification/service.go` *(`keeps`، ردِ پنجرهٔ بلند، `humanAge`)*
- `internal/core/domain/notification/{port,errors}.go`
- `internal/core/domain/notification/window_test.go` *(تازه)*
- `internal/adapter/db/postgres/notification.go` *(`PageBySource` یک `time.Time` می‌گیرد)*
- `internal/adapter/db/postgres/notification_test.go`
- `internal/adapter/api/grpcsrv/{mapper,notification}.go` *(`toWindow`)*
- `internal/config/settings/retention.go` *(`LoadRetentionAge`)*
- `internal/config/gateway.go` *(`RetentionAge`)*
- `internal/bootstrap/{gateway,dispatcher}.go`
- `internal/core/usecase/{fakes,query,submit,dispatch,retention}_test.go`
- `.env.gateway.example`، `docs/CONFIG.md`

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — سبز
- دستی، با gateway واقعی:

```
نگه‌داشت ۳۰ روز (پیش‌فرض)
  LAST_HOUR · LAST_DAY · LAST_MONTH   →  کار می‌کنند
  window: 9                            →  InvalidArgument: unknown time window

نگه‌داشت ۷ روز
  LAST_WEEK    →  کار می‌کند  (دقیقاً روی مرز)
  LAST_MONTH   →  InvalidArgument: this service keeps messages for 7 days
  UNSPECIFIED  →  کار می‌کند  ← همیشه
```

دادهٔ تست پاک شد و `.env.gateway` برگردانده شد.

# Follow-ups / Risks

- **تداخل با `feat/more-channels`.** آن برنچ هم `common.proto`، `gen/` و
  `mapper.go` را دست زده. هرکدام دوم merge شود، تداخل دارد — کوچک و مکانیکی
  (هر دو فقط چیزی اضافه کرده‌اند)، ولی باید انتظارش را داشت.
- **دو کلید که می‌توانند اختلاف پیدا کنند.** `NOTIF_RETENTION_AGE` باید در هر دو
  `.env` یکسان باشد. اگر gateway عددِ بزرگ‌تری داشته باشد، لیستی سرو می‌شود که
  خالی است چون dispatcher پاکش کرده — دقیقاً همان چیزی که این تغییر می‌خواست
  بردارد. تنها راهِ قطعی، یک `.env` مشترک است.
- **`LAST_MONTH` یعنی ۳۰ روز، نه «ماه».** تقویم را نمی‌شناسد. برای این کار کافی
  است و نامش کمی بیش از واقعیت می‌گوید.
- **کشفِ حد فقط از راهِ خطاست.** مشتری تا وقتی رد نشود نمی‌داند چقدر می‌تواند به
  عقب برود. یک rpc ــِ `Limits` جوابش است، ولی سطحِ تازه است و هنوز کسی
  نخواسته.

# Instruction

«گرفتنِ list با زمان باید enum بشود و از srosha بیاید — مثلاً الان ما تا ۱ ماه
نگه می‌داریم، پس نباید بتونه بیش از ۱ ماه رو بگیره.» و بعد از توضیحِ اینکه enum
به‌تنهایی سوراخ دارد چون `RETENTION_AGE` قابلِ تنظیم است: «الان و جدا» — یعنی
قبل از SDK و روی برنچِ خودش.
