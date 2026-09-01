Branch: `feat/admin-architecture-page`

# Summary

دیاگرام معماری سرویس ساخته شد، در repo ذخیره شد، و پنل ادمین یک لینک تازه گرفت که
بازش می‌کند — فقط برای `super_admin`.

## دیاگرام از کجا آمد

با skill به اسم `archify` ساخته شد: یک فایل json ورودی می‌گیرد و یک صفحهٔ html
کامل و مستقل بیرون می‌دهد — svg داخل خودش، تم روشن و تیره، zoom و pan، و سه
«نمای راهنما». آن json در `docs/assets/brand/srosha.architecture.json` ماند تا
دفعهٔ بعد از روی همان دوباره ساخته شود و کسی مجبور نباشد html را دستی ویرایش کند.

یازده node دارد و هرکدام به فایل واقعی خودش در repo وصل است، روی revision
`51c14ee`. مسیر اصلی از چپ به راست است:

```
Customer service → Traefik → gateway → NATS → dispatcher → channel providers
```

و کنارش postgres، console، migrate و callback. دو boundary هم دارد: خودِ VPS، و
`srosha-net` که هیچ host port منتشر نمی‌کند.

`Gotify` مربوط به alert اپراتور **در دیاگرام نیست**، و این یک تصمیم است نه فراموشی:
هر سه binary مستقیم به آن می‌زنند، و کشیدن هر سه edge مسیر اصلی را از خوانایی
می‌انداخت. اگر روزی لازم شد، جای درستش یک دیاگرام جدا برای مسیرهای عملیاتی است.

## `public/` نیمهٔ سوم گرفت

فایل html باید داخل binary باشد تا سرو شود، و `go:embed` بیرون از دایرکتوری خودش
را نمی‌بیند — پس باید زیر `public/` می‌رفت. هیچ‌کدام از دو نیمهٔ موجود جایش نبود:

```
static/     مرورگر بایت به بایت می‌گیرد   → هرکس url را حدس بزند می‌بیند
templates/  روی سرور render می‌شود        → این template نیست، سند کامل است
guarded/    بایت به بایت، ولی پشت گارد    ← تازه
```

`web.guardedFile(surface, name)` **یک فایلِ نام‌برده** را می‌خواند، نه یک `fs.FS`.
تفاوتش با `browserFiles` عمدی است: اگر یک file system پشت route می‌نشست، مسیرِ
داخل request فایل را انتخاب می‌کرد و گارد از دایرکتوری‌ای محافظت می‌کرد که کسی
محتویاتش را ندیده. اینجا اسم فایل موقع ساختن surface تعیین می‌شود و request هیچ
چیزی را انتخاب نمی‌کند.

خواندنش هم موقع `NewAdmin` است نه موقع request: فایل نبودن یعنی console بالا
نمی‌آید، نه صفحه‌ای که بار اول ۵۰۰ می‌دهد.

## `super_admin`، به همان دلیلی که `/audit` هست

`/architecture` روی گروه `top` نشست، کنار `/audit` و `/people` — نه روی `guarded`.
دیاگرام اسم هر host، هر port، هر store و شبکهٔ خصوصی‌ای که رویش نشسته‌اند را
می‌برد. این شکلِ deployment است، و اپراتوری که source تأیید می‌کند کاری با آن ندارد.

لینکش هم داخل همان شاخهٔ `{{if .SuperAdmin}}` در nav است، کنار Audit و People، با
`target="_blank"` چون یک سند تمام‌صفحه است نه یک صفحهٔ پنل.

## یک چیز که قبل از commit از فایل حذف شد

خروجی `archify` سه تا `<link>` به Google Fonts در head داشت. `docs/CONFIG.md` صریح
می‌گوید هیچ چیزی اینجا حق ندارد موقع اجرا به یک font host برسد — پنل باید روی
شبکه‌ای که نمی‌تواند هم کار کند. هر سه حذف شدند؛ فونت stack خودش fallback دارد
(`ui-monospace`, `Menlo`, `Consolas`, …) و ظاهر عوض نشد.

