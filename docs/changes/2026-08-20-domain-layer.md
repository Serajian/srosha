Branch: `refactor/domain-layer`

# Summary

لایهٔ domain بازطراحی شد. تغییر اصلی مدل است نه کد: قبلاً یک `Notification` بود که
`deliveries []Delivery` را در خودش داشت و یک وضعیت کل که از روی آن‌ها ساخته می‌شد.
حالا دو aggregate جداست.

**`notification` فقط پیام است.** بدون status، بدون گیرنده. هرچه دارد موقع ساخت قطعی
می‌شود و تنها فیلد مخفی‌اش `metadata` است، که موقع ورود و خروج کپی می‌شود تا کسی که
map را ساخته نتواند بعد از اعتبارسنجی عوضش کند. `Request` هم `Recipients` را از دست
داد، چون دیگر کار این package نیست.

**`delivery` یک پیام به یک گیرنده است.** شناسهٔ خودش را دارد، ماشین حالت خودش را، و
ردیف خودش در دیتابیس. جدا شد چون چرخهٔ عمر مستقل دارد: dispatcher تمام دنیایش یک
delivery است — با id می‌خواندش، وضعیتش را عوض می‌کند، همان یک ردیف را می‌نویسد، و
هیچ‌وقت پیام را لمس نمی‌کند. این همان چیزی است که `docs/CONVENTIONS.md` می‌گوید:
دو چیزی که جدا ذخیره می‌شوند، دو domain اند.

سه فیلد `Delivery` — `ID`، `NotificationID`، `Recipient` — موقع ساخت قطعی می‌شوند.
هفت فیلد دیگر مخفی‌اند و فقط از طریق `MarkSent` و `MarkFailed` عوض می‌شوند، چون با
هم حرکت می‌کنند: نوشتن یکی به‌تنهایی ردیفی می‌سازد که با خودش نمی‌خواند، مثلاً `SENT`
که هنوز `failure_reason` یک تلاش قبلی را دارد.

**`Recipient` رفت به `shared`** — یک value object با کانال و آدرس. الان `delivery`
نگهش می‌دارد و `source` تولیدش می‌کند، پس ترفیعش طبق قانون است نه از روی پیش‌بینی.
comparable است، پس تشخیص تکراری یک lookup در map است. تکراری روی **کل** گیرنده چک
می‌شود نه روی کانال: یک کانال با دو آدرس یعنی «یک پیام به چند نفر» و درست است.

## تصمیم‌هایی که به این شکل رسید

**بدون `PROCESSING`.** یک نوشتن اضافه به ازای هر ارسال بود، برای پنجره‌ای که دویست
میلی‌ثانیه است، و تکراری‌شدن را هم حل نمی‌کرد: worker ای که بعد از ارسال موفق و قبل
از نوشتن نتیجه بمیرد، در هر دو طرح منجر به ارسال دوباره می‌شود. سقف retry به‌جای
`attempts` در دیتابیس، از `MaxDeliver` خود JetStream می‌آید.

**شکست گذرا هیچ چیزی نمی‌نویسد.** delivery در `PENDING` می‌ماند و NAK می‌رود. نتیجه‌اش
این است که `FAILED` واقعاً نهایی است — retry هیچ‌وقت از آن بیرون نمی‌آید — و همین
است که جدول transition را به سه حالت و دو یال کاهش داد.

**بدون `DELIVERED`.** وظیفهٔ srosha ارسال است نه پیگیری رسیدن. Bot API تلگرام هم اصلاً
رسید تحویل نمی‌دهد، پس حالتی که برای بعضی کانال‌ها هرگز پر نشود سوءتفاهم می‌سازد نه
اطلاعات. در عوض `ProviderMessageID` ذخیره می‌شود تا source بتواند خودش پیگیری کند.

**`EXPIRED` یک دلیل است نه یک حالت.** هیچ‌جای برنامه با آن کاری نمی‌کند که با `FAILED`
نکند، پس یک ستون `FailureReason` گرفت در کنار `MAX_ATTEMPTS` و `PERMANENT` و
`NO_SENDER`. حالت‌ها کم می‌مانند، دلیل‌ها غنی.

**وضعیت کل پیام ذخیره نمی‌شود.** حذف شد، چون هر خلاصه‌ای یک query است و ستونی که
ذخیره شود می‌تواند از ردیف‌های delivery عقب بیفتد. با این کار `Restore` هم دیگر
status نمی‌گیرد، یعنی repository اصلاً راهی ندارد که وضعیت ناسازگار بنویسد.

