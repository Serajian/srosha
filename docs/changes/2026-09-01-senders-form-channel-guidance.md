Branch: `fix/senders-form-channel-guidance`

# Summary

صفحهٔ Senders در پرتال مشتری یک فیلد Settings دارد که مشتری باید در آن json خام بنویسد.
تا حالا این فرم نمی‌گفت چه چیزی آنجا می‌رود:

- placeholder برای هر هشت channel یکی بود، `{"host":"smtp.acme.test"}`. این فقط برای email
  درست است. برای پنج channel دیگر کلید اشتباهی را پیشنهاد می‌کرد، و برای `telegram`، `bale`
  و `fcm` که اصلاً settings نمی‌خوانند، دعوتی بود به نوشتن چیزی که هیچ‌جا خوانده نمی‌شود.
- hint زیرش می‌گفت «What each channel wants is in the SDK's README»، بدون link. آن README
  هم نام فیلدهای struct در Go را نوشته — `PhoneNumberID`، `ServerURL` — نه کلیدهای json که
  همین فرم پارس می‌کند.
- hint فیلد Name قانون اسم را نمی‌گفت، و مشتری که آن را می‌شکست فقط
  «credential name has the wrong format» می‌گرفت، بدون اینکه format کجا نوشته شده باشد.

حالا فرم خودش این‌ها را می‌گوید.

`internal/adapter/api/web/portal_channels.go` جدید است و یک type دارد، `channelGuide`: برای
هر channel، secret آن چیست، یک نمونهٔ json از settings آن، و جمله‌ای که کلیدها را نام می‌برد.
`channelGuides` هشت خط دارد، به همان ترتیبی که `shared.AllChannels` می‌دهد، پس `<select>` فرم
و راهنمای زیرش یک لیست‌اند نه دو تا.

منبع محتوای هر خط `internal/adapter/sender/<channel>/config.go` است — `host`، `port`،
`username`، `from`، `content_type` برای email؛ `homeserver`؛ `server_url`؛
`phone_number_id`؛ `key_id`، `team_id`، `topic`، `environment` — و برای آن سه تا که
`config.go` ندارند، جمله‌اش این است که این channel هیچ settings نمی‌گیرد. متن secret هم از
همان‌جا و از `internal/adapter/sender/registry.go` درآمده: برای fcm کل فایل service account
است، برای apns محتوای فایل `.p8`، برای gotify همان application token.

`sendersPage` یک فیلد `Guides` گرفت، و `newSendersPage` اضافه شد چون این صفحه از سه مسیر
render می‌شود (`showSenders` با و بدون خطا، و `renderSenders`) و هر سه باید راهنما را
داشته باشند؛ صفحه‌ای که آن را جا بیندازد دوباره همان فرمی است که چیزی نمی‌گوید.

فیلدی که متن مربوط به Secret را نگه می‌دارد `Identity` اسم دارد، نه `Secret`. اسم اولش
`Secret` بود و gosec درست ایراد گرفت (`G117` و `G101`): یک struct field به اسم `Secret` با
یک string literal کنارش، دقیقاً شکل credential ای است که کسی داخل کد نوشته باشد. اینجا آن
رشته فقط متن راهنماست، ولی خاموش‌کردن آن check برای این یک مورد بدتر از عوض‌کردن اسم بود.

در template زیر Secret و زیر Settings یک `<ul class="guide">` هست با یک `<li>` برای هر
channel. **همهٔ هشت خط render می‌شوند.** `public/static/portal/senders.js` — اولین script
این surface — فقط خط channel انتخاب‌شده را نگه می‌دارد، بقیه را `hidden` می‌کند، و
placeholder فیلد Settings را از `data-settings` همان خط برمی‌دارد. یعنی script یک بهبود است
نه یک شرط: اگر بارگذاری نشود صفحه بلندتر است و همچنان درست.

## صفحهٔ Reference

آن hint قدیمی به README ی اشاره می‌کرد که مشتری تازه‌وارد اصلاً به آن دسترسی ندارد: در
module ای است که تازه بعد از دانستن اینکه چه چیزی را import کند به دستش می‌رسد. پس خود سند
آمد داخل پرتال، روی `/reference`، پشت همان guard بقیهٔ صفحه‌ها.

همان فایل است، نه خلاصه‌اش. `make sdk-docs` آن را از `sdk/go/README.md` به
`public/guarded/portal/sdk.md` کپی می‌کند، چون embed directive نمی‌تواند بیرون از `public/`
را ببیند و `sdk/go` هم module جداگانه‌ای است که server حق import کردنش را ندارد. کپی همان
چیزی است که drift می‌کند، پس `TestThePortalsCopyOfTheSDKReadmeIsCurrent` در package `public`
دو فایل را بایت‌به‌بایت مقایسه می‌کند و تا وقتی یکی نباشند fail است — و در پیام خطا نوشته
`run: make sdk-docs`.

