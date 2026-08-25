Branch: `feat/credential-crypto`

# Summary

**رازهای فرستنده حالا رمز می‌شوند، و از راه API ثبت می‌شوند.**

ستون `secret` از روز اول در migration بود و کامنتش قالبش را هم نوشته بود، ولی
هیچ کدی آن را پر نمی‌کرد و هیچ متدی برای ساختن credential وجود نداشت. هر دو حالا
هست.

## قالبی که خودش را توصیف می‌کند

```
v1 . 1 . <nonce> . <ciphertext>
│    │
│    └─ کدام کلید این را بسته
└─ کدام الگوریتم
```

سخت‌ترین قسمت رمزنگاری نیست، **روزی است که کلید عوض می‌شود** — و آن یک «کِی» است
نه یک «اگر». اگر ستون فقط ciphertext داشته باشد، هیچ‌چیزِ ردیف نمی‌گوید کدام کلید
تولیدش کرده، پس تغییر کلید یعنی توقف سرویس، باز کردن همه‌چیز با کلید قدیم و بستن
دوباره با جدید — یک outage که می‌تواند وسط راه شکست بخورد و دو نوع ردیف بگذارد
که از هم قابل تشخیص نیستند.

با این قالب، چرخش کلید این است:

```
کلید دوم را اضافه کن  →  KEY_ID را روی آن ببر  →  ردیف‌ها هنگام خوانده‌شدن خودشان را دوباره می‌بندند
```

هیچ قدمی سرویس را متوقف نمی‌کند.

## و آن شرطی که همه‌چیز به آن بند است

`NeedsReseal` مهم‌ترین سه خط این commit است:

```go
func (k *Keyring) NeedsReseal(value string) bool {
	_, id, _, _, err := split(value)
	if err != nil { return false }
	return id != k.active
}
```

بدونش، **هر خواندن یک نوشتن می‌شود**. چون nonce تصادفی است، بستنِ همان مقدار با
همان کلید هرگز رشتهٔ یکسانی نمی‌دهد و هیچ‌چیز جلوی نوشتن را نمی‌گیرد — یعنی صفِ پر
از پیام، به‌ازای هر پیام یک `UPDATE` بی‌فایده روی یک ردیف داغ. و همین‌جاست که ارزش
داشتنِ key id **داخل خود مقدار** معلوم می‌شود: این پرسش نه ستون اضافه می‌خواهد نه
query دوم.

`TestSealingTwiceNeverRepeatsItself` دقیقاً همین را ثابت می‌کند، و برای همین
نوشته شده.

## AAD: حمله‌ای که هیچ کلیدی در آن شکسته نمی‌شود

```
کسی که به دیتابیس می‌رسد، ciphertext ــِ source A را در ردیف source B کپی می‌کند
   →  B با هویت A پیام می‌فرستد
```

هیچ رمزی شکسته نشده، فقط یک مقدار جابه‌جا شده. پس هویت خودِ credential در امضا
بسته می‌شود:

```
sourceID | channel | credentialID
```

**نه نام.** نام می‌تواند عوض شود، و پیوندی که جابه‌جا می‌شود پیوندی است که باید
بازنویسی شود. هر سه فیلد دیگر ثابت‌اند، و هیچ‌کدام نمی‌توانند `|` داشته باشند —
دوتا ULID اند و سومی از یک مجموعهٔ بسته.

`TestATokenReadUnderAnotherIdentityDoesNotOpen` این را می‌سنجد.

## `adapter/secret`: تنها جایی که هر دو طرف را می‌بیند

```
credential.Credential   هویت، بدون هیچ رازی      ← core
credentials.secret      راز، بدون هیچ معنایی      ← postgres
                   ↘  secret.Keeper  ↙
```

core جایی برای گذاشتن راز ندارد (`credential.Credential` عمداً هیچ ندارد)، و
postgres نمی‌داند چیزی رمز شده. `secret.Keeper` تنها نقطه‌ای است که هر دو هم‌زمان درست‌اند.

