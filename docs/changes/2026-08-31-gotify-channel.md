Branch: `feat/gotify-channel`

# Summary

کانالِ هشتم اضافه شد: Gotify، یک push server خودمیزبان (self-hosted). عدد
enum پروتوی تازه `8` گرفت. Matrix دست‌نخورده ماند -- کانالِ هفتم همچنان همان
است که بود.

# کانال تازه: Gotify

پیام با `POST {server}/message?token=…` روی سرور پست می‌شود. credential دو
تکه دارد -- `server_url` (نه راز، در `config` مثلِ `SMTP.Host` و
`APNs.TeamID`) و `application_token` (راز، `pkg/crypto` مثلِ بقیهٔ کانال‌ها
seal اش می‌کند) -- و `address` مالِ `application_id` است.

## تنشی که owner صریح خواسته بود حل شود، نه پنهان

در API مستندِ Gotify، فقط توکن تعیین می‌کند پیام به کدام application می‌رود؛
هیچ فیلدی برای application id در request نیست. پس با آن خوانش، `address` این
credential با خودِ توکن هم‌پوشانی دارد (redundant). این سرویس دسترسیِ شبکه
ندارد تا این را روی یک سرور واقعی چک کند، پس این یک **فرض** است، نه واقعیت
تأییدشده.

طبق مشخصاتِ owner ساخته شد -- `address` را application id حمل می‌کند -- و
`address` هیچ‌وقت بی‌صدا دور انداخته نمی‌شود: به‌عنوانِ یک query parameter
دومِ خودمان، `appid`، کنارِ `token` مستندِ Gotify فرستاده می‌شود. یک سرورِ
استانداردِ Gotify پارامترهای ناشناخته را نادیده می‌گیرد، پس این بی‌ضرر است؛
دقیقاً همان‌جایی معنا پیدا می‌کند که سرورِ owner استاندارد نباشد.

تمامِ این استدلال -- و اینکه اگر owner به‌جایِ application token از client
token استفاده کند چه چیزی عوض می‌شود (client token طبقِ API مستند اصلاً
اجازهٔ `POST /message` ندارد؛ فقط برای stream و management است) -- در یک جای
واحد و پرکامنت نوشته شد: `(*Sender).endpoint` در
`internal/adapter/sender/gotify/sender.go`. همان‌جا اولین جایی است که باید
با owner چک شود -- این فرض هنوز تأیید نشده و ماهیتِ ریسکِ اصلیِ این کار است.

## `ValidateAddress`

Application id در Gotify یک عددِ صحیحِ مثبت است -- primary keyِ خودِ اپلیکیشن،
از ۱ شروع می‌شود -- بر پایهٔ مدلِ داده‌ایِ مستندِ Gotify، نه چک‌شده روی یک سرورِ
واقعی. فقط همین چک شد؛ شکلِ دیگری برای آن مستند نیست.

## طبقه‌بندیِ خطا

Gotify مثلِ بقیه یک vocabulary مستندِ errcode ندارد، پس http status حکم
می‌کند: `401`/`403` یعنی توکن -- permanent -- `429` transient با
`defaultRetryAfter`، `5xx` transient، بقیهٔ `4xx` permanent. عمداً هیچ حالتی
به `SendUnreachable` نگاشت نشد: آن یعنی provider گیرنده را رد کرده، و Gotify
مستند نمی‌کند دربارهٔ مشترکینِ یک application چیزی برگرداند.

## پروتو

`api/proto/notification/v1/common.proto`: `CHANNEL_GOTIFY = 8` اضافه شد،
کنارِ `CHANNEL_MATRIX = 5` که دست‌نخورده ماند. `buf generate` اجرا شد.

## جاهایی که یک کانالِ جدید همیشه لمس می‌کند

`internal/core/shared/channel.go` (`ChannelGotify`، `AllChannels`،
`ValidateAddress`، `isGotifyAppID`)، `internal/adapter/sender/registry.go`
(`Fallback.Gotify`، دو شاخه)، `internal/config/settings/sender.go`،
`internal/bootstrap/dispatcher.go`، `internal/adapter/api/grpcsrv/mapper.go`
(هر دو جهت)، SDK (`ChannelGotify`، `GotifyCredential`، `Gotify()`،
`convert.go`)، `contract_test.go`، CHECK constraintهای `00005`/`00008`،
تمپلیت‌های پرتال، `README.md`، `docs/CONFIG.md`، `docs/ARCHITECTURE.md`، هر
دو SDK README، `.env.dispatcher.example`. همه‌جا Gotify کنارِ Matrix اضافه
شد، نه به‌جایِ آن.

## Migration: بدونِ redeploy

سرویس هیچ‌وقت deploy نشده، پس طبقِ رویهٔ همین repo (همان‌طور که
`description`/`reviewed_at` به `00003` اضافه شدند)، CHECK constraint مستقیم
در `00005_create_credentials.sql` و `00008_create_deliveries.sql` عوض شد --
`gotify` کنارِ بقیه اضافه شد. روی dev دستی اعمال شد و با یک scratch database
اثبات شد بدونِ drift (همان رویه‌ای که پایین‌تر آمده).

# Files Changed

