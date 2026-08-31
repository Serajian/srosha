Branch: `feat/admin-panel`

# Summary

Task 7 از طرحِ admin panel: خودِ صفحه‌های HTML. تا اینجا `usecase.Operators` و
چهار حالتِ یک source آماده بودند و هیچ صفحه‌ای رویشان نبود؛ این تغییر سطحِ دومِ
باینریِ `console` را می‌سازد و routeها را به آن use case وصل می‌کند.

`internal/adapter/api/web/admin.go` دقیقاً شکلِ `portal.go` را تکرار می‌کند:
`AdminDeps`، `NewAdmin`، و یک جدولِ route که هرکس بدونِ grep می‌فهمد این سطح
چه چیزی سرو می‌کند. سه فایلِ handler کنارش:

```
admin_review.go   reviewHandler   queue، همه‌ی sourceها، یک source، چهار تصمیم، لاگِ پیام‌ها
admin_people.go   peopleHandler   roster و نقش‌ها -- فقط super_admin
admin_audit.go    auditHandler    audit log
```

مرزِ بین دو audience همانی است که `docs/ARCHITECTURE.md` نوشته: `operator()`
در `session.go` از قبل نوشته شده بود، از روی ردیفِ زنده‌ی `users` می‌خواند، و
اینجا فقط استفاده شد -- کنارش `superAdmin()` اضافه شد، به همان شکل. دو گروهِ
جدا در `NewAdmin` -- یکی با `operator`، یکی با `superAdmin` -- یعنی این‌که کدام
صفحه به کدام نقش نیاز دارد از همان‌جا که routeها لیست شده‌اند دیده می‌شود، و
خودِ `Operators` (در `mayGovernPeople`) هم همان چک را دوباره می‌زند -- defense
in depth، نه تکرار.

## سه تصحیح از طرحِ اصلی

قبل از نوشتنِ کد یک conflict scan روی brief زده شده بود که سه جا را عوض کرد:
گروهِ guard شده به‌جای `inside`، اسمش شد `guarded` (چون `portal.go` از قبل
`var inside` دارد و یک local با همین اسم آن را برای کل تابع سایه می‌انداخت)؛
`chrome` عریض نشد، این سطح مالِ خودش `adminChrome{SignedIn, SuperAdmin bool}`
گرفت؛ و هر page view model -- شاملِ صفحه‌های sign-in -- این را embed می‌کند. هر
سه در کد رعایت شدند.

## یک تفاوت با نسخه‌ی نوشته‌شده در brief: `adminChrome` یک مقدار نیست، یک تابع است

Brief گفته بود این سطح "own package-level value" مثلِ `inside` پرتال بگیرد.
اما `SuperAdmin` واقعاً به کسی که نگاه می‌کند بستگی دارد: یک `admin` و یک
`super_admin` هر دو به همان queueِ guard شده می‌رسند، و nav باید برایشان فرق
کند. یک مقدارِ ثابت یا این لینک را همیشه از `admin` مخفی می‌کرد یا همیشه به او
نشان می‌داد. به‌جایش `chromeFor(u *user.User) adminChrome` نوشته شد که هر
هندلر از روی `signedInUser(c)` می‌سازدش.

## صفحه‌های sign-in همچنان `chrome` پرتال را embed می‌کنند، نه `adminChrome`

`NewAdmin` طبق خودِ brief همان `signInHandler` و `accountHandler` پرتال را
دوباره می‌سازد (چهار قدمِ ورود، روی همین listener). این دو type از قبل
`signInPage`/`codePage` را با `chrome` پرتال (نه `adminChrome`) رندر می‌کنند،
و چون کارِ این تسک اجازه نمی‌داد چیزی از پرتال فراتر از تغییرِ نامِ `surface`
دست بخورد، این دو type دست‌نخورده ماندند. امن است چون `admin/layout.html`
`.SuperAdmin` را فقط داخلِ `{{if .SignedIn}}` می‌خواند -- `html/template`
فیلدها را lazy ارزیابی می‌کند، پس رویِ صفحه‌ای که `.SignedIn` رویش همیشه
`false` است (مقدارِ صفرِ `chrome`) آن شاخه هرگز اجرا نمی‌شود و فیلدِ نبود هرگز
خوانده نمی‌شود. `TestTheAdminSignInPagesAreWholeAndHaveNoNavigation` همین را
اثبات می‌کند: صفحه کامل رندر می‌شود، فرمش هست، navی نیست.

## Deliveriesِ یک پیام: query string، نه یک route تازه

Spec نوشته «یک پیام باز می‌شود به deliveryهایش»، ولی جدولِ routeِ `NewAdmin` که
خودِ brief داده هیچ routeِ سوم برایش ندارد. حلش شد با یک query string روی همان
GETِ لاگ: `?message=<id>` -- همان صفحه یک سوالِ بعدی می‌پرسد، نه صفحه‌ی تازه.
`reviewHandler.messages` وقتی این پارامتر هست `Operators.Deliveries` را صدا
می‌زند و نتیجه را کنارِ لیستِ پیام‌ها می‌گذارد.