و `Store` را **خودش اعلام می‌کند**، نه اینکه از postgres import کند — همان قاعده‌ای
که `KeyScheme` و `nats.Dispatcher` دارند. تنها پیاده‌سازی‌اش همان
`postgres.CredentialRepository` است که bootstrap تویش می‌گذارد، پس `INSERT` دقیقاً
جایی است که باید باشد.

چرا یک package جدا و نه داخل postgres یا داخل usecase: قالبِ سربسته یک **قرارداد**
است و بستن و بازکردنش باید یک‌جا بمانند. بازکردن اجباراً adapter-side است — مسیر
ارسال از `SenderRegistry` می‌گذرد و کامنت خودش می‌گوید *«چطور رمزگشایی می‌شود کار
adapter است»*. پس بستن هم adapter-side می‌ماند، وگرنه دو نیمهٔ یک قالب در دو لایه
می‌نشینند و روزی یکی بدون دیگری عوض می‌شود. داخل postgres هم نرفت چون آن‌وقت یک
متد `Read` وسط کارش `UPDATE` می‌زد.

اسمش `vault` بود و عوض شد: بوی یک سرویس بیرونی می‌داد و چنین چیزی در کار نیست.

## reseal بهترین تلاش است، نه یک شرط

راز از قبل باز شده و پیام آمادهٔ رفتن است. شکست‌دادن ارسال به‌خاطر شکستِ یک
بازنویسی، یک قدم مرتب‌کاری را به یک حادثه تبدیل می‌کند. لاگ می‌شود و خواندن بعدی
دوباره تلاش می‌کند.

و مقدار قدیم در `WHERE` است، پس دو sender که هم‌زمان یک credential را می‌خوانند و
هر دو reseal می‌کنند، بازنده چیزی نمی‌نویسد به‌جای اینکه روی برنده بنویسد. هر دو
همان plaintext را بسته بودند؛ فقط یکی می‌تواند آن ردیف باشد.

## gateway کلید را دارد، و این یک انتخاب است

رمزنگاری متقارن است: هرکس بتواند ببندد می‌تواند باز کند. پس `RegisterCredential`
به‌عنوان یک rpc روی gateway یعنی gateway می‌تواند توکن هر source ای را بخواند.

پذیرفته شده، نه نادیده گرفته شده. مدل تهدیدی که `ARCHITECTURE.md` نوشته **دامپ
دیتابیس** است، و gateway همین حالا با همان connection string همان ردیف‌ها را
می‌خواند. این کلید به دسترسی‌اش چیزی اضافه نمی‌کند که نداشته باشد.

راه دیگری هم بود — نامتقارن، gateway با کلید عمومی می‌بندد و فقط dispatcher باز
می‌کند — که تنها راهِ برقرارکردن **واقعی** آن خاصیت است. رد نشد، فقط انتخاب نشد،
و اگر روزی لازم شود اول در `ARCHITECTURE.md` نوشته می‌شود.

## proto: پیامی که فیلد راز ندارد

```proto
message Credential {
  string id = 1;  Channel channel = 2;  string name = 3;
  bool is_default = 4;  bool is_active = 5;
  google.protobuf.Timestamp created_at = 6;
}
```

هیچ فیلدی برای راز ندارد و نخواهد داشت. این خودِ نکته است: تصادفی نمی‌شود اضافه‌اش
کرد.

`config` به‌صورت رشتهٔ json می‌آید و رشته می‌ماند. شکل‌دادن به آن اینجا یعنی
پشتیبانی از provider دوم تبدیل شود به تغییری در همین contract، و هر client مجبور
شود برای provider ای که استفاده نمی‌کند دوباره generate کند.

# Files Changed

