Branch: `feat/credential-trial`

# Summary

`usecase.Trials` نوشته شد: هفت تست و یک متد. task دو از چهار.

```go
func (t *Trials) Run(
	ctx context.Context, actor *user.User, sourceID string, credentialID shared.ID,
) (string, error)
```

رشته‌ای که برمی‌گرداند شناسهٔ خودِ provider است — همان چیزی که صفحه نشان می‌دهد.

## چرا type ــِ خودش، نه متدی روی `Credentials`

`usecase.Credentials` را **دو** باینری می‌سازند: console، و gateway که همان عملیات را
روی gRPC به SDK می‌دهد. اگر `SenderRegistry` به constructor ــِ آن اضافه می‌شد، gateway هم
باید یکی می‌داد — و gateway اصلاً هیچ sender adapter ای ندارد. این همان تقسیمی است که قبلاً
بین `Operators` و `Sources` انجام شد.

## ترتیبِ ردها عمدی است

```
source فعال است؟            نه → این source هنوز تأیید نشده
credential مالِ همین source؟ نه → not found (نه forbidden)
آدرسِ پیش‌فرضِ آن کانال؟      نه → این کانال آدرسی ندارد، تست جایی ندارد برود
زیرِ سقف؟                    نه → الان تست‌های زیادی زده‌ای
        ↓
registry.For(source, channel, name)  →  Send
```

ارزان‌ترین ردها اول، و **هیچ‌چیزی خرج نمی‌شود قبل از اینکه معلوم شود source اجازه دارد**.
سقف آخر از همه گرفته می‌شود، وقتی واقعاً چیزی برای فرستادن هست. دو تست این را نگه
می‌دارند: هیچ‌کدام از ردها نباید حتی sender بسازد.

## با **نام** resolve می‌شود، نه با id

```go
out, err := t.senders.For(ctx, sourceID, cred.Channel, cred.Name)
```

دقیقاً همان چیزی که dispatcher صدا می‌زند. تستی که جورِ دیگری resolve کند چیزِ دیگری را
ثابت می‌کند — و مهم‌تر: هویتی که source خاموشش کرده باید اینجا هم رد شود، به همان دلیلی که
در production رد می‌شود. خاموش کردنش یک تصمیم بوده و جای آن ایستادن، آن تصمیم را بی‌صدا
باطل می‌کند.

## چند تصمیمِ کوچک که در plan باز مانده بود

**actor آرگومان است، نه مقداری در context.** plan گفته بود این را موقع نوشتن حل کن و از
همان الگوی بقیهٔ use case ها پیروی کن. شد `Run(ctx, actor, sourceID, credentialID)`.

**`t.sources.Load` و نه `Manage`.** `Load` خودش `EnsureActive` را می‌زند، پس شرطِ اولِ
spec بدون یک خطِ اضافه برآورده می‌شود. `Manage` عمداً source ــِ غیرفعال را هم قبول
می‌کند — که برای پیکربندی درست است و برای فرستادن نه.

**`t.creds.Get` با sourceID.** خودش scope شده، پس id ــِ کسِ دیگر «پیدا نشد» می‌گیرد نه
«اجازه نداری» — بدون اینکه کدی برای این نوشته شود.

**`TrialLimiter` یک port ــِ جدا است، نه `source.RateLimiter`.** شکلشان یکی است ولی
معنایشان نه: آن یکی سهمیهٔ **فرستادن** است و در process ــِ دیگری خرج می‌شود. یکی کردنشان
یعنی یک تست، یک پیام از سهمیهٔ مشتری بخورد — که نمی‌خورد و نباید به‌نظر برسد که می‌خورد.

**سقف در configuration است، نه در ثابت.** `NOTIF_CONSOLE_TRIAL_PER_MINUTE`، پیش‌فرض `3`،
و صفر رد می‌شود چون «سقفِ صفر یک محدودیت نیست، یعنی دکمه برای همیشه خراب است».

# Files Changed

- `internal/core/usecase/trial.go` *(تازه — `Trials`، `TrialLimiter`، `Run`)*
- `internal/core/usecase/trial_test.go` *(تازه — هفت تست، هر رد با sentinel خودش)*
- `internal/core/usecase/const.go` *(`ActCredentialTest`، و متنِ خودِ پیامِ تست)*
- `internal/core/usecase/errors.go` *(`ErrTrialNoAddress`، `ErrTrialTooMany`)*
- `internal/config/settings/console.go` *(`TrialPerMinute` و `r.Check` ــِ آن)*

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass (هر هفت تستِ تازه سبز)
- `make precommit` — pass

# Follow-ups / Risks

- **`ActCredentialTest` به `sourceDecisionVerbs` اضافه **نشد**، عمداً.** actor ــِ آن
  مشتری است و آن لیست فیلترِ صفحه‌ای است که یک `admin` ساده بازش می‌کند — همان استدلالی
  که `/audit` را به `super_admin` برد.
- **`Gate.Do` بعد از موفقیت به operator خبر می‌دهد.** یعنی هر تستِ موفقِ هر مشتری یک
  اعلانِ Gotify می‌شود. spec گفته بود از gate رد شود و همان انجام شد، ولی اگر شلوغ شد،
  جایش `Gate` است که باید یاد بگیرد کدام verb خبر دارد — نه اینکه trial از gate بیرون
  بیاید و ردیفِ audit اش را از دست بدهد.
- `Trials` ساخته شده و هنوز هیچ صفحه‌ای صدایش نمی‌زند. task سه دکمه است.

# Instruction

قدمِ دومِ credential trial: use case ای که هویت را مثل یک ارسالِ واقعی resolve می‌کند،
یک پیامِ کوتاه به آدرسِ پیش‌فرضِ همان کانال می‌فرستد، و حرفِ خودِ provider را برمی‌گرداند.