## صفحه‌ی یک source: سِندرهایش هم هست

اولین نسخه‌ی این commit سِندرها را رها کرده بود، چون `usecase.Operators` متدی
برایش نداشت -- نه در brief، نه در کدِ Task های ۳ تا ۶. Review همین را به‌درستی
یک gap شناخت: specِ صفحه‌ها «its senders» را جزوِ چیزی می‌داند که صفحه‌ی یک
source نشان می‌دهد، و اپراتوری که یک source را بدونِ دیدنِ این‌که چه چیزی
می‌فرستد approve می‌کند، کورکورانه approve می‌کند -- دقیقاً همان چیزی که queue
برایش ساخته شده تا جلویش را بگیرد. سه تکه اضافه شد:

`Operators` یک فیلدِ `credentials credential.Repository` و `NewOperators` یک
پارامترِ هم‌نام گرفت -- پورت از قبل دقیقاً همان چیزی را داشت که لازم بود:
`ListBySourceID`. تنها caller اش در تولید هنوز جایی نیست (مثلِ بقیه‌ی
پارامترهای `NewOperators`)، پس فقط rigِ تستِ `operator_test.go` عوض شد.

`Operators.Senders(ctx, actor, sourceID) ([]credential.Credential, error)` در
`operator_read.go` کنارِ `Messages`/`Deliveries`، از `mayOperate` رد می‌شود نه
از چکِ `super_admin` -- کارِ معمولِ اپراتور است. برخلافِ `Messages`/`Deliveries`
که یک نوعِ تازه (`OperatorMessage`/`OperatorDelivery`) برمی‌گردانند تا چیزی که
نباید دیده شود از رویِ صفحه بیرون بماند، این یکی خودِ `credential.Credential`
را برمی‌گرداند: آن نوع از اول هم فیلدی برای secret ندارد -- unexported است و
accessor ندارد -- پس چیزی برای فیلترکردن نیست.

رویِ `source.html` یک بخشِ «What it sends as» اضافه شد: نام، کانال، روشن یا
خاموش -- نه چیزِ دیگری. حالتِ خالی («Nothing registered... it sends as
srosha») هم نوشته شد، چون یک source بدونِ sender حالتِ معمولی است، نه یک
خرابی -- همان چیزی که پیام‌ِ اول را کار می‌اندازد.

اثباتِ مرز اینجا هم تکرار شد: چکِ `mayOperate` از تویِ `Senders` برداشته شد،
`TestACustomerCannotReadSenders` قرمز شد با «a customer read the sender
list»، برگردانده شد، دوباره سبز شد.

## استایل: همان زبانِ طراحی، ستونِ عریض‌تر

`public/static/admin/admin.css` همان tokenهای `portal.css` را عیناً کپی
می‌کند -- رنگ‌ها، فونت‌ها، `.card`/`.pill`/`.problem`/`.nav` و بقیه. چیزی که
اضافه شد چیزی است که یک پنلِ ادمین می‌خواهد و فرمِ sign-in نه: `.form` از
۴۰۴px به ۹۶۰px عریض شد (بیشترِ صفحه‌های این سطح جدول یا لیست‌اند، نه یک فرمِ
تنها)، و `.narrow` صفحه‌های sign-in/code را دوباره به عرضِ پرتال برمی‌گرداند.
`.table`/`.table-wrap` برای لاگ، roster و audit، و `button.danger` برای
تصمیم‌هایی که یک source یا یک آدم را کنار می‌گذارند (refuse، suspend، switch
off).

## اثباتِ مرز

طبقِ خواسته‌ی تسک، مرز عمداً شکسته شد و برگردانده شد، دوبار:

1. `operator()` در `session.go` موقتاً شد `func operator(u *user.User) bool { return true }`.
   `TestTheAdminSurfaceRefusesACustomer` قرمز شد -- یک مشتری به `/`، `/sources`
   و `/audit` رسید. برگردانده شد، سبز شد.
2. یک routeِ ادمین رویِ engineِ پرتال mount شد: `authed.GET("/queue",
   account.show)` در `portal.go`. `TestNoAdminRouteAnswersOnThePortal` قرمز
   شد -- پرتال با ۲۰۰ جواب داد به‌جایِ ۴۰۴. برگردانده شد، سبز شد.

# Files Changed