**`Restore` در هر دو package اعتبارسنجی نمی‌کند.** ردیفی که موقع نوشتن معتبر بوده باید
بعد از سخت‌تر شدن یک قاعده هم قابل بارگذاری بماند، وگرنه یک تغییر قاعده تبدیل می‌شود
به outage روی دادهٔ تاریخی. برای `Delivery` یک `Snapshot` گذاشته شد تا `Restore` ده
پارامتر نگیرد.

# Files Changed

- `internal/core/shared/recipient.go` *(جدید — `Recipient` به‌عنوان value object)*
- `internal/core/domain/notification/entity.go` *(بازنویسی — فقط پیام)*
- `internal/core/domain/notification/types.go` *(جدید — `Origin` و `Request`)*
- `internal/core/domain/notification/errors.go` *(چهار sentinel، بقیه منتقل یا حذف)*
- `internal/core/domain/notification/entity_test.go` *(جدید — ۲۵ تست)*
- `internal/core/domain/notification/status.go` *(حذف — به `delivery` رفت)*
- `internal/core/domain/notification/status_test.go` *(حذف — به `delivery` رفت)*
- `internal/core/domain/delivery/entity.go` *(جدید — `Delivery`، `NewSet`، `Restore`، انتقال‌ها)*
- `internal/core/domain/delivery/types.go` *(جدید — `IDFunc` و `Snapshot`)*
- `internal/core/domain/delivery/status.go` *(جدید — `Status` و `FailureReason`)*
- `internal/core/domain/delivery/errors.go` *(جدید)*
- `internal/core/domain/delivery/entity_test.go` *(جدید — ۵۱ تست)*
- `internal/core/domain/delivery/status_test.go` *(جدید)*
- `internal/core/domain/source/*` *(تغییرات مالک پروژه)*
- `docs/CONVENTIONS.md` *(قانون تازه: authorship کامیت)*

# Tests Run

- `go build ./...` — clean
- `go vet ./internal/... ./pkg/...` — clean
- `go test -race ./...` — pass (delivery, notification, source, shared, errs)
- `make arch-check` — سبز؛ لایهٔ domain فقط stdlib و `shared` و `pkg/errs` را import می‌کند

# Follow-ups / Risks

- service حالا برای یک درخواست دو aggregate می‌سازد و باید هر دو را در **یک
  transaction** بنویسد، وگرنه پیامی می‌ماند که هیچ گیرنده‌ای ندارد. پورت
  unit-of-work هنوز نوشته نشده.
- سه تصمیم گرفته شد که هنوز کدی ندارند و مال messaging و migration اند:
  `Nats-Msg-Id` قطعی به‌همراه `duplicate_window` روی استریم، ایندکس
  `(status, updated_at)` روی `deliveries`، و `UNIQUE (notification_id, channel, address)`.
  اولی باید **قبل** از نوشتن کد publish نهایی شود؛ عوض‌کردن طرح شناسهٔ پیام بعداً
  dedup را در دورهٔ گذار می‌شکند.
- سقف سهمیهٔ priority (مثلاً «۱۰ تا critical در روز، بعدش تنزل به high») بحث شد و
  عمداً کنار گذاشته شد. وقتی بیاید، `Origin.MaxPriority` جایش را به یک
  `EffectivePriority` از پیش حساب‌شده می‌دهد، چون آن تصمیم I/O لازم دارد.
- `domain/webhook` هنوز خالی است.
- `shared.Channel.ValidateTarget` هنوز `Target` نام دارد در حالی که همه‌جای دیگر
  `Address` شده. تغییر نامش تست‌های `shared` را هم لمس می‌کند، پس جدا انجام می‌شود.
- `ValidateTarget` برای تلگرام هنوز `@username` را می‌پذیرد، ولی Bot API نام کاربری
  یک شخص را به chat_id تبدیل نمی‌کند — فقط برای کانال عمومی کار می‌کند. یعنی الان
  آدرسی را قبول می‌کنیم که موقع ارسال همیشه شکست می‌خورد.

# Instruction

مالک خواست لایهٔ domain با هم مرور و درست شود. مدل مرحله‌به‌مرحله در گفت‌وگو شکل
گرفت — هر تصمیم (تعداد حالت‌ها، جای وضعیت، جدا شدن `delivery`، مخفی‌کردن فیلدها)
پیشنهاد شد، بررسی شد و تأیید گرفت. توافق نهایی: `Delivery` یک aggregate جدا با
فیلدهای متغیر مخفی، و تست برای هر دو entity.