- `internal/adapter/sender/gotify/{sender,config,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/gotify/sender_test.go` *(تازه)*
- `internal/core/shared/{channel,const}.go` *(`ChannelGotify`، `isGotifyAppID`)*
- `internal/core/shared/channel_test.go`
- `internal/adapter/sender/registry.go`، `registry_test.go` *(`Fallback.Gotify`)*
- `internal/adapter/sender/contract_test.go` *(کیسِ gotify اضافه شد)*
- `internal/config/settings/sender.go` *(`Gotify{Token, ServerURL}`)*
- `internal/bootstrap/dispatcher.go`
- `internal/adapter/api/grpcsrv/mapper.go` *(هر دو جهت)*
- `api/proto/notification/v1/common.proto` + `sdk/go/notification/v1/*.pb.go`
  *(`CHANNEL_GOTIFY = 8`، از `buf generate`)*
- `sdk/go/srosha/{channel,credential_types,convert}.go`، `setup_test.go`
  *(`GotifyCredential`، `Gotify()`)*
- `migrations/{00005_create_credentials,00008_create_deliveries}.sql`
  *(`gotify` به لیستِ CHECK اضافه شد)*
- `public/templates/portal/{source_new,senders,layout}.html`
  *(گزینه‌ی `gotify`؛ شمارشِ کانال‌ها به هشت به‌روز شد)*
- `.env.dispatcher.example`
- `README.md`، `docs/CONFIG.md`، `docs/ARCHITECTURE.md` *(کانال‌ها هشت‌تا شدند)*
- `sdk/go/README.md`، `sdk/go/README.fa.md`

# Tests Run

- `go build ./...` — سبز
- `go test -count=1 ./...` — سبز، همه‌ی پکیج‌ها (`matrix` و `gotify` هر دو)
- `go test -count=1 -tags=integration ./internal/adapter/db/postgres/` —
  سبز، روی `srosha-postgres-dev` بعد از اعمالِ دستیِ CHECK تازه
- `go test -count=1 -race ./...` در `sdk/go` — سبز
- `make prepush` — سبز (fmt، golines، vet، arch-check، sqlc-check، buf lint،
  golangci-lint، race tests، sdk)
- گاردِ contract: `TestEverySDKCredentialParsesOnThisSide` و
  `TestTheChannelNamesMatch` با `-v` اجرا شدند، کیسِ gotify واقعاً رد شد.
- خرابکاریِ واقعی و برگرداندنش: در `classify` (`gotify/errors.go`)، کیسِ
  `429` را عمداً `SendPermanent` کردم به‌جایِ `SendTransient`. تست:

  ```
  --- FAIL: TestWhatIsWorthAnotherAttempt/too_much,_too_fast (0.00s)
      sender_test.go:192: kind = 1, want 0 (TooManyRequests: slow down)
  ```

  خروجیِ واقعی و قرمز بود، نه cache و نه خالی. فایل بعد از آن به حالتِ اول
  برگردانده شد و تست دوباره سبز شد.
- Migration: هر دو دیتابیس (`srosha-postgres-dev` بعد از ALTER دستی، و یک
  scratch database تازه از فایل‌های migration) از نظرِ `information_schema`
  و CHECK constraintِ هر دو جدول یکسان بودند. scratch database حذف شد؛ dev
  دست‌نخورده ماند.

# Follow-ups / Risks

- **بزرگ‌ترین ریسک: فرضِ شکلِ API.** این سرویس دسترسیِ شبکه ندارد، پس اینکه
  Gotify واقعاً `application_id` را از query parameterِ `appid` می‌خواند یا
  نه -- یا اصلاً به آن نیاز دارد یا نه -- تأیید نشده. کامنتِ بالای
  `(*Sender).endpoint` این فرض و جایگزینِ آن (client token) را کامل نوشته؛
  اولین کاری که با یک سرورِ Gotifyِ واقعی باید انجام شود، تست کردنِ همین فرض
  است.
- `errors.go` مطمئن نیست شکلِ json خطای Gotify چیست (`apiError` یک حدسِ
  مستندشده است، نه واقعیت). اگر غلط باشد، فقط `Detail` را کم‌رنگ‌تر می‌کند --
  طبقه‌بندی همچنان روی http status تکیه دارد و درست می‌ماند.
- **ارسالِ موفق آزموده نشد**، مثلِ بقیهٔ کانال‌ها. یک سرورِ Gotifyِ واقعی
  می‌خواهد.
- در حینِ این کار، حذفِ کاملِ Matrix هم انجام و کامل تست شده بود -- شامل
  `reserved` کردنِ عددِ `5` در پروتو و باریک‌کردنِ CHECK constraintها -- ولی
  owner قبل از هر commit ای نظرش را عوض کرد: Matrix می‌ماند. آن تغییر کامل
  برگردانده شد؛ این گزارش فقط چیزی را ثبت می‌کند که واقعاً می‌رود: Gotify
  اضافه شد، Matrix دست‌نخورده ماند.

# Instruction

اول: کانالِ Gotify اضافه شود -- credential اش `server_url` +
`application_token` (توکن راز، url در config)، آدرس اش `application_id`،
proto number اش `8` -- و Matrix حذف شود، با `reserved` نگه‌داشتنِ عددِ `5` در
پروتو و بدونِ دست‌زدن به گزارش‌های تاریخیِ `docs/changes/`.

بعد، owner قبل از هر commit ای نظرش را عوض کرد: Matrix می‌ماند، Gotify هم
می‌ماند. کارِ حذف -- که کامل انجام و تست شده بود -- به‌طورِ کامل برگردانده شد
(شش فایلِ `internal/adapter/sender/matrix/` از `master` بازخوانی شدند، نه از
حافظه)، و پروتو، migrationها، SDK، تمپلیت‌ها و مستندات هر دو کانال را با هم
نشان می‌دهند. عددِ enum ی که به Gotify داده شده بود (`8`) عوض نشد.
