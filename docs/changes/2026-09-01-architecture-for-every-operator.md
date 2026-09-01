Branch: `feat/architecture-for-every-operator`

# Summary

لینک و صفحهٔ `/architecture` از `super_admin` باز شد و حالا **هر operator** — چه
`admin` چه `super_admin` — می‌بیندش. تا همین دیروز، در `feat/admin-architecture-page`
که با MR شمارهٔ ۶۴ داخل `master` شد، `super_admin` بود.

## چرا عوض شد

استدلال اولیه همان استدلال `/audit` بود: دیاگرام اسم هر host، هر port، هر store و
شبکهٔ خصوصی را می‌برد، و این شکلِ deployment است.

مالک تصمیم گرفت باز شود، با این دلیل که operator‌ای که باید قضاوت کند یک source
اجازهٔ ارسال دارد یا نه، دارد دربارهٔ سیستمی نظر می‌دهد که اجازه نداشته شکلش را
ببیند.

عارضه‌اش ثبت می‌شود و پوشانده نمی‌شود: **یک `admin` حالا هاست‌ها، پورت‌ها و
storeها را می‌خواند.** این هزینهٔ آگاهانهٔ آن تصمیم است، نه چیزی که از قلم افتاده
باشد.

## چه چیزی جابه‌جا نشد

گاردِ جلویش. route روی گروه `guarded` نشست که قاعده‌اش `operator` است، نه بیرونِ
هر گاردی. یعنی مشتری‌ای که session معتبرِ portal دارد — و چون cookie با port
scope نمی‌شود، به همین listener هم می‌رسد — رد می‌شود.

و همچنان `/static` نیست: هر چیزی زیر `public/static/admin/` بدون هیچ sessionی
گرفته می‌شود، و این سند نمی‌شود.

## تست‌ها

`TestAnAdminIsRefusedOnTheArchitectureDiagram` معنایش را از دست داد و جایش دو تا
آمد، چون حالا دو ادعای جدا هست:

- `TestEveryOperatorReachesTheArchitectureDiagram` — هر دو نقش سند را می‌گیرند،
  `<svg` داخلش هست، و هیچ font host در بایت‌ها نیست.
- `TestACustomerIsRefusedOnTheArchitectureDiagram` — نیمه‌ای که جابه‌جا نشد.

`TestTheNavShowsTheTopLinksOnlyToASuperAdmin` هم دیگر اسمش رفتارش را نمی‌گفت:
`/architecture` از فهرست لینک‌های `super_admin` بیرون آمد، و به‌جایش **برعکسش**
assert می‌شود — لینک روی صفحهٔ **هر دو** نقش باید باشد. اسم شد
`TestTheNavShowsPeopleAndAuditOnlyToASuperAdmin`.

آن assert برعکس عمدی است: خطایی که می‌گیرد این است که لینک دوباره داخل شاخهٔ
`{{if .SuperAdmin}}` سُر بخورد، و هیچ چیز دیگری در این فایل متوجهش نمی‌شد.

هر دو جهت امتحان شد و هر دو شکستند وقتی باید می‌شکستند:

```
route به گروه top برگشت      → TestEveryOperatorReachesTheArchitectureDiagram
                                "an operator whose role is admin ... 303"
لینک به {{if .SuperAdmin}}   → TestTheNavShowsPeopleAndAuditOnlyToASuperAdmin
                                "an admin's queue page does not offer ..."
```

# Files Changed

- `internal/adapter/api/web/admin.go` *(route از گروه `top` به `guarded` رفت، با دلیلش)*
- `internal/adapter/api/web/admin_architecture.go` *(توضیح handler بازنویسی شد)*
- `internal/adapter/api/web/admin_const.go` *(توضیح `pathArchitecture`)*
- `internal/adapter/api/web/admin_test.go` *(یک تست شد دو تا؛ تست nav برعکس شد و اسمش عوض شد)*
- `public/templates/admin/layout.html` *(لینک از شاخهٔ `SuperAdmin` بیرون آمد)*
- `docs/CONFIG.md` *(route، و چرا باز شد و چه چیزی هنوز جلویش هست)*

# Tests Run

- `go build ./...` — clean
- `golangci-lint run` — clean
- `go test -race ./...` — pass
- `make arch-check` — clean

# Follow-ups / Risks

- **یک `admin` حالا شکلِ deployment را می‌خواند.** بالا توضیح داده شد؛ تصمیم
  آگاهانه است، ولی اگر روزی نقش سومی بین این دو لازم شد، این همان جایی است که
  اولین بار به آن برمی‌خوریم.
- `AdminOnlyPaths` دست نخورد: `/architecture` همچنان مسیری است که فقط این surface
  جواب می‌دهد، و مرزِ ۴۰۴ با portal بی‌تغییر است.

# Instruction

دکمهٔ architecture برای `admin` هم دیده شود، نه فقط `super_admin`.
