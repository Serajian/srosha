Branch: `feat/admin-panel`

# Summary

سه تغییرِ به‌هم‌وابسته، یک instruction: صفحه‌ی ادمینِ یک source جایی که پیام‌هایش
می‌رود را نشان نمی‌داد، domain اجازه می‌داد sourceـی که هیچ آدرسی ندارد فعال
شود، و صفحه‌ی مشتری به چنین sourceـی می‌گفت «صبر کن» درحالی‌که هیچ‌وقت تأیید
نمی‌شد.

## گارد در domain: `Source.IsReachable`

قاعده تازه: `Approve` و `Restore` -- تنها دو متدی که `IsActive` را روشن
می‌کنند -- حالا رد می‌شوند اگر sourceـی هم `DefaultAddresses` نداشته باشد و هم
`AllowCustomAddress` اش خاموش باشد. هرکدام به‌تنهایی کافی است؛ فقط ترکیبِ هر دو
یعنی sourceـی که هیچ‌جا نمی‌تواند پیام بفرستد.

سوال به یک متد داده شد -- `IsReachable() bool` روی `*Source` -- تا هم گارد و
هم صفحه‌ی پرتال همان سوال را بپرسند، نه دو کپیِ جدا از یک قاعده:

```go
func (s *Source) IsReachable() bool {
	return len(s.DefaultAddresses) > 0 || s.AllowCustomAddress
}
```

`Approve` قبلاً هیچ error برنمی‌گرداند؛ حالا برمی‌گرداند، دقیقاً شکلِ
`Refuse`/`Suspend`/`Restore` که از قبل بودند. پیام خطا می‌گوید چه چیزی کم است
و چه کسی می‌تواند درستش کند -- اپراتور نمی‌تواند، فقط مشتری با اضافه‌کردنِ یک
آدرس. Sentinel تازه `source.ErrNoReachableAddress` کنارِ `ErrAlreadyApproved`
در `errors.go`.

`New` دست‌نخورده ماند: ثبت‌نامِ یک source بدونِ آدرس هنوز قانونی است -- مشتری
قبل از تأیید شدن سیستم را می‌سازد -- قاعده فقط دربارهٔ فعال‌سازی است، نه ساخت.

## صفحه‌ی ادمینِ یک source: حالا می‌گوید کجا می‌فرستد

`source.html` بخشِ «Where it sends» گرفت -- کانال و آدرس، همان شکلِ کارتی که
«What it sends as» برای senderها دارد. **بدونِ ماسک**، برخلافِ
`/sources/:id/log` که آدرسِ گیرنده -- شخصِ ثالث -- را ماسک می‌کند: این‌ها
تنظیماتِ خودِ مشتری‌اند، بخشی از چیزی که یک اپراتور دارد دربارهٔ آن قضاوت
می‌کند، و ماسک‌کردنشان دلیلِ وجودِ این بخش را از بین می‌برد. تفاوت در کامنتِ
بالای بخش نوشته شد.

حالتِ خالی گفته می‌شود که معمولی است -- زمانِ ثبت‌نام -- و وقتی
`AllowCustomAddress` هم خاموش است، می‌گوید همین ترکیب یعنی این source جایی
برای فرستادن ندارد و تأیید نمی‌شود تا مشتری آدرسی اضافه کند.

خودِ صفحه هم پیش‌بینی می‌کند، اما نه با صدازدنِ `Approve`/`Restore`: نسخه‌ی
اولِ این کار دقیقاً همین کار را می‌کرد -- روی یک کپی از source این دو متد را
صدا می‌زد تا پیامِ خطا را بخواند -- و در بازبینی رد شد، به‌درستی: `Approve` و
`Restore` برای **جابه‌جا کردنِ** یک source ساخته شده‌اند، نه برای **جواب دادن
به یک سوال**؛ استفاده از آن‌ها به‌عنوانِ پیش‌بینی یعنی هر اثرِ جانبیِ تازه‌ای
که این دو یک روز بگیرند (یک متریک، یک خطِ لاگ) رویِ هر بارِ رندرِ صفحه هم
اجرا می‌شود. `time.Now()` هم برای امضای متد لازم بود و نتیجه‌اش هیچ‌وقت
خوانده نمی‌شد -- تشریفات برای راضی‌کردنِ یک امضا. و شاخه‌ی
`if IsReviewed() {Restore} else {Approve}` یک کپیِ دومِ همان تصمیمی بود که
از قبل در use case هست.

نسخه‌ی نهایی مستقیم سوال را می‌پرسد: `cannotBeLetOut` فقط `Source.IsReachable`
را می‌خواند، بدونِ کپی، بدونِ ساعت، بدونِ صدازدنِ هیچ گذاری. بررسی شد که آیا
`Restore` گاردِ دیگری هم دارد که `IsReachable` پوشش نمی‌دهد: `Restore` گاردِ
`IsReviewed` را هم دارد، اما در همان شاخه‌ی template که دکمه‌ی Restore را نشان
می‌دهد، `IsReviewed` از قبل true است (چون شاخه‌ی مقابلش -- «هنوز بررسی نشده» --
دکمه‌ی Approve را نشان می‌دهد). پس در بافتِ این صفحه، `IsReachable` تنها دلیلِ
ممکن برای شکستِ هرکدام از دو دکمه است -- و اگر یک روز `Restore` گاردِ تازه‌ای
گرفت که `IsReachable` پوشش نمی‌دهد، این فرض باید دوباره بررسی شود (کامنتِ بالای
تابع همین را می‌گوید).

