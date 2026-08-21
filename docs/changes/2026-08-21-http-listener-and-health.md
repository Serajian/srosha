Branch: `feat/bootstrap`

# Summary

دو چیز که با هم معنی می‌دهند: یک سرور HTTP در infra، و اولین چیزی که سرو می‌کند.

## internal/infra/httpserver

مثل بقیهٔ infra: `New` فقط validate می‌کند، `Start` می‌بندد و سرو می‌کند،
`Shutdown` تمام می‌کند. هیچ چیز از route هایش نمی‌داند — `http.Handler` را از بیرون
می‌گیرد.

یک تصمیم داخلش هست که ارزش گفتن دارد. `Start` **اول listener را bind می‌کند و همان
موقع برمی‌گردد اگر نشد**، و فقط بعدش در goroutine سرو می‌کند:

```
ln, err := lc.Listen(ctx, "tcp", addr)   // همزمان
if err != nil { return err }
go s.srv.Serve(ln)                        // در پس‌زمینه
```

اگر هر دو در goroutine بودند، پورتِ گرفته‌شده می‌شد یک خط log که process از آن جان
سالم به در می‌برد: یک gateway که «سالم» بالا آمده و روی هیچ پورتی گوش نمی‌دهد.
`TestStartFailsOnAPortAlreadyTaken` همین را می‌بندد.

`Err()` هم هست: یک کانال که فقط شکستِ سرو را حمل می‌کند، نه خاموشی عادی.
`http.ErrServerClosed` چیزی است که `Shutdown` تولید می‌کند، پس شکست نیست. بدون این
کانال، listener ای که می‌میرد را هیچ‌کس متوجه نمی‌شود.

`Addr()` آدرسِ واقعاً bind شده را می‌دهد نه `Config.Addr` — که وقتی پورت را به هسته
سپرده‌ای فرق دارد، و همان چیزی است که تست‌ها را بدون پورت ثابت ممکن می‌کند.

## internal/adapter/api/health

دو endpoint که عمداً دو سؤال متفاوت‌اند:

`/healthz` **همیشه ۲۰۰** برمی‌گرداند و این موقتی نیست. liveness می‌پرسد «آیا این
process هنوز خودش است؟». اگر وقتی postgres پایین است ۵۰۳ بدهد، orchestrator کانتینر
را می‌کشد و restart می‌کند — که postgres را برنمی‌گرداند و فقط یک حلقهٔ restart
می‌سازد که خطای واقعی را زیر خودش دفن می‌کند.

`/readyz` واقعی است: `res.Ready` را صدا می‌زند، که روی postgres یک `select 1` و روی
nats یک `AccountInfo` می‌زند. اگر یکی پایین باشد ۵۰۳ می‌دهد — از چرخش بیرون، ولی
زنده.

متن خطا **در body نمی‌آید**. این پورت ممکن است از جایی دورتر از آنچه نویسنده‌اش فکر
می‌کند قابل دسترس باشد، و آن متن اسم dependency ها و آدرس‌های داخلی ما را حمل می‌کند.
می‌رود در log. `TestReadinessDoesNotPublishTheReason` همین نشت را چک می‌کند.

handler خودش نمی‌داند چه چیزی باز است و نباید بداند: یک `func(context.Context) error`
می‌گیرد. لیست dependency ها مال `bootstrap` است، و این یک adapter است که طبق قانون
`registry` را نمی‌بیند.

## config

گروه تازهٔ `settings.HTTPServer` با چهار timeout، مشترک بین دو باینری. **کجا** گوش
بدهند مال خودشان است: `GRPC.HTTPAddr` برای gateway و `HTTP.Addr` برای dispatcher،
که هر دو از قبل بودند. برای همین `registry.HTTPServer` آدرس را جدا می‌گیرد و از یک
گروه settings نمی‌خواند.

`READ_HEADER_TIMEOUT` تنها یکی از آن چهارتاست که دلیل امنیتی دارد: بدون آن یک client
می‌تواند با فرستادن header بایت‌به‌بایت، اتصال را باز نگه دارد.

# Files Changed

- `internal/infra/httpserver/server.go` *(تازه — `Config`، `validate`، `Server` با `New`/`Start`/`Shutdown`/`Addr`/`Err`)*
- `internal/infra/httpserver/server_test.go` *(تازه — هفت تست)*
- `internal/adapter/api/health/handler.go` *(تازه — `Handler(ready, log)`)*
- `internal/adapter/api/health/handler_test.go` *(تازه — چهار تست)*
- `internal/registry/httpserver.go` *(تازه — `HTTPServer(...)` در `tierServer`)*
- `internal/registry/httpserver_test.go` *(تازه — رد شدن addr خالی، و بسته شدن همراه بقیه)*
- `internal/config/settings/httpserver.go` *(تازه — چهار timeout)*
- `internal/config/gateway.go`, `dispatcher.go` *(گروه `HTTPServer`)*
- `.env.example`, `docs/CONFIG.md` *(همان کلیدها)*

# Tests Run

- `make prepush` — همه پاس

# Follow-ups / Risks

- `/readyz` الان فقط می‌گوید ready یا نه. کدام dependency خراب است در body نمی‌آید،
  و `Resources.Ready` هم فقط یک `error` با `errors.Join` برمی‌گرداند که handler
  نمی‌تواند تفکیکش کند بدون parse کردن متن — که ممنوع است. این جدا اصلاح می‌شود.
- اگر process واقعاً deadlock شود، goroutine سرور HTTP ممکن است هنوز جواب بدهد و
  `/healthz` دروغ بگوید. این ذاتیِ liveness probe است، نه عیب این کد.
- `Err()` هنوز مصرف‌کننده ندارد. `bootstrap` جدا نوشته می‌شود.

# Instruction

«bootstrap را بنویسیم» — و در همان گفتگو انتخاب «ب»: سرور سلامت هم در همین کار
نوشته شود، تا `Resources.Ready` مصرف‌کننده پیدا کند و باینری چیزی داشته باشد که
واقعاً بشود امتحانش کرد.
