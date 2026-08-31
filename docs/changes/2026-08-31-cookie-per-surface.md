Branch: `refactor/admin-on-its-own-host`

# Summary

هر surface حالا cookie ــِ خودش را دارد: portal با `srosha_portal` و admin با
`srosha_admin`. این task یک از plan ــِ
`docs/superpowers/plans/2026-08-31-admin-on-its-own-host.md` است.

`sessions` تنها چیزی در این adapter است که cookie را می‌خواند و می‌نویسد، و از
قبل هر surface نمونهٔ خودش را می‌ساخت — فقط هر دو یک نام می‌نوشتند. حالا نام یک
فیلد است:

```go
func newSessions(signIn SignIn, name string, secure bool) *sessions
```

و `sessionCookieName` جایش را به دو ثابت داد. کامنتِ قبلی‌اش می‌گفت «یک نامِ دوم
هم آن‌ها را جدا نمی‌کرد، چون cookie با port اسکوپ نمی‌شود» — که راست بود و دیگر
نیست: دو surface قرار است با **host** جدا شوند نه با port، و cookie با host
اسکوپ **می‌شود**.

در `begin` یک کامنت اضافه شد که چرا هیچ `Domain` ای ست نمی‌شود. آن نبودن است که
cookie را host-only نگه می‌دارد، و اگر روزی کسی `Domain=srosha.ir` بگذارد، کلِ
این جدایی بی‌صدا از بین می‌رود.

# چیزی که plan ندیده بود

بعد از تغییر، `TestASourceThatCanBeApprovedShowsNoWarning` و چند تستِ دیگر
شکستند. دلیلش درست بود: helper ــِ مشترکِ `answer.session()` نامِ cookie را
literal نوشته بود (`"srosha_portal"`)، پس روی surface ــِ admin دیگر چیزی پیدا
نمی‌کرد.

به `sessionNamed(name string)` تبدیل شد و دو نام از `export_test.go` عبور داده
شدند — دقیقاً به همان دلیلی که جدولِ route از همان‌جا عبور می‌کند و خودِ آن فایل
نوشته: نامی که در تست دوباره literal نوشته شود، نامی است که می‌تواند از کدِ
اصلی جدا بیفتد و تست همچنان سبز بماند.

# چیزی که قدمِ ۸ ــِ plan گیر انداخت

plan اجبار می‌کند تستِ تازه را عمداً قرمز کنی. کردم — و **قرمز نشد.**

`TestOneSurfaceDoesNotReadTheOthersCookie` مکانیزم را تست می‌کند: `sessions` را
با نامِ صریح می‌سازد. پس اگر `admin.go` ثابتِ اشتباه را پاس بدهد، آن تست هیچ
خبری ندارد. سیم‌کشی تست نشده بود.

کلِ پکیج با آن خرابی می‌شکست، ولی به‌شکلِ ده‌ها خطِ «could not sign in» که
نمی‌گوید چرا. دو تست اضافه شد که مستقیم اسمِ عیب را می‌برند:
`TestTheAdminSurfaceWritesItsOwnCookie` و `TestThePortalWritesItsOwnCookie`.
دوباره خراب کردم و این بار پیامش این بود:

> the admin surface set the portal's cookie, so a customer's session would be
> presented here rather than never sent

بعد برگرداندم و سبز شد.

# Files Changed

- `internal/adapter/api/web/const.go` *(دو ثابت به‌جای یکی، با کامنتِ تازه)*
- `internal/adapter/api/web/session.go` *(فیلدِ `name`؛ کامنتِ نبودنِ `Domain`)*
- `internal/adapter/api/web/portal.go` *(پاس دادنِ `portalCookieName`)*
- `internal/adapter/api/web/admin.go` *(پاس دادنِ `adminCookieName`)*
- `internal/adapter/api/web/export_test.go` *(دو نام از مرز عبور می‌کنند)*
- `internal/adapter/api/web/session_test.go` *(دو تستِ تازه + به‌روزرسانیِ helper ها)*
- `internal/adapter/api/web/admin_test.go` *(تستِ سیم‌کشی؛ `sessionNamed`)*
- `internal/adapter/api/web/portal_test.go` *(تستِ سیم‌کشی؛ `session` → `sessionNamed`)*

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass، بدون هیچ شکستی
- `make precommit` — pass
- خراب‌کردنِ عمدی دو بار: بارِ اول ثابت کرد تستِ واحد کافی نیست، بارِ دوم ثابت کرد
  تستِ تازه واقعاً قرمز می‌شود

# Follow-ups / Risks

- تا وقتی task 2 انجام نشده، هیچ‌چیز عوض نشده که پنل را روی اینترنت ببرد. این
  commit به‌تنهایی فقط دو نام است و بی‌خطر.
- `NOTIF_ADMIN_ADDR` هنوز در production باید loopback باشد. task 2 آن را
  برمی‌دارد، و **نباید** بدونِ این commit انجام شود.

# Instruction

مالک گفت plan ــِ `refactor/admin-on-its-own-host` شروع شود. این task یک از سه
تا است: cookie به ازای هر surface، به‌همراه تست‌هایی که جدایی را نگه می‌دارند.
