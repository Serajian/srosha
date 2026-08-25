Branch: `feat/credential-api`

# Summary

**`CredentialService` کامل شد.** تا دیروز فقط `Register` داشت — source ای که نام
یک هویت را فراموش می‌کرد هیچ راهی برای پرسیدن نداشت، و توکنی که لو می‌رفت راهی
برای عوض‌شدن.

```
Register   (بود)
List        چه چیزی ثبت کرده‌ام
Deactivate  خاموش، بدون فراموش‌شدن
Activate    برگشت
SetDefault  جابه‌جایی پیش‌فرض
Rotate      راز تازه، نام همان
```

## چرا `Rotate` مهم‌ترینِ این پنج است

بدون آن، source ای که توکنش لو رفته چه می‌کرد؟ نمی‌توانست «alerts» را دوباره ثبت
کند — نام گرفته بود. مجبور بود «alerts-2» بسازد، و آن‌وقت:

```
هر پیامی که  sender: "alerts"  می‌گفت  →  NO_SENDER
```

یعنی **یک نشت توکن به تغییر کد در سمت مشتری ختم می‌شد.** حالا نام می‌ماند و
فقط راز عوض می‌شود.

و عمداً با `Reseal` یکی نشد. تفاوتشان در `WHERE` است:

```
Reseal   ... AND secret = @previous     همان راز، کلید تازه — بازندهٔ مسابقه چیزی ننویسد
Rotate   ... AND is_active              رازِ متفاوت — اگر روی مقدار قدیم شرط بگذارد،
                                        یک reseal که وسط بیفتد چرخش را گم می‌کند
```

## هر lookup و هر write با `source_id` محدود شده

`id` از **بدنهٔ درخواست** می‌آید. بدون این، یک source می‌توانست با حدس‌زدن یک ULID
هویت دیگری را غیرفعال یا rotate کند:

```sql
WHERE id = @id AND source_id = @source_id
```

پس بدترین کاری که یک id حدس‌زده می‌تواند بکند، **پیدا نکردن** است.

روی سرویس زنده آزموده شد: id ــِ یک source دیگر → `NotFound`، نه `PermissionDenied`
— چون گفتن «هست ولی مال تو نیست» خودش یک نشت است.

## سه قاعده که در خودِ statement نشسته‌اند، نه در کد

**خاموش‌کردن، پیش‌فرض را هم پس می‌گیرد:**

```sql
is_default = CASE WHEN @is_active THEN is_default ELSE FALSE END
```

پیش‌فرضی که قابل استفاده نیست، هر پیامی را که نامی نمی‌برد با «چیزی تنظیم نشده»
شکست می‌دهد. پس کانال بی‌پیش‌فرض می‌ماند تا source یکی را نام ببرد — حدس‌زدن
اینکه کدام باید جانشین شود، آن را بی‌صدا جابه‌جا می‌کند.

**روشن‌کردن، پیش‌فرض را پس نمی‌دهد.** همان دلیل، از جهت مخالف.

**`SetDefault` هویت خاموش را رد می‌کند**، و این چک در `WHERE` است نه در Go:
خواندن و نوشتن دو زمان متفاوت‌اند.

## حذف نیست

همان قاعده‌ای که `api_keys` دارد و `ARCHITECTURE.md` نوشته: *«علامت‌گذاری می‌شود،
هرگز حذف نمی‌شود. بعد از یک حادثه اولین سؤال این است که کِی باطل شد.»*

و به همین دلیل `List` خاموش‌ها را هم برمی‌گرداند — جواب «چه چیزی دارم» باید شامل
آنی باشد که کسی غیرفعالش کرده، وگرنه هیچ‌کس نمی‌تواند برش گرداند.

## و باز هم یک fake که دروغ می‌گفت

`TestSetDefaultMovesIt` رد شد. `fakeSecrets.ClearDefault` فقط صدا زدن را می‌شمرد
و ردیف‌ها را دست نمی‌زد — در حالی که در تولید همان `postgres.CredentialRepository`
است. یعنی یک `SetDefault` شکسته از تست رد می‌شد.

این سومین بار در این پروژه است که یک fake شاخه‌ای را زنده نگه داشته که در تولید
وجود ندارد. حالا write-through می‌کند، مثل `Add`.

# Files Changed

- `internal/adapter/db/postgres/queries/credential.sql` *(چهار statement تازه)* + `gen/`
- `internal/adapter/db/postgres/credential.go` *(`ListBySourceID`، `ReadByID`، `Deactivate`، `Activate`، `SetDefault`، `Rotate`)*
- `internal/adapter/db/postgres/credential_test.go` *(۵ تست integration)*
- `internal/core/domain/credential/port.go` *(پورت کامل شد)*
- `internal/core/domain/credential/service.go` *(`List`، `Get`، `Deactivate`، `Activate`، `MakeDefault`، `ClearDefault` — و حالا یک ساعت دارد)*
- `internal/adapter/secret/keeper.go` *(`Replace`)*
- `internal/core/usecase/credential.go` *(پنج متد تازه)*
- `internal/core/usecase/{credential,fakes,submit}_test.go`
- `api/proto/notification/v1/credential.proto` *(۵ rpc)* + `gen/`
- `internal/adapter/api/grpcsrv/{credential,mapper}.go`
- `internal/bootstrap/{gateway,dispatcher}.go` *(ساعت به `credential.NewService`)*

هیچ کلید تازه‌ای در کانفیگ نیست.

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، چرخهٔ عمر کامل روی gateway واقعی:

```
Register alerts (default) + marketing
SetDefault marketing        →  alerts=false  marketing=true
Deactivate marketing        →  marketing=false/inactive، کانال بی‌پیش‌فرض
Activate marketing          →  فعال، ولی پیش‌فرض برنگشت
SetDefault روی خاموش        →  InvalidArgument: an inactive credential cannot be the default
Rotate alerts               →  همان id، همان نام
List channel=EMAIL          →  خالی
id ــِ یک source دیگر        →  NotFound
                            ↓
postgres  alerts    v1.1.… (۷۲ کاراکتر — راز تازه)
          marketing v1.1.… (۶۴)
          plaintext در کل جدول: ۰ بار
```

# Follow-ups / Risks

- **`Update` برای `config` نیست.** `Rotate` فقط راز را عوض می‌کند. تا email نوشته
  نشود `config` عملاً فقط `parse_mode` است، ولی با SMTP عوض‌کردن host لازم می‌شود.
- **`List` صفحه‌بندی ندارد.** یک source چند هویت دارد نه چند هزار تا، ولی این یک
  فرض است نه یک محدودیت.
- فیلتر کانال در `List` در Go انجام می‌شود نه در SQL — یک حلقه روی چند ردیف، در
  برابر یک statement که فقط برای صرفه‌جویی در همان حلقه وجود داشته باشد.

# Instruction

«اول ۴ را تمام کنیم» — یعنی API نیمه‌کارهٔ credential. با دو تصمیم: `SetDefault`
هم باشد، و `Rotate` فقط راز را عوض کند نه تنظیمات را.
