Branch: `feat/sdk-module`

# Summary

**قدمِ دوم: کلاینتی که مشتری واقعاً import می‌کند.**

قدمِ اول قرارداد را جابه‌جا کرد و عمداً هیچ کدِ SDK ای نداشت. این یکی مسیرِ
روزانه را می‌آورد — `New`، `Submit`، `Get`، `List` — و **هیچ تایپِ protobuf ای
از آن بیرون نمی‌زند**.

```go
c, _ := srosha.New(ctx, "srosha.acme.test:443", key)
defer c.Close()

c.Submit(ctx, srosha.Message{
    Title: "سفارش ارسال شد",
    Body:  "کد رهگیری: ۱۲۳",
    Routes: []srosha.Route{
        srosha.Email("a@b.test"),
        srosha.Telegram("123456789").From("alerts"),
    },
})
```

# سه چیزی که ارزشش را می‌سازند

## ۱ — کلیدِ idempotency خودش ساخته می‌شود

`Submit` بدون کلید تکرارپذیر نیست: timeout بخوری و دوباره بفرستی، دو پیام
می‌شود. پس اگر caller ندهد، SDK یکی می‌سازد — ۱۶ بایتِ `crypto/rand` به hex،
**بدون وابستگیِ تازه**.

```
یک Submit با ۳ تلاش   یک کلید   یک پیام
دو Submit جدا          دو کلید    دو پیام   ← و این درست است
```

کلید **تصادفی** است نه hash ــِ محتوا: دو هشدارِ یکسان با چند دقیقه فاصله دو
پیام‌اند، و hash دومی را ناپدید می‌کرد.

## ۲ — enum ها با نام می‌روند و با نام برمی‌گردند

```
بیرون  مقداری که این build نمی‌شناسد → UNSPECIFIED، و سرویس نظرش را می‌گوید
داخل   هرچه سرویس نامیده، همان می‌ماند
```

نامتقارن، و عمدی: حدس زدن در راهِ بیرون یعنی فرستادنِ چیزی که مشتری نخواسته؛
حدس زدن در راهِ داخل یعنی پنهان کردنِ اینکه سرویس رشد کرده. `Channel` رشته است
پس **سرورِ جدیدتر SDK ــِ قدیمی‌تر را نمی‌شکند**.

## ۳ — صفحه‌بندی یک range است

```go
for n, err := range c.List(ctx, srosha.LastWeek) { … }
```

و صفحه‌ها **وقتی حلقه بخواهد** گرفته می‌شوند، نه از قبل: `break` یعنی درخواستِ
بعدی نمی‌رود. تست دارد.

# خطاها: نُه sentinel، و نه ریزتر

```go
if errors.Is(err, srosha.ErrRateLimited) { … }
```

`Error` یکی از sentinel ها را wrap می‌کند (`Unwrap`، نه `Is` ــِ دست‌نویس) و
جملهٔ سرویس را حمل می‌کند.

**ریزتر نمی‌شود**، چون `InvalidArgument` چند چیزِ متفاوت را می‌پوشاند و تنها راهِ
تشخیصشان تطبیقِ متن است. صریح در کامنت نوشته شده که اگر روزی لازم شد، جوابش یک
کدِ ماشین‌خوان روی سیم است نه هوشمندی اینجا.

# retry

```
Unavailable · DeadlineExceeded · ResourceExhausted   ✓
بقیه                                                  ✗
```

نمایی با jitter، و rate limit صبرِ بلندتری می‌گیرد چون برگشتنِ فوری همان سهمیه را
دوباره خرج می‌کند. سرویس هیچ راهنماییِ زمانی نمی‌فرستد، پس این کاملاً انتخابِ SDK
است.

# دو چیزی که lint گرفت و درست بود

**G117 روی `Config.APIKey`** — فیلدِ صادراتی‌ای که اگر marshal شود کلید را لو
می‌دهد. با `json:"-"` و یک `String()` ــِ حذف‌کننده بسته شد، همان کاری که
`sender.SMTP.Password` در سرور می‌کند. `//nolint` نزدم چون ایراد واقعی بود.

**دو غلط املاییِ بریتانیایی** — پیکربندیِ مخزن US است.

# Files Changed

- `sdk/go/srosha/{client,send,query,types,channel,errors,convert,const}.go` *(تازه)*
- `sdk/go/srosha/{srosha,example,export}_test.go` *(تازه — ۱۹ تست، ۴ مثالِ کامپایل‌شونده)*
- `sdk/go/internal/transport/{transport,const}.go` *(تازه)*
- `sdk/go/internal/retry/{retry,const}.go` *(تازه)*

# Tests Run

- `make prepush` — سبز، شاملِ `make sdk`
- تست‌ها با `bufconn`: سرورِ واقعیِ gRPC روی لولهٔ حافظه، بدون پورت و بدون Docker
- دستی، **از یک ماژولِ بیرونی** که SDK را با `replace` مصرف می‌کند، روی gateway واقعی:

```
submit    : id=01M121… priority=high downgraded=true duplicate=false
idempotent: same id=true duplicate=true
get       : title="srosha" priority=high requested=critical deliveries=2
            email     pending
            telegram  pending
list      : 2 messages in the last day
bad addr  : invalid=true   srosha: invalid request: invalid delivery address
bad key   : unauthorized=true  srosha: unauthorized: invalid credentials
```

و با نگه‌داشتِ ۷ روزه، پنجرهٔ بلندتر:

```
window    : invalid=true  srosha: invalid request: this service keeps messages for 7 days
```

`downgraded=true` نکتهٔ ریزی است که ارزش دیدن دارد: source سقفش `HIGH` بود و
`CRITICAL` خواست — پذیرفته شد، پایین آورده شد، و **گفته شد**.

دادهٔ تست پاک شد و `.env.gateway` برگردانده شد.

# Follow-ups / Risks

- **`Credentials` و `Webhooks` هنوز نیستند.** قدمِ سوم‌اند. یعنی مشتری امروز
  می‌تواند بفرستد ولی نمی‌تواند هویتِ فرستنده‌اش را ثبت کند.
- **تستِ متقابلِ بینِ ماژول‌ها هنوز نوشته نشده** — آن هفت تستی که ثابت می‌کنند
  json ــِ هر credential را سرور می‌پذیرد. با قدمِ سوم می‌آید.
- **`New` هیچ‌کس را صدا نمی‌زند.** `grpc.NewClient` اتصال را سرِ اولین تماس
  می‌سازد، پس آدرسِ غلط یک درخواستِ شکست‌خورده است نه یک سازندهٔ شکست‌خورده.
  `context` گرفته می‌شود برای روزی که این عوض شود.
- **`grpc.NewClient` هدف را با DNS حل می‌کند.** در تست‌ها `passthrough:///`
  لازم شد. مشتریِ واقعی `host:port` می‌دهد و درست کار می‌کند، ولی کسی که
  آدرسِ غیرعادی بدهد باید بداند.
- **بدونِ `bufconn`، هیچ تستی سرور را لمس نمی‌کند.** تست‌های اینجا قرارداد را
  می‌سنجند نه رفتارِ سرویس را؛ آن کارِ integration تست‌های سرور است.

# Instruction

«برو» — قدمِ دومِ «Order of work» ــِ spec: کلاینت و مسیرِ ارسال.