- `internal/adapter/api/web/const.go` *(`surfacePortal`, `surfaceAdmin`)*
- `internal/adapter/api/web/portal_const.go` *(`surface` حذف شد، دو جای استفاده در `portal.go` به `surfacePortal` عوض شد)*
- `internal/adapter/api/web/portal.go` *(دو استفاده از `surface` → `surfacePortal`)*
- `internal/adapter/api/web/session.go` *(`superAdmin`)*
- `internal/adapter/api/web/admin.go` *(تازه -- `AdminDeps`, `Operators` interface شاملِ `Senders`, `adminChrome`, `chromeFor`, `NewAdmin`)*
- `internal/adapter/api/web/admin_const.go` *(تازه -- path/page/field constهای این سطح)*
- `internal/adapter/api/web/admin_review.go` *(تازه -- `reviewHandler`, `adminSourcePage.Senders`, `senders()`)*
- `internal/adapter/api/web/admin_people.go` *(تازه -- `peopleHandler`)*
- `internal/adapter/api/web/admin_audit.go` *(تازه -- `auditHandler`)*
- `internal/adapter/api/web/admin_test.go` *(تازه -- شاملِ `memCredentials`)*
- `public/templates/admin/{layout,signin,code,queue,sources,source,log,people,person,audit}.html` *(تازه -- `source.html` بخشِ «What it sends as» را دارد)*
- `public/static/admin/admin.css` *(تازه)*
- `public/static/admin/crane.svg` *(تازه -- کپیِ همان فایلِ پرتال)*
- `internal/core/usecase/operator.go` *(فیلد و پارامترِ `credentials credential.Repository`)*
- `internal/core/usecase/operator_read.go` *(متدِ `Senders`)*
- `internal/core/usecase/operator_read_test.go` *(دو تستِ تازه)*
- `internal/core/usecase/operator_test.go` *(rig یک credential seed می‌کند)*

# Tests Run

- `go build ./...` -- سبز
- `go test -count=1 ./...` -- سبز، همه‌ی پکیج‌ها
- `make prepush` -- سبز (fmt، vet، arch-check، sqlc-check، buf lint، golangci-lint، race tests، `sdk`)
- شکستنِ عمدیِ گارد (`operator` همیشه `true`): `TestTheAdminSurfaceRefusesACustomer`
  قرمز شد با «a customer reached /، /sources، /audit»؛ برگردانده شد، سبز شد.
- شکستنِ عمدیِ mount (یک routeِ ادمین رویِ engineِ پرتال): `TestNoAdminRouteAnswersOnThePortal`
  قرمز شد با «the portal answers /queue with 200»؛ برگردانده شد، سبز شد.
- تست‌های تازه در `admin_test.go`: مرز (چهار تستِ خواسته‌شده در brief، عیناً)،
  کامل‌بودنِ هر صفحه با `whole()`، نمایشِ شرطیِ لینکِ People، چرخه‌ی
  approve/refuse/suspend/restore شاملِ پیغامِ خودِ domain برای «already
  approved»، یک ردیفِ audit به‌ازای هر تصمیم و صفر ردیف برای یک refusalِ
  ناموفق، تغییرِ role توسطِ super_admin، امتناعِ خودزنی (`ErrSelfTarget`)،
  دیدنِ sender ها توسطِ اپراتور، و حالتِ خالی وقتی هیچ sender ای نیست.
- تست‌های تازه در `operator_read_test.go`: `Senders` چیزی که seed شده را
  برمی‌گرداند، و یک customer رد می‌شود (`errors.Is(err, usecase.ErrNotOperator)`).
- شکستنِ عمدیِ دوم: چکِ `mayOperate` از تویِ `Operators.Senders` برداشته شد.
  `TestACustomerCannotReadSenders` قرمز شد با «a customer read the sender
  list»؛ برگردانده شد، سبز شد.

# Follow-ups / Risks

- **فیلترِ `/sources` هنوز نیست.** `AllSources` هیچ پارامترِ فیلتر نمی‌گیرد
  (همان‌طور که در brief آمده بود)؛ صفحه فقط pillِ وضعیت را نشان می‌دهد.
- **`internal/bootstrap` و `internal/config` دست نخوردند** -- طبقِ خواسته،
  وصل‌کردنِ این سطح به listenerِ سومِ `console` (`:8092`) کارِ Task 8 است.
  `NewAdmin` امروز به هیچ‌جا صدا زده نمی‌شود جز از `admin_test.go`.

# Instruction

اجرای Task 7 از طرحِ admin panel طبقِ
`.superpowers/sdd/2026-08-30-admin-panel/task-7-brief.md`، با سه تصحیحی که
پیش از شروع داده شد (اسمِ `guarded`، `adminChrome` جدا، embedِ آن رویِ هر صفحه
شاملِ sign-in): سطحِ HTML دومِ باینریِ `console` ساخته شود و به
`usecase.Operators` وصل شود -- بدونِ دست‌زدن به `bootstrap`/`config`، و بدونِ
تغییری در پرتال فراتر از تغییرِ نامِ ثابتِ `surface`.

و بعد، از reviewِ همان تسک: صفحه‌ی یک source سِندرهایش را هم نشان بدهد --
`Operators` یک `credential.Repository` بگیرد، `Operators.Senders` نوشته شود
(کنارِ `Messages`/`Deliveries`، با همان چکِ `mayOperate`)، و `source.html` یک
بخشِ «What it sends as» بگیرد که هرگز secret را نشان نمی‌دهد و حالتِ نبودنِ
sender را به‌جایِ یک لیستِ خالی توضیح می‌دهد.