این چیزی است که دفعهٔ بعد فراموش می‌شود، پس تست نگهش می‌دارد:
`TestAnAdminIsRefusedOnTheArchitectureDiagram` روی بایت‌های واقعیِ سرو شده assert
می‌کند که هیچ نام font host در آن نیست.

## تست‌ها

`TestAnAdminIsRefusedOnTheArchitectureDiagram` تازه است: یک `admin` رد می‌شود، یک
`super_admin` سند را می‌گیرد، `<svg` داخلش هست، و هیچ font host در آن نیست.

منفی‌اش هم امتحان شد — route از `top` به `guarded` منتقل شد و تست شکست. یعنی
واقعاً گارد را نگه می‌دارد و صرفاً سبز نیست.

`TestNoAdminRouteAnswersOnThePortal` خودش route را از جدولِ زندهٔ engine می‌خواند،
پس `/architecture` را بدون هیچ کاری چک کرد؛ فقط `AdminOnlyPaths` — کفِ آن تست —
اسمش را گرفت تا اگر روزی route بی‌صدا حذف شد، تست بشکند.

`TestTheNavShowsPeopleOnlyToASuperAdmin` حالا سه لینک را می‌بیند نه دو تا، و چون
اسمش دیگر رفتارش را نمی‌گفت به `TestTheNavShowsTheTopLinksOnlyToASuperAdmin`
تغییر کرد.

`TestEveryAdminPageIsWhole` عمداً `/architecture` را نام نمی‌برد: آن صفحه layout و
nav و دکمهٔ خروج ندارد، چون صفحهٔ پنل نیست.

# Files Changed

- `docs/assets/brand/srosha.architecture.json` *(تازه — منبع دیاگرام)*
- `public/guarded/admin/architecture.html` *(تازه — سندِ سرو شده، بدون font host)*
- `public/embed.go` *(`guarded` هم embed شد، و توضیحِ سه‌نیمه‌ای بازنویسی شد)*
- `internal/adapter/api/web/assets.go` *(`guardedFile` اضافه شد)*
- `internal/adapter/api/web/admin_architecture.go` *(تازه — `architectureHandler`)*
- `internal/adapter/api/web/admin_const.go` *(`pathArchitecture`, `fileArchitecture`)*
- `internal/adapter/api/web/admin.go` *(سند خوانده و route روی گروه `top` سوار شد)*
- `internal/adapter/api/web/export_test.go` *(`AdminOnlyPaths`)*
- `internal/adapter/api/web/admin_test.go` *(یک تست تازه، یک تست تغییرِ نام‌یافته)*
- `public/templates/admin/layout.html` *(لینک nav، داخل شاخهٔ `SuperAdmin`)*
- `docs/CONFIG.md` *(نیمهٔ سوم `public/`، فایل و route و منبعش، و قاعدهٔ font)*

# Tests Run

- `go build ./...` — clean
- `golangci-lint run` — clean
- `go test -race ./...` — pass
- `make arch-check` — clean

# Follow-ups / Risks

- **`public/**` در watch pathهای auto-deploy نیست.** این از قبل بوده و مالِ این
  تغییر نیست — templateهای portal هم همان‌جا هستند — ولی حالا یک فایل ۷۰۷KB هم
  آنجاست. این بار deploy می‌افتد چون `internal/**` هم عوض شده. دفعه‌ای که فقط
  دیاگرام دوباره ساخته شود، نمی‌افتد. تصمیمِ اضافه کردنش با مالک است.
- **binary حدود ۷۰۷KB بزرگ‌تر شد**، در هر سه سرویس نه فقط console، چون `public`
  یک package است و همه یک image می‌سازند. آگاهانه پذیرفته شد.
- دیاگرام به revision `51c14ee` سنجاق شده — لینک‌های `SRC` روی node‌ها به همان
  commit در GitHub می‌روند. یعنی با گذشت زمان کهنه می‌شود و بازسازی‌اش دستی است.
  چیزی آن را چک نمی‌کند.

# Instruction

دیاگرام معماری‌ای که در پیام قبل ساخته شد، داخل repo ذخیره شود، و در پنل ادمین
دکمه/لینکی گذاشته شود که آن را نشان بدهد — **فقط** برای `super_admin`.