`public/guarded/` تا حالا یک ساکن داشت، صفحهٔ architecture در admin، که به‌صورت بایت خام
بیرون می‌رود. این دومی از در دیگری خارج می‌شود: markdown است و به‌عنوان یک field به template
داده می‌شود، پس مثل هر field دیگری escape می‌شود. قاعدهٔ آن directory دست‌نخورده می‌ماند —
هیچ‌چیز آنجا به‌عنوان template پارس نمی‌شود، که برای این فایل حیاتی است چون پر از `{{` و
`}}` است. توضیح این تفاوت در doc comment خود `public/embed.go` نوشته شد.

هیچ markdown renderer ای اضافه نشد. README خودش hard-wrap شده — از ۸۸۹ خط فقط ۲۲ خط بالای
۸۰ کاراکتر است — و همان‌طور که نوشته شده خوانده می‌شود. به‌جایش `chrome` یک فیلد `Wide`
گرفت و layout ستون را برای این یک صفحه از ۴۰۴ به ۸۲۰ پیکسل باز می‌کند؛ بقیهٔ صفحه‌ها که
form اند دست‌نخورده‌اند.

لینک‌ها هم بسته شدند: `Reference` در nav پرتال، و زیر راهنمای Settings در فرم Senders یک
جمله که به همان صفحه اشاره می‌کند — دقیقاً جای همان اشارهٔ بی‌مقصد قبلی.

## فیلد Secret که style نداشت

موقع نگاه‌کردن به صفحهٔ render شده پیدا شد: در `portal.css` انتخابگر
`input[type="email"], input[type="text"]` بود و `password` در آن نبود، پس تنها فیلد password
کل پرتال — همین فیلد Secret — با ظاهر پیش‌فرض مرورگر، یک جعبهٔ سفید کوچک، وسط یک فرم تیره
می‌نشست. `input[type="password"]` به همان انتخابگر اضافه شد. `admin.css` همین کمبود را دارد
ولی هیچ صفحهٔ admin فیلد password ندارد، پس دست نخورد.

# Files Changed

- `internal/adapter/api/web/portal_channels.go` *(جدید: `channelGuide` و جدول هشت‌خطی)*
- `internal/adapter/api/web/portal_identity.go` *(فیلد `Guides` روی `sendersPage`، و
  `newSendersPage` که هر سه مسیر render از آن می‌سازند)*
- `internal/adapter/api/web/portal.go` *(لیست فایل‌ها در doc comment، فیلد `Wide` روی
  `chrome` و متغیر `wide`، خواندن README موقع ساختن surface، و route جدید)*
- `internal/adapter/api/web/portal_reference.go` *(جدید: `referenceHandler`)*
- `internal/adapter/api/web/portal_const.go` *(`pathReference`، `pageReference`،
  `fileSDKReadme`)*
- `internal/adapter/api/web/portal_test.go` *(شش تست و helper آن‌ها، و `/reference` در
  `TestEveryPageBehindTheGuardCanReachTheOthers`)*
- `public/templates/portal/senders.html` *(option ها از جدول، دو لیست راهنما، قانون اسم در
  hint فیلد Name، لینک به صفحهٔ reference، و tag script)*
- `public/templates/portal/reference.html` *(جدید)*
- `public/templates/portal/layout.html` *(لینک Reference در nav، و کلاس `wide` روی ستون)*
- `public/static/portal/senders.js` *(جدید)*
- `public/static/portal/portal.css` *(کلاس `.guide`، کلاس `.doc` و `.form.wide`، و
  `input[type="password"]`)*
- `public/guarded/portal/sdk.md` *(جدید: کپی `sdk/go/README.md`)*
- `public/sdk_readme_test.go` *(جدید: تستی که کهنه‌بودن آن کپی را می‌گیرد)*
- `public/embed.go` *(doc comment: ساکن دوم `guarded/` و اینکه از چه دری بیرون می‌رود)*
- `Makefile` *(target `sdk-docs`)*

# Tests Run

- `go build ./...` — clean
- `go test ./internal/adapter/api/web/...` — pass
- `go test ./...` — pass
- `golangci-lint run` — clean

تست‌های اضافه‌شده:

- `TestEveryChannelIsOnTheSendersForm` — روی `shared.AllChannels` می‌چرخد، پس channel نهم
  که بدون خط راهنما اضافه شود همین‌جا می‌افتد.
