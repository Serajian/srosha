Branch: `feat/portal-spec`

# Summary

`internal/adapter/api/web` حالا روی **gin** است، نه `http.ServeMux` — و همان‌جا
به دو نیمه شکست: نیمهٔ مشترک، و هر سطح یک struct مستقل با engine خودش.

```
web.NewPortal(...)   سطحِ مشتری
web.NewAdmin(...)    بعداً، دقیقاً به همین شکل

مشترک: newEngine, sessions, guard, pageRender, formValue
        ← اشتراکِ کد است، نه اشتراکِ instance
```

اول این را دو **package** جدا کردم (`web/portal` و `web/admin`) و
`make arch-check` ردش کرد — و درست هم می‌گفت. قاعده اجازه می‌دهد یک adapter
subpackage خودش را import کند، نه برعکس؛ و surfaceای که parentِ مشترک را import
می‌کند دقیقاً همان برعکس است. برگشتم به دو struct در یک package، که همان جداییِ
مسیرها و handlerها را می‌دهد. چیزی که نمی‌دهد یک مرزِ کامپایلری بینِ handlerهای
یک سطح و جدولِ مسیرهای سطحِ دیگر است، و تستِ اولِ `ARCHITECTURE.md` جای آن را
می‌گیرد.

دلیلش شکلِ چیزی است که در راه است نه چیزی که هست: شش مسیر router نمی‌خواهد، سی
مسیر می‌خواهد. ثبتِ source، کلیدها، credentialها و callbackها هرکدام صفحه
می‌آورند، و سطحِ ادمین دستهٔ خودش را پشتِ گاردی دیگر. group و middleware همان
چیزی است که این را خوانا نگه می‌دارد.

گارد از روی هر مسیر رفت روی group، و **قاعده‌اش پارامتر شد**:

```go
engine.GET(pathSignIn, in.show)            باز
engine.POST(pathSignIn, in.request)

authed := engine.Group("", sessions.Guard(web.Anybody, pathSignIn))
authed.GET(pathHome, account.show)         صفحه‌ای که اینجا اضافه شود
                                           نمی‌تواند گارد را فراموش کند
```

`web.Anybody` قاعدهٔ پورتال است: وارد شدن تمامِ ماجراست. `web.Operator` قاعدهٔ
ادمین خواهد بود، و **همین حالا نوشته و تست شده** — که مهم‌ترین دستاوردِ این
تفکیک است.

gin فقط در همین یک package است. `core` و `infra` و `registry` نمی‌دانند وجود
دارد، و چون `gin.Engine` خودش `http.Handler` است، در `registry` و
`internal/infra/httpserver` یک خط هم عوض نشد. `api/http` — یعنی `/healthz` و
`/readyz` — عمداً روی کتابخانهٔ استاندارد ماند: دو مسیرند که باید وقتی همه‌چیز
خراب است جواب بدهند، و هرچه بینِ probe و جواب کمتر باشد بهتر است.

## چیزی که از دست رفت

**یک تضمینِ زمانِ کامپایل تبدیل به تضمینِ زمانِ اجرا شد.** قبلاً handlerهای
محافظت‌شده امضای متفاوتی داشتند:

```go
func (h *accountHandler) show(w, r, u *user.User)
```

یعنی صفحه‌ای که بیرونِ گارد mount می‌شد **کامپایل نمی‌شد**. handlerهای gin همه یک
امضا دارند، پس کاربر در context سفر می‌کند و `signedInUser` وقتی نباشد panic
می‌کند. جایگزینِ آن تضمین، خودِ group است: یک مسیر با **جایی که نوشته شده**
محافظت می‌شود نه با چیزی که می‌پذیرد. این ضعیف‌تر است، و آن panic عمدی است —
سرو کردنِ بی‌سروصدای صفحه‌ای بدونِ کاربر بدتر از ۵۰۰ است.

و همان‌جا دومین اثرش را نشان داد: `signedInUser` اول `v.(*user.User)` بدونِ چک
بود و `errcheck` گرفتش. با تضمینِ قبلی چنین تبدیلی اصلاً وجود نداشت، چون کاربر
پارامتر بود نه مقداری داخلِ context. حالا هر دو حالت چک می‌شوند و هر کدام با
پیامِ خودش panic می‌کند.

**بیست‌وشش وابستگیِ غیرمستقیم**، از جمله یک mongo driver، `quic-go` و یک
assembler، در ریپویی که فهرستِ مستقیمش کوتاه است. هیچ‌کدام در این سرویس استفاده
نمی‌شوند؛ از مسیرِ json و validation‌ــِ gin می‌آیند.

## چیزهایی که از پیش‌فرضِ gin برگردانده شد

هر چهارتا لازم بودند، نه سلیقه:

- **`gin.New` نه `gin.Default`** — Default لاگرِ خودش را می‌آورد و برای هر درخواست
  یک خطِ دوم با شکلِ متفاوت کنارِ لاگِ ساختاریافتهٔ ما می‌نویسد.
- **`HandleMethodNotAllowed = true`** — در gin خاموش است. بدونش `GET /signout`
  جوابِ ۴۰۴ می‌گرفت، یعنی «این مسیر وجود ندارد» در حالی که دارد.
  `TestSigningOutIsNotAGet` دقیقاً همین را گرفت.