## صفحه‌ی مشتری: «صبر کن» دیگر همیشه درست نیست

`portal/source.html` شاخه‌ی waiting دو شد. اگر `Source.IsReachable` باشد، همان
جملهٔ قبلی. اگر نه، می‌گوید هیچ‌کس -- نه اپراتور -- نمی‌تواند این source را
تأیید کند تا وقتی آدرسی اضافه شود، و به `/sources/:id/edit` اشاره می‌کند.
قاعده از همان متدِ domain خوانده می‌شود، نه یک کپیِ تازه در template.

# Files Changed

- `internal/core/domain/source/entity.go` *(`IsReachable`؛ `Approve` حالا
  error برمی‌گرداند و گارد دارد؛ `Restore` همان گارد را گرفت)*
- `internal/core/domain/source/errors.go` *(`ErrNoReachableAddress`)*
- `internal/core/usecase/operator.go` *(`Approve` خطای domain را قبل از gate
  چک می‌کند)*
- `internal/adapter/api/web/admin_review.go` *(`cannotBeLetOut` -- می‌پرسد
  `IsReachable`، پیش از فشردنِ دکمه؛ بعد از بازبینی، بدونِ کپی و بدونِ صدازدنِ
  `Approve`/`Restore`)*
- `public/templates/admin/source.html` *(بخشِ «Where it sends»)*
- `public/templates/portal/source.html` *(شاخهٔ waiting دو حالته شد)*
- تست‌ها: `internal/core/domain/source/service_test.go`,
  `internal/core/usecase/operator_test.go`,
  `internal/adapter/api/web/admin_test.go`,
  `internal/adapter/api/web/portal_test.go`,
  `internal/adapter/db/postgres/source_test.go` *(فیکسچرهایی که `Approve`
  صدا می‌زدند و آدرس نداشتند، یا آدرس گرفتند یا `AllowCustomAddress=true`)*

# Tests Run

- `go build ./...` — سبز
- `go test -count=1 ./...` — سبز، همه‌ی پکیج‌ها
- `go test -count=1 -tags=integration ./internal/adapter/db/postgres/` — سبز
- `make prepush` — سبز (fmt، vet، arch-check، sqlc-check، buf lint،
  golangci-lint، race tests، sdk)
- هر گارد عمداً برداشته شد و تستِ مربوطه fail شد، بعد برگردانده شد: گاردِ
  `Approve`، گاردِ `Restore`، شاخه‌ی پرتال (با `{{else if false}}`)، تابعِ
  `cannotBeLetOut` صفحه‌ی ادمین (خروجی‌اش همیشه `""` شد)، و بخشِ «Where it
  sends» (کامنت‌شده). هر پنج‌بار خروجی واقعاً قرمز بود، نه cache و نه خالی.

# Follow-ups / Risks

- پیامِ خطا حالا در سه جا literal است: `Approve`، `Restore` و
  `cannotBeLetOut`. دو تای اولی طبقِ بازبینی همین‌طور می‌مانند -- سبکِ فایل است.
  سومی جای دیگری است (لایه‌ی web) و عمداً از صدازدنِ domain خودداری می‌کند؛
  اگر متنِ پیام یک روز عوض شد، هر سه جا باید با هم عوض شوند و چیزی این را
  خودکار چک نمی‌کند.
- `cannotBeLetOut` فرض می‌کند در بافتِ صفحه‌ی خودش `Restore` فقط به دلیلِ
  `IsReachable` رد می‌شود (چون `IsReviewed` در آن شاخه از قبل true است). این
  فرض در کامنتِ بالای تابع نوشته شده؛ اگر `Restore` یک‌روز گاردِ تازه‌ای گرفت،
  این فرض باید دوباره بررسی شود.

# Instruction

دو تغییرِ آغازین از owner: صفحه‌ی ادمینِ یک source باید `DefaultAddresses` اش
را نشان بدهد (بدونِ ماسک، برخلافِ لاگِ deliveryها)، و sourceـی که هیچ آدرسِ
پیش‌فرض و اجازه‌ی آدرسِ سفارشی ندارد نباید بتواند فعال شود (`Approve`/`Restore`
در domain، sentinel کنارِ `ErrAlreadyApproved`). حینِ کار، owner یک تغییرِ سوم
اضافه کرد که به همین کار تعلق دارد: صفحه‌ی خودِ مشتری هم باید بگوید یک source
بدونِ آدرس تأیید نمی‌شود و مشتری خودش باید آدرس اضافه کند -- نه یک کپیِ تازه از
قاعده، بلکه همان متدِ domain. هر سه یک instruction و یک گزارش.