- `pkg/crypto/{keyring,const,errors}.go` *(تازه — `Seal`، `Open`، `NeedsReseal`)*
- `pkg/crypto/keyring_test.go` *(تازه — ۱۳ تست)*
- `internal/adapter/secret/{keeper,const}.go` *(تازه — `Add`، `Material`، reseal)*
- `internal/adapter/secret/keeper_test.go` *(تازه)*
- `internal/config/settings/crypto.go` *(تازه — `KEYS`، `KEY_ID`، و `String()` که نشت نمی‌کند)*
- `internal/config/{gateway,dispatcher}.go` *(هر دو `Crypto` را می‌گیرند)*
- `internal/config/config_test.go` *(keyring در boot بررسی می‌شود)*
- `internal/core/usecase/credential.go` *(تازه — `Credentials.Register`، پورت‌های `CredentialSecrets` و `CredentialDefaults`)*
- `internal/core/usecase/credential_test.go` *(تازه)*
- `api/proto/notification/v1/credential.proto` *(تازه)* + `gen/`
- `internal/adapter/api/grpcsrv/credential.go` *(تازه)*، `mapper.go`، `register.go`، `errors_test.go`
- `internal/adapter/db/postgres/queries/credential.sql` *(`ResealCredentialSecret`)* + `gen/`
- `internal/adapter/db/postgres/credential.go` *(`Reseal`)*
- `internal/adapter/db/postgres/credential_test.go` *(دو تست integration)*
- `internal/bootstrap/gateway.go` *(keyring، `secret.Keeper`، `Credentials`، `CredentialServer`)*
- `.env.example`، `docs/CONFIG.md` *(ردیف `crypto`، و بخش «هنوز خوانده نشده» حذف شد — هر دو کلیدش حالا خوانده می‌شوند)*
- `docs/ARCHITECTURE.md` *(چه کسی کلید را دارد، و reseal هنگام خواندن)*

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، روی gateway و postgres واقعی:

```
بدون کلید                       →  Unauthenticated: invalid credentials
با کلید                         →  credential برگشت، بدون هیچ فیلد راز
                                     ↓
postgres  secret = v1.1.28Af_SYv269UQuTE.LlwqXh2aQz8S0dPMjtBDa8bdr…
          plaintext در کل جدول: ۰ بار
                                     ↓
نام تکراری                      →  AlreadyExists
نام "Alerts"                    →  InvalidArgument: wrong format
config = "chat_id=42"           →  InvalidArgument: not valid json
بدون channel                    →  InvalidArgument: channel is required
default دوم                     →  گرفت، و اولی is_default=f شد   (یک transaction)
بدون هیچ رازی                   →  ردیف با secret = NULL، نه رمزِ رشتهٔ خالی
                                     ↓
SIGTERM   http(3) → grpc(3) → nats(1) → postgres(0)
```

دادهٔ تستی بعدش پاک شد.

# Follow-ups / Risks

- **هیچ راهی برای دیدن یا حذف credential نیست.** فقط `Register` هست. یک source
  که نامی را فراموش کند هیچ راهی برای پرسیدن ندارد. `List` و `Deactivate` قدم
  بعدی همین سرویس‌اند.
- **`Material` هنوز هیچ صداکننده‌ای ندارد.** senderها stub اند، پس مسیر
  رمزگشایی و reseal فقط در تست اجرا می‌شود. اولین sender آن را زنده می‌کند.
- **dispatcher `NOTIF_CRYPTO_KEYS` را می‌خواهد و هنوز مصرفش نمی‌کند.** عمدی —
  senderها قدم بعدی‌اند و این یک churn کانفیگ کمتر است. ولی امروز یعنی dispatcher
  بدون کلیدی که استفاده نمی‌کند بالا نمی‌آید.
- چرخشِ کلید هرگز روی سرویس زنده آزموده نشده، چون هیچ‌چیز هنوز نمی‌خواند. تست‌های
  `secret` کاملش را پوشش می‌دهند، ولی آن یک تست است نه یک تمرین.

# Instruction

«crypto + RegisterCredential + gRPC» — با پنج تصمیمی که تأیید شد: `pkg/crypto`،
`AAD = sourceID|channel|credentialID`، keyring به‌شکل JSON با یک key id فعال،
هم `Seal` و هم `Open`، و ثبت credential از راه API نه دستی در دیتابیس. به‌علاوهٔ
شرطی که خودت روی آن دست گذاشتی: در هر ارسال، اگر key id همان بود، ننویس.