- **`ReleaseMode` بیرون از development** و یک recovery از خودمان — خروجیِ
  debugــِ gin خودِ درخواستی را که panic کرده چاپ می‌کند، و درخواست‌های این سطح
  کدِ ورود دارند. یک panic نباید کدی را در لاگ بگذارد که قرار بود ده دقیقه
  ارزش داشته باشد.
- **`HTMLRender` از خودمان** — loaderهای gin همهٔ قالب‌ها را در یک set می‌گذارند و
  هر صفحه اینجا `content` را تعریف می‌کند، پس یک set یعنی فقط آخرین صفحهٔ
  parse‌شده برنده می‌شود؛ بی‌صدا، بدونِ خطا، با یک صفحهٔ خالی در انتها.

## تستی که زودتر از موعد نوشته شد

چون `Guard` قاعده را پارامتر می‌گیرد، مرزِ بینِ مشتری و سطحِ ادمین **قبل از اینکه
آن سطح وجود داشته باشد** قفل شد. `ARCHITECTURE.md` این را واجب می‌دانست و امروز
نوشتنی نبود:

```
TestOperatorPagesRefuseACustomer        مشتری با sessionِ معتبر رد می‌شود
TestOperatorPagesLetOperatorsThrough    admin و super_admin رد نمی‌شوند
TestTheCustomerSurfaceAsksOnlyForASession
TestEveryRefusalLooksTheSame            «session ندارم» و «نقشت اشتباه است»
                                        یک جواب می‌دهند
```

آن آخری هم لازم بود: اگر دو جوابِ متفاوت می‌داد، یک مشتری از همان تفاوت می‌فهمید
که صفحه‌های ادمین آن‌جا هستند.

# Files Changed

- `internal/adapter/api/web/web.go` *(`SignIn`، `NewEngine`، recovery، حالت)*
- `internal/adapter/api/web/session.go` *(`Sessions`، `Guard(May, path)`، `Anybody`/`Operator`)*
- `internal/adapter/api/web/session_test.go` *(new — مرزِ مشتری/اپراتور)*
- `internal/adapter/api/web/render.go` *(`PageRender`، حالا با surface)*
- `internal/adapter/api/web/{assets,form,errors,const}.go` *(مشترک و صادرشده)*
- `internal/adapter/api/web/portal/{portal,signin,account,const}.go` *(new — سطحِ مشتری)*
- `internal/adapter/api/web/portal/portal_test.go` *(منتقل‌شده از `signin_test.go`)*
- `internal/bootstrap/console.go` *(`portal.New` و `Debug`)*
- `go.mod`, `go.sum` *(gin و ۲۶ وابستگیِ غیرمستقیم)*
- `docs/ARCHITECTURE.md` *(بخشِ تازه: gin کجا هست و کجا نیست)*

# Tests Run

- `make prepush` — pass
- `go test ./internal/adapter/api/web/` — pass، همان پنج تست بدونِ تغییر
- روی باینریِ واقعی در حالتِ production:

```
GET  /                  303 → /signin
GET  /signin            200
GET  /signout           405        ← نه ۴۰۴
GET  /nope              404
GET  /static/portal.css 200  7066 بایت
GET  /static/crane.svg  200 20087 بایت
POST /signin/code       303 → /   و cookie ست شد
```

- قالب‌ها همچنان بیرون نمی‌روند: `/templates/portal/layout.html` و
  `/static/../templates/portal/layout.html` هر دو ۴۰۴.
- لاگِ راه‌اندازی در production هیچ سروصدایی از gin ندارد.

# Follow-ups / Risks

- **دو سبکِ HTTP در درخت.** `api/web` روی gin و `api/http` روی کتابخانهٔ
  استاندارد. عمدی است و در `ARCHITECTURE.md` نوشته شد، ولی هزینه‌اش واقعی است:
  کسی که یکی را خوانده، دیگری را همان‌طور نمی‌بیند.
- **آن panic تست ندارد.** `web.SignedInUser` وقتی گارد نباشد panic می‌کند و هیچ
  تستی این را نمی‌گیرد، چون امروز هیچ راهی برای mount کردنِ اشتباه نیست. وقتی
  `web/admin` بیاید و دو group وجود داشته باشد، ارزشِ یک تست را دارد.
- **تستِ دومی که `ARCHITECTURE.md` واجب می‌داند هنوز ممکن نیست**: اینکه هر مسیرِ
  ادمین روی handlerِ پورتال ۴۰۴ بدهد. تا `web/admin` نوشته نشود مسیری نیست که
  امتحان شود.
- **`gin.SetMode` سراسری است.** اگر روزی دو `web.New` با `Debug` متفاوت در یک
  process ساخته شوند، آخری برنده است. امروز یکی بیشتر نیست.

# Instruction

«می‌خواهم از Gin استفاده کنی.» — نگرانی‌ام را قبلاً گفته بودم (routerــِ
کتابخانهٔ استاندارد از Go 1.22 بیشترِ کارِ gin را می‌کند، و برتریِ باقی‌مانده‌اش
برای JSON است نه فرمِ HTML)، مالک تصمیمش را گرفت و دستور صریح بود. اجرا شد، و
هزینه‌ها به‌جای بحث در `ARCHITECTURE.md` و همین گزارش ثبت شدند.
