Branch: `feat/sdk-route-constructors`

# Summary

هر یک از ۸ constructor کانالِ Go SDK (`Email`، `Telegram`، `Bale`، `WhatsApp`،
`Matrix`، `Gotify`، `FCM`، `APNs`) یک `address` را positional می‌گرفت. مسئلهٔ
اینجا این بود که حالتِ **رایج** -- یعنی همان آدرسِ پیش‌فرضی که یک source یک‌بار
در پورتال تنظیم می‌کند و بعد تقریباً هر پیام از همان استفاده می‌کند -- بدترین
شکلِ نوشتاری را داشت: `srosha.Telegram("")`. یک رشتهٔ خالی به‌عنوانِ یک مقدارِ
معنادار دقیقاً همان چیزی است که خواننده باید برود و معنایش را نگاه کند.

هر ۸ constructor به دو تا شکافته شد:

```go
srosha.Telegram()               // آدرسِ پیش‌فرضِ همین source
srosha.TelegramTo("123456789")  // آدرسی که پیام خودش نام می‌برد
```

`To(channel, address)` (escape hatch ــِ عمومی برای کانالی که این نسخه هنوز
constructor ای برایش ندارد) و `Route.From(sender)` دست‌نخورده ماندند و روی هر
دو فرمِ تازه chain می‌شوند.

# چرا این یک breaking change است، و نسخهٔ بعدی چه می‌شود

`sdk/go` الان تگِ `v0.1.0` دارد. Go overloading ندارد، پس spelling قدیمی
(`srosha.Telegram("...")` با یک آرگومان) نمی‌توانست کنارِ تازه بماند -- طبقِ
دستورِ owner عمداً حذف شد، نه نگه داشته شد. هیچ تگِ git زده نشد؛ این طبقِ
`docs/CONVENTIONS.md` تصمیمِ owner است، نه من. **نسخهٔ بعدی که باید زده شود
`v0.2.0` است، نه `v0.1.1`**، چون این تغییری روی امضای عمومیِ یک ماژولِ
منتشرشده است -- یک مصرف‌کننده که از spelling قدیمی استفاده می‌کرد با ارتقا
دیگر build نمی‌شود، و این دقیقاً همان چیزی است که semver ــِ minor برایش
هست، نه patch.

# کامنتِ بالای constructor ها

کامنتِ قبلی توضیح می‌داد چرا یک تابع به‌ازای هر کانال ارزشش را دارد (بدنه یک
literal است، `srosha.` تایپ کردن کانال‌ها را لیست می‌کند، کانالِ تازه یک خط
هزینه دارد). آن استدلال هنوز درست است ولی دیگر کافی نیست -- باید بگوید چرا
حالا ۱۶ تا هستند نه ۸ تا. بازنویسی شد تا دلیلِ واقعی را بگوید: حالتِ رایج باید
کوتاه نوشته شود، و رشتهٔ خالی راهِ خوبی برای گفتنِ «پیش‌فرض» نیست.

# Files Changed

- `sdk/go/srosha/channel.go` *(۸ constructor به ۱۶ تا شکافته شد؛ کامنتِ بالای
  آن‌ها بازنویسی شد؛ کامنتِ داخلِ `Route.From` هم به‌روزرسانی شد)*
- `sdk/go/srosha/channel_test.go` *(تازه -- سه تست: فرمِ ساده آدرس را خالی
  می‌گذارد، فرمِ `To` آدرسِ داده‌شده را حمل می‌کند، `From` روی هر دو فرم
  chain می‌شود)*
- `sdk/go/srosha/client.go` *(مثالِ کامنتِ package)*
- `sdk/go/srosha/example_test.go` *(`Example`, `ExampleError`)*
- `sdk/go/srosha/srosha_test.go` *(`msg()` helper و یک route ــِ اضافه در
  `TestASubmittedMessageComesBackWithItsReceipt`)*
- `sdk/go/README.md`, `sdk/go/README.fa.md` *(هر جا این ۸ constructor صدا زده
  می‌شد به spelling تازه رفت؛ بخشِ «Addresses, per channel» یک پاراگراف و یک
  مثال گرفت که دو فرم را معرفی می‌کند)*
- `README.md` *(روتِ مخزن -- مثالِ quickstart)*

فایل‌های تاریخی دست نخورده ماندند: `docs/changes/2026-08-27-sdk-client.md`،
`docs/changes/2026-08-27-sdk-setup.md` و
`docs/superpowers/specs/2026-08-27-go-sdk-design.md` هرکدام spelling ای را
ثبت کرده‌اند که آن روز درست بود؛ بازنویسی‌شان تاریخ را جعل می‌کرد، نه ثبت.

# Tests Run

- `go build ./...` -- clean
- `go test -count=1 ./...` -- pass (این ماژولِ ریشه است؛ `sdk/go` ماژولِ جدایی
  است و اینجا build نمی‌شود -- طبقِ طراحیِ `docs/ARCHITECTURE.md`)
- `make sdk` -- pass (`go build`، `go vet`، `go test -race` روی `sdk/go`، به‌علاوهٔ
  `golines` و `golangci-lint`)
- `make prepush` -- pass (شاملِ `make sdk`)

# مثالِ سه-route ای که owner نوشت

```go
Routes: []srosha.Route{
	srosha.EmailTo("someone@acme.test"),      // an email to an address the message names
	srosha.Telegram(),                        // a telegram to the source's default
	srosha.GotifyTo("42").From("ops"),        // a gotify application id, sent from "ops"
}
```

# Follow-ups / Risks

هیچ. تنها نکته این است که `sdk/go` هنوز تگ نخورده -- `v0.2.0` انتخابِ درست است
وقتی owner دستورِ commit و بعد تگ‌زدن را بدهد، نه patch.

# Instruction

هر ۸ constructor کانالِ SDK ــِ Go به دو تا شکافته شود: فرمِ ساده برای آدرسِ
پیش‌فرضِ source، فرمِ `...To(address)` برای آدرسی که پیام نام می‌برد. نامِ
پارامترهای فرمِ صریح (`room`، `applicationID`، `deviceToken`) نگه داشته شود.
کامنتِ بالای constructor ها بازنویسی شود تا استدلالِ شکلِ تازه را بگوید، نه
شکلِ قدیمی را. هر caller در مخزن -- تست‌ها، مثال‌ها، هر دو README، کامنتِ
package -- به spelling تازه برود، هرکدام با فرمی که برای چیزی که نشان می‌دهد
بهتر می‌خواند. تست اضافه شود که فرمِ ساده واقعاً آدرس را خالی می‌گذارد و فرمِ
صریح آدرسِ داده‌شده را حمل می‌کند، و یکی که `From` روی هر دو فرم chain
می‌شود. هیچ تگِ git زده نشود -- فقط در گزارش گفته شود که نسخهٔ بعدی `v0.2.0`
است و چرا.
