Branch: `fix/a-failed-send-costs-no-quota`

# Summary

کدی که هرگز فرستاده نشد، دیگر سهمیهٔ ورود را خرج نمی‌کند.

# اشکال، همان‌طور که امروز دیده شد

`SignIn.Request` ردیفِ کد را **قبل** از ارسال می‌نوشت، و `CountSince` ردیف‌ها را
می‌شمارد. پس هر ارسالِ ناموفق هم یکی از پنج‌تا را می‌خورد.

امروز صبح روی production دقیقاً همین افتاد: SMTP روی پورتِ اشتباه بود، پنج بار
تلاش شد، هیچ کدی به هیچ صندوقی نرفت، و مالک یک ساعت قفل شد.

و آن سقف برای این وجود دارد که کسی نتواند صندوقِ یک غریبه را پر کند — کاری که
وقتی mailer خراب است **اصلاً انجام نشده**. یعنی سقف چیزی را محافظت نمی‌کرد و
فقط صاحبِ حساب را بیرون نگه می‌داشت.

# اصلاح

ترتیب دست‌نخورده ماند. کد **باید** قبل از ارسال ذخیره شود، وگرنه ممکن است کدی
تحویل شود که قابلِ تأیید نیست. به‌جایش، ارسالِ ناموفق ردیف را پس می‌گیرد:

```go
if err := s.mail.SendCode(ctx, u.Email, code); err != nil {
    _ = s.codes.Forget(ctx, made.ID)
    return err
}
```

`Forget` به port اضافه شد. خطای خودش عمداً نادیده گرفته می‌شود: صداکننده از
قبل دارد خطای ارسال را می‌گیرد و آن خطایی است که ارزش دارد؛ دومی جایش را
می‌گرفت. بدترین حالتش یکی از پنج‌تاست.

**چرا حذف و نه یک ستونِ `sent_at`:** آن هم درست است و معنی‌اش تمیزتر، ولی یک
migration می‌خواهد — و migration ها حالا deploy را گِیت می‌کنند. برای چیزی که
یک متد حلش می‌کند ارزشش را ندارد.

# تست دقیقاً همان را می‌گیرد

قبل از اصلاح:

```
--- FAIL: TestAFailedSendCostsNoQuota
    request 6 was refused for asking too often, though nothing had been sent
```

و نیمهٔ دومش هم قفل شد — `TestASentCodeStillCostsQuota`: ارسالِ **موفق** همچنان
می‌شمارد و پنج بار همچنان یعنی یک ساعت صبر.

# و مقابلِ دیتابیسِ واقعی، نه فقط fake

دو تستِ یکپارچه اضافه شد: `Forget` واقعاً ردیف را از شمارش برمی‌دارد، و ردیفِ
ناموجود خطا نیست.

این لازم بود. امروز صبح دیدیم یک fake می‌تواند سبز بماند در حالی که دیتابیس
کارِ دیگری می‌کند. سه fake یاد گرفتند `Forget` کنند و در کامنتِ هرکدام نوشته
شد چرا نباید ردیف را نگه دارند — وگرنه تست سبز می‌ماند و اشکال برمی‌گردد.

# Files Changed

- `internal/core/domain/logincode/port.go` *(`Forget`)*
- `internal/core/usecase/signin.go` *(پس گرفتنِ ردیف)*
- `internal/core/usecase/signin_test.go` *(دو تست)*
- `internal/adapter/db/postgres/queries/logincode.sql`, `logincode.go` *(query و متد)*
- `internal/adapter/db/postgres/logincode_test.go` *(دو تستِ یکپارچه)*
- `internal/core/usecase/fakes_test.go`, `internal/adapter/api/web/portal_test.go` *(fake ها)*
- `internal/adapter/db/postgres/gen/` *(sqlc)*

# Tests Run

- `go test -count=1 ./...` — بدون شکست
- `go test -tags integration ./internal/adapter/db/postgres/` — pass، مقابلِ postgres واقعی
- `make prepush` — pass
- قبل از اصلاح، تستِ تازه قرمز بود با پیامِ خودش

# Follow-ups / Risks

- اگر خودِ `Forget` شکست بخورد، یکی از پنج‌تا سوخته می‌ماند. تنزلِ پذیرفته‌شده
  است و در کامنت نوشته شده.
- شمارش همچنان بهترین‌تلاش است: دو درخواستِ هم‌زمان می‌توانند هر دو از سقف رد
  شوند. از قبل همین‌طور بود و این تغییر بدترش نمی‌کند.

# Instruction

مالک موردِ دوم از فهرستِ باز را انتخاب کرد: سهمیهٔ ورود که خرجِ تلاش می‌شد
به‌جای خرجِ ارسال. طراحی در چت تأیید شد: حذفِ ردیف بعد از ارسالِ ناموفق، بدونِ
migration.