- `TestTheSendersFormSaysWhenAChannelTakesNoSettings` — برای telegram و bale و fcm نمونه‌ای
  پیشنهاد نمی‌شود و جمله‌اش «no settings» دارد؛ برای آن پنج تای دیگر نمونه خالی نیست.
- `TestTheSendersFormStatesTheNameRule` — قانون `credential.validateName` روی صفحه هست.
- `TestTheReferencePageCarriesTheSDKReadme` — سه عنوان خود README روی صفحه هست، پس یک کپی
  خالی یا فیلدی که از template افتاده باشد بی‌صدا رد نمی‌شود.
- `TestTheReferencePageDoesNotRenderTheReadmeAsMarkup` — سند به‌عنوان متن می‌نشیند نه markup.
- `TestTheReferencePageNeedsASession` — پشت همان guard است.
- `TestThePortalsCopyOfTheSDKReadmeIsCurrent` (در package `public`) — کپی با
  `sdk/go/README.md` بایت‌به‌بایت یکی است.

`make arch-check` هم اجرا شد و clean بود.

# Follow-ups / Risks

- **جدول راهنما و `config.go` ها دو جا نوشته شده‌اند و می‌توانند از هم جدا بیفتند.** یک
  adapter حق ندارد adapter دیگری را import کند، پس `web` نمی‌تواند کلیدها را از
  `internal/adapter/sender/<channel>` بخواند. تست بالا فقط *مجموعهٔ* channel ها را نگه
  می‌دارد؛ اینکه `phone_number_id` همان چیزی باشد که `whatsapp.ParseConfig` می‌خواند را
  هیچ چیز جز review نگه نمی‌دارد. راه واقعی‌اش این است که صورت درست این جدول جایی باشد که
  هر دو طرف بتوانند ببینند — مثل کاری که `internal/adapter/sender/contract_test.go` با json
  خود SDK می‌کند. الان ساخته نشده.
- **`sdk/go/README.md` هنوز نام فیلدهای Go را نشان می‌دهد، نه کلیدهای json.** حالا که همان
  فایل روی `/reference` خوانده می‌شود، این تناقض داخل خود پرتال دیده می‌شود: فرم
  `phone_number_id` می‌گوید و سند `PhoneNumberID`. هر دو درست‌اند — یکی json ی است که فرم
  پارس می‌کند و دیگری فیلدی از struct که SDK می‌سازد — ولی سند این را جایی نمی‌گوید. جای
  درستش خود README است، و در این تغییر دست نخورد.
- **`README.fa.md` سرو نمی‌شود.** SDK دو README دارد و فقط انگلیسی داخل پرتال آمد. لینک
  `[فارسی](README.fa.md)` بالای همان سند حالا به فایلی اشاره می‌کند که از این صفحه در
  دسترس نیست.
- **کپی README یک truth دوم است.** `TestThePortalsCopyOfTheSDKReadmeIsCurrent` آن را
  می‌گیرد، ولی فقط وقتی تست‌ها اجرا شوند؛ `make sdk-docs` عمداً به هیچ target خودکاری وصل
  نشد، چون فایل می‌نویسد و `precommit` و `prepush` هر دو read-only اند.
- **این اولین javascript این surface است.** فایل کوچک است و هیچ داده‌ای در خودش ندارد — فقط
  data attribute هایی را می‌خواند که template نوشته — ولی از این به بعد یک چیز بیشتر برای
  نگه‌داشتن هست. صفحه عمداً طوری نوشته شد که بدون آن هم کامل باشد.

# Instruction

فرم Senders باید خودش یاد بدهد که هر channel چه می‌خواهد: با انتخاب هر channel، همان
channel نمونهٔ settings خودش را در placeholder و hint خودش را زیر فیلد نشان بدهد، و برای
`telegram`، `bale` و `fcm` صریح بگوید که این channel هیچ settings نمی‌گیرد. چون secret هر
channel هم خیلی فرق دارد — یک bot token در برابر یک فایل json کامل در برابر محتوای یک `.p8` —
آن هم گفته شود. و در همان صفحه، قانون اسم credential (فقط حروف کوچک، رقم و خط تیره، از
`internal/core/domain/credential/entity.go`) در hint نوشته شود.

بعد از دیدن صفحهٔ render شده، دو چیز دیگر هم خواسته شد و در همین تغییر انجام شد: فیلد
Secret که style نداشت درست شود، و همان README ی که hint قدیمی به آن اشاره می‌کرد جایی در
خود پرتال قابل خواندن باشد.
