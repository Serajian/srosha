Branch: `feat/admin-panel`

# Summary

Task 9 از طرحِ admin panel: صفحهٔ `source.html` در پورتالِ مشتری حالا هر چهار
وضعیتِ یک source را از هم تشخیص می‌دهد، نه دو تا. تا اینجا `sources.is_active`
`false` هم برای «هنوز تأیید نشده» بود و هم برای «رد شد» و هم برای «تأیید شد و
بعد خاموش شد» -- و هر سه یک جمله می‌گرفتند: «Waiting for approval». یک source
که هفته‌ها پیش رد شده بود، همچنان می‌گفت منتظر است.

`{{if not .Source.IsActive}}` حالا سه شاخه دارد به‌جای دو تا:

```
IsReviewed && !IsApproved   →  رد شده — This source was not approved. {{.ReviewNote}}
IsApproved                  →  تأیید شده و بعد خاموش — An operator switched this source off.
else (=  !IsReviewed)       →  منتظر — Waiting for approval.
```

**ترتیب عمدی است.** شاخهٔ رد‌شده باید قبل از شاخهٔ منتظر چک شود چون یک source
ردشده هم `IsApproved() == false` است -- اگر شاخهٔ «منتظر» (که چیزی جز `else`
نیست) قبل از شاخهٔ رد‌شده بیاید، یک source ردشده که هیچ‌وقت واقعاً approved
نبوده باز هم می‌افتد توی `else` و می‌گوید «Waiting». تستِ زیر دقیقاً همین را
قفل می‌کند.

## تصمیمی که brief باز گذاشته بود: آیا دلیلِ suspend هم نشان داده شود؟

جواب: نه، و دلیلش در خودِ کد به‌صورتِ کامنت نوشته شده. `Source.Suspend` عمداً
`ReviewNote` را دست نمی‌زند -- طبقِ خودِ کامنتِ `entity.go`. اما این یعنی وقتی
یک source به وضعیتِ suspended می‌رسد (`IsReviewed && IsApproved && !IsActive`)،
`ReviewNote` **همیشه خالی است**: تنها دو راهی که به این وضعیت می‌رسند `Approve`
و `Restore` هستند، و هر دو خودشان `ReviewNote = ""` می‌نویسند در همان فراخوانی
که `ApprovedAt` را ست می‌کنند. پس چیزی برای نشان‌دادن نیست -- نمایشِ
`{{.Source.ReviewNote}}` روی این شاخه همیشه یک رشتهٔ خالی بعد از جمله می‌گذاشت،
بدونِ هیچ اطلاعاتی. جملهٔ ثابتِ «An operator switched this source off» درست
است چون تنها چیزی که مشتری لازم دارد بداند همین است: کار می‌کرد، الان نه.

## و همان چهار حالت روی فهرست هم آمد

اول فقط `source.html` عوض شد، و فهرست (`sources.html`) دست‌نخورده ماند --
همان‌جا که مشتری اول می‌بیند. Coordinator بعد از بررسی این را برگرداند: یک
source که سه هفته پیش رد شده، در فهرست همچنان «Waiting for approval» می‌گفت،
دقیقاً همان دروغی که این تسک قرار بود از بین ببرد.

فهرست حالا همان چهار حالت را دارد، ولی به شکلِ **pill** چون جا کم است، نه
پاراگراف:

```
IsActive                     →  pill ok    "Sending"
IsReviewed && !IsApproved    →  pill bad   "Not approved"   + "Open it to read why."
IsApproved                   →  pill off   "Switched off"   + همان جملهٔ ثابت
else (= !IsReviewed)         →  pill hold  "Waiting for approval" + همان راهنمای قبلی
```

همان قاعدهٔ ترتیب دوباره: شاخهٔ رد‌شده قبل از fallbackِ منتظر چک می‌شود، و کامنتِ
بالای شرط به `source.html` ارجاع می‌دهد تا دلیل دوبار نوشته نشود.

