Branch: `refactor/domain-layer`

# Summary

entity ی `source` درست شد، واژگان `Target` در کل هسته به `Address` تبدیل شد، و
حجم کامنت‌ها در فایل‌های امروز کم شد.

## یک حفرهٔ امنیتی که خودمان ساخته بودیم

`IsActive` را هیچ‌کس چک نمی‌کرد. قبلاً `notification.New` این کار را می‌کرد، ولی وقتی
`notification` را از `source` جدا کردیم آن چک با خودش رفت و `ErrSourceInactive` هم
حذف شد. یعنی یک source غیرفعال می‌توانست پیام بفرستد.

حالا `EnsureActive()` در `source` است — جایی که قاعده واقعاً به آن تعلق دارد، نه در
package ای که فقط تصادفاً `*source.Source` را در دست داشت.

## یک واژگان به‌جای دو تا

`shared.Recipient` و کل `delivery` می‌گفتند `Address`؛ `source` و `shared.Channel`
هنوز می‌گفتند `Target`. یک مفهوم با دو اسم، بین packageهایی که با هم حرف می‌زنند.

```
ValidateTarget            → ValidateAddress
ErrEmptyTarget            → ErrEmptyAddress
ErrInvalidTarget          → ErrInvalidAddress
DefaultTargets            → DefaultAddresses
AllowCustomTarget         → AllowCustomAddress
ResolveTarget             → Resolve
ErrCustomTargetNotAllowed → ErrCustomAddressNotAllowed
ErrNoTargetForChannel     → ErrNoAddressForChannel
```

متن پیام‌ها هم: «delivery target is empty» شد «delivery address is empty».

## `Resolve` حالا `shared.Recipient` می‌دهد

به‌جای رشته. service دیگر مجبور نیست کانال و آدرس را دستی کنار هم بگذارد، پس
اشتباه جفت‌کردنشان ناممکن می‌شود.

و یک چیز مجانی آورد: چون از `Recipient.Validate()` استفاده می‌کند، حالا **کانال هم
اعتبارسنجی می‌شود**. نسخهٔ قبلی فقط شکل آدرس را می‌دید و یک کانال ناشناخته بی‌صدا رد
می‌شد. تستش اضافه شد.

خروجی slice است نه یک مقدار، چون تصمیم «یک آدرس به ازای هر کانال» ممکن است برای
واتس‌اپ عوض شود — آنجا معادل گروه وجود ندارد. اگر آن روز برسد فقط بدنهٔ تابع عوض
می‌شود، نه هیچ صدازننده‌ای.

## یک آدرس پیش‌فرض به ازای هر کانال، نه چند تا

اول پیشنهاد دادم چند تا باشد، بعد پس گرفتم: تلگرام و بله **گروه** دارند و ایمیل
لیست پستی دارد. سه اپراتور یعنی یک گروه با یک chat_id، که مدیریتش هم دست خودِ
مشتری است نه ما. `source_channels` هم با کلید `(source_id, channel)` ساده می‌ماند.
تنها شکافی که می‌ماند واتس‌اپ است.

## کامنت‌ها

در فایل‌های امروز از ۹۲ خط کامنت به ۵۸ خط، و در `source/entity.go` از ۳۲ به ۱۲.
هیچ دلیلی حذف نشد — چیزی که رفت تکرار و توضیح آشکار بود. `EnsureActive` و
`copyMetadata` و `MarkNotified` هیچ کامنتی لازم ندارند.

# Files Changed

- `internal/core/domain/source/entity.go` *(بازنویسی — `EnsureActive`، `Resolve`، `CreatedAt`/`UpdatedAt`)*
- `internal/core/domain/source/errors.go` *(`ErrSourceInactive` برگشت، دوتای دیگر تغییر نام)*
- `internal/core/domain/source/entity_test.go` *(۱۱ تست)*
- `internal/core/shared/channel.go`، `errors.go`، `channel_test.go` *(تغییر نام)*
- `internal/core/shared/recipient.go` *(تغییر نام + کامنت کمتر)*
- `internal/core/domain/delivery/*`، `internal/core/domain/notification/*` *(فقط کامنت)*

# Tests Run

- `make prepush` — سبز: fmt، vet، arch-check، golangci-lint (`0 issues`)، `go test -race ./...`
- `source` حالا ۱۱ تست دارد، از جمله دو تای تازه: `EnsureActive` و رد شدن کانال ناشناخته

# Follow-ups / Risks

- credential هر source هنوز جایی ندارد. حالت ۱ (بات مال source) قرار شد بعد بیاید،
  و `Source` باید برایش فیلد بگیرد و یک جدول رمزنگاری‌شده لازم دارد. مرحلهٔ بعدی.
- `EnsureActive` نوشته شد ولی هنوز هیچ‌کس صدایش نمی‌زند، چون service وجود ندارد.
  اولین کاری که `submit` باید بکند همین است.
- `shared.Channel.ValidateAddress` برای تلگرام هنوز `@username` را می‌پذیرد، در حالی
  که Bot API نام کاربری یک شخص را به chat_id تبدیل نمی‌کند. آدرسی را قبول می‌کنیم
  که موقع ارسال همیشه شکست می‌خورد.

# Instruction

مالک خواست قبل از هر چیز یک بررسی روی `source` انجام شود، بعد entity اش درست شود.
از شش موردی که گزارش شد، سه تا انتخاب شد: چک `IsActive`، یکسان‌سازی واژگان، و
برگرداندن `Recipient` از `Resolve`. دربارهٔ چند پیش‌فرض به ازای هر کانال بحث شد و
تصمیم بر یکی شد. در پایان خواست کامنت‌ها کم شوند.