**دلیلِ رد روی فهرست نمی‌آید** -- طبقِ تصمیم: فهرست می‌گوید source در چه
وضعیتی است، نه چرا. پاراگرافِ `.why` کنارِ pillِ «Not approved» فقط می‌گوید
«Open it to read why.» -- دعوت به بازکردنِ صفحه، نه تکرارِ `ReviewNote`.

رنگِ pillِ تازه (`.pill.bad`) از تاکنِ موجودِ `--bad` (`#E4726A`) ساخته شد، با
همان الگویی که سه pillِ دیگر دارند: زمینه با opacityِ کم از همان رنگ، متن با
خودِ توکن، حاشیه با opacityِ بیشتر. هیچ رنگِ تازه‌ای اضافه نشد -- `--bad` از
اول در `portal.css` بود و روی پاراگرافِ `.problem` در صفحهٔ یک source همین
الان استفاده می‌شود؛ فقط یک modifierِ pill برایش نبود.

# Files Changed

- `public/templates/portal/source.html` *(دو شاخه شد سه شاخه؛ کامنتِ بالای شرط
  ترتیب و تصمیمِ suspend را توضیح می‌دهد)*
- `public/templates/portal/sources.html` *(دو شاخه شد چهار شاخه؛ همان ترتیب،
  pillِ تازهٔ `bad` برای رد‌شده)*
- `public/static/portal/portal.css` *(`.pill.bad`، از رویِ توکنِ موجودِ
  `--bad`)*
- `internal/adapter/api/web/portal_test.go` *(`TestARefusedSourceShowsItsReason`
  -- عیناً همان تستِ brief؛ `TestARefusedOrSuspendedSourceDoesNotReadAsWaitingOnTheList`
  -- تازه، روی صفحهٔ فهرست)*

# Tests Run

- `go build ./...` -- سبز
- `go test -count=1 ./...` -- سبز، همهٔ پکیج‌ها
- `make prepush` -- سبز (fmt، vet، arch-check، sqlc-check، buf lint،
  golangci-lint، race tests، sdk)
- شکستنِ عمدیِ اول (صفحهٔ یک source): شاخهٔ رد‌شده را به شرطِ شاخهٔ منتظر
  (`not .Source.IsReviewed`) وصل کردم. `TestARefusedSourceShowsItsReason`
  قرمز شد، با هر دو خطا: «the customer is not told why» و «a refused source
  still says it is waiting». برگردانده شد، دوباره سبز شد.
- شکستنِ عمدیِ دوم (فهرست): همان کار روی `sources.html` -- شرطِ pillِ
  «Not approved» شد `not .IsReviewed`.
  `TestARefusedOrSuspendedSourceDoesNotReadAsWaitingOnTheList` قرمز شد با
  «a refused or suspended source still reads as waiting for approval».
  برگردانده شد (با `diff` تأیید شد که فایل عیناً همان قبل از شکستن است)،
  دوباره سبز شد.

# Follow-ups / Risks

- هیچ. فهرست و صفحهٔ یک source حالا هر دو یک منطق دارند، و pillِ رد‌شده مشتری
  را به همان جایی می‌فرستد که دلیل هست.

# Instruction

اجرای Task 9 از طرحِ admin panel طبقِ
`.superpowers/sdd/2026-08-30-admin-panel/task-9-brief.md`: صفحهٔ یک source در
پورتالِ مشتری بینِ منتظر، تأیید‌شده، رد‌شده و suspended فرق بگذارد -- با تستِ
دقیقاً همانی که brief داده، و ترتیبِ شاخه‌ها (رد‌شده قبل از منتظر) رعایت شود.
تصمیمِ نشان‌دادن یا ندادنِ دلیلِ suspend به عهدهٔ من گذاشته شده بود.

بعدش، از بازبینیِ coordinator: فهرستِ `sources.html` هم همان چهار حالت را
بگیرد -- pill کوتاه است، رد‌شده و suspended باید متنِ متفاوت داشته باشند، دلیل
روی فهرست نمی‌آید (فقط روی صفحهٔ خودِ source)، pillِ رد‌شده باید مشتری را به
بازکردنِ صفحه دعوت کند، و رنگِ تازه‌ای اختراع نشود -- از توکن‌های موجودِ
`portal.css` استفاده شود.
