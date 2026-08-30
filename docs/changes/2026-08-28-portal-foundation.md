Branch: `feat/portal-spec`

# Summary

باینریِ سوم ساخته شد: `console`. کاری که می‌کند این است که یک نفر با یک کدِ
یک‌بارمصرف وارد می‌شود، و هر تغییری که بعداً قرار است انجام شود از قبل جایی
برای ثبت‌شدن دارد. این پیاده‌سازیِ فاز یکِ
`docs/superpowers/specs/2026-08-28-customer-portal-design.md` طبقِ
`docs/superpowers/plans/2026-08-28-portal-foundation.md` است.

اسمِ باینری `console` است نه `portal`، چون قرار است **دو سطح** را serve کند: پورتالِ
مشتری امروز، و سطحِ ادمین بعداً روی listenerــِ خودش. تصمیم این شد که هر دو در یک
باینری بمانند و جدایی در سطحِ پورت باشد — و آن انتخاب یک چیز را حیاتی می‌کند:
**cookie با پورت جدا نمی‌شود**، پس sessionــِ یک مشتری به پورتِ ادمین هم می‌رسد و
تنها چیزی که جلویش را می‌گیرد چکِ نقش در گاردِ خودِ ادمین است، خوانده‌شده از ردیفِ
زنده در هر request.

سه domain جدید اضافه شد. `user` یک آدم است — customer و operator یک جور ردیف‌اند
با `role` متفاوت، چون دو جدولِ حساب یعنی دو جریانِ ورود و دو دسته باگ. هیچ ستونِ
password ندارد و نخواهد داشت: ورود با کدِ یک‌بارمصرف است، و همین است که اجازه
می‌دهد اولین operator با دست و با SQL نوشته شود — هشِ argon2 را نمی‌شود تایپ کرد،
ایمیل را می‌شود. `logincode` کدِ فرستاده‌شده و اتفاقی است که برایش افتاده، با
چهار قاعده‌ای که شش رقم را امن می‌کند: یک‌بار مصرف، سقفِ حدس، عمرِ کوتاه، و سقفِ
درخواست. `session` یک مرورگرِ واردشده است و سمتِ server نگه داشته می‌شود، تا
غیرفعال‌کردنِ یک نفر در همان request بعدی او را بیرون بیندازد نه هر وقت که
token‌ای که دستش است منقضی شود.

`usecase.SignIn` این‌ها را به هم می‌بندد. مهم‌ترین رفتارش این است که ثبت‌نام و
ورود **یک جریان‌اند**: آدرسی که هیچ‌کس استفاده نکرده در همان مسیر `customer`
می‌شود. دو جریانِ جدا یعنی یکی می‌گوید «این آدرس گرفته شده» و دیگری می‌گوید «کد
فرستاده شد»، و هرکسی می‌تواند این دو را از هم تشخیص بدهد و بفهمد چه کسی اینجا
حساب دارد. یک جریان چیزی برای لو دادن ندارد.

`usecase.Gate` نقطه‌ای است که هر تغییرِ نویسنده از آن رد می‌شود و یک ردیفِ
`audit_log` می‌نویسد. **هیچ چیزی در این تغییر آن را صدا نمی‌زند و همین هدف است**:
gateای که بعد از ده caller اضافه شود، gateای است با ده راهِ دور زدن. اولین
caller‌اش ثبتِ source است، در planِ بعدی. ترتیبش هم عمدی است — اول ثبت، بعد
اجرا: لاگ **تلاش** را ثبت می‌کند، چون تغییری که کسی نتواند حسابش را پس بدهد بدتر
از تغییرِ ردشده است، و تلاشِ ناموفق دقیقاً همان چیزی است که یک بررسی دنبالش
می‌گردد.

`internal/adapter/mailer` تنها پیامی را می‌فرستد که این سرویس از طرفِ خودش
می‌فرستد. **از صفِ srosha رد نمی‌شود** و مستقیم SMTP می‌رود: ورودی که به سرویسی
وابسته باشد که داری واردش می‌شوی تا درستش کنی، یک تله است.

`internal/adapter/api/web` صفحه‌هاست: HTMLــِ server-render، فرمِ ساده، بدونِ
build pipeline و بدونِ JavaScript. خودِ HTML و CSS در `public/` ریشهٔ ریپو
نشسته‌اند نه لای درختِ Go، تا کسی که یک صفحه یا یک stylesheet را عوض می‌کند
مجبور نباشد دنبالشان داخلِ package بگردد. باینری همچنان یک فایلِ تنهاست:
`public/embed.go` آن‌ها را embed می‌کند، چون `go:embed` نمی‌تواند از پوشهٔ
package خودش بیرون برود.

داخلِ `public/` دو نیمهٔ جدا هست و همین جدایی نکته است — `static/` را مرورگر
می‌گیرد و `templates/` روی server رندر می‌شود و **هرگز serve نمی‌شود**. سرو کردنِ
`templates/` یعنی تحویلِ شکلِ هر صفحه و نامِ هر فیلد در یک درخواست، پس
`web.browserFiles` قبل از هر چیز داخلِ `static/portal` می‌رود و file server فقط
همان را می‌بیند.

فایل‌بندیِ `web` قانونش این است: **یک فایل، یک تایپ، و متدهای هیچ تایپی از فایلِ
خودش بیرون نمی‌روند.** `sessions` صاحبِ cookie و `guard` است، `renderer` صاحبِ
قالب‌ها، و `signInHandler` و `accountHandler` هرکدام فقط چیزی را نگه می‌دارند که
لازم دارند. هیچ تایپی نیست که همهٔ handlerها از داخلش به همه‌چیز برسند — که همان
راهی است که یک صفحه بی‌سروصدا به چیزی وابسته می‌شود که کسی نمی‌خواست به آن بدهد.

`web.go` فقط جدولِ مسیرهاست، و اینکه کدام صفحه session لازم دارد از روی همان
جدول خوانده می‌شود چون `guard` آن‌جا نشسته نه بالای هر handler — پس صفحهٔ جدید
بدونِ تصمیم‌گرفتن دربارهٔ این اضافه نمی‌شود.

طراحیِ صفحه‌ها Lapis است — همان جهتی که انتخاب شد: یک ستونِ کاشی‌کاری‌شده کنارِ
فرم، درنای برند به‌صورتِ مونوی سفید، و طلایی که فقط و فقط روی شش رقمِ کد
می‌نشیند. یک `portal.css` و بس.

## چیزهایی که در plan غلط بود و عوض شد

**دو تا از پنج شناسه‌ی داخلِ plan را `ulid` domain رد می‌کرد.** Crockford base32
حروفِ I، L، O و U را ندارد، و `01K0USER…` حرفِ U داشت و `01K0CODE…` حرفِ O.
اثرش موذی بود: در Task 1 قرار بود آن INSERT به‌خاطرِ CHECKـِ `role` رد شود، ولی
در واقع سرِ خودِ شناسه رد می‌شد. با `01K0ACCT…` و `01K0CDE…` عوض شد و خودِ plan
هم اصلاح شد.

**`Role` را plan داخلِ `entity.go` گذاشته بود.** CONVENTIONS می‌گوید `entity.go`
دقیقاً یک تایپ اعلام می‌کند و enum‌ها در `types.go` می‌روند. منتقل شد.

**plan هر دو `Code` و `Session` را در یک package به اسمِ `signin` می‌گذاشت.** هر
دو entity واقعی‌اند — جدولِ خودشان، کلیدِ خودشان — و CONVENTIONS می‌گوید domainــی
که دو چیز نگه می‌دارد دو domain است، و package نباید به اسمِ یک **فعالیت**
نام‌گذاری شود. شد `logincode` و `session`.

**helperــِ `truncate` در تست‌ها فقط `sources` را پاک می‌کرد** و `users` به آن FK
ندارد، پس ده تستِ integration ردیف‌هایشان را برای هم جا می‌گذاشتند. حالا
`TRUNCATE sources, users CASCADE` است و بقیهٔ جدول‌های جدید با FKشان پاک می‌شوند.

**`smtp.Identity` فیلدِ `From` ندارد** ولی plan فرض کرده بود دارد. آدرسِ فرستنده
آرگومانِ جداگانهٔ `mailer.New` شد — که درست‌تر هم هست: حسابی که با آن authenticate
می‌کنیم همیشه آدرسی نیست که یک نفر باید ببیند و جواب بدهد.

**`registry.SMTPDialer` کلِ گروهِ `settings.HTTPClient` را می‌گرفت** و فقط
`Timeout` را استفاده می‌کرد. حالا خودِ `time.Duration` را می‌گیرد، چون دو باینریِ
نیازمندش آن را از دو جای متفاوت می‌خوانند.

**`GET /` در `http.ServeMux` همه‌چیز را می‌گیرد**، پس `GET /signout` به handlerــِ
خانه می‌رسید و ۴۰۴ می‌گرفت به‌جای ۴۰۵. با الگوی دقیقِ `GET /{$}` درست شد.

# Files Changed

- `migrations/00008_create_users.sql` *(new — یک جدول برای همه، بدونِ password)*
- `migrations/00009_create_login_codes.sql` *(new)*
- `migrations/00010_create_sessions.sql` *(new)*
- `migrations/00011_create_audit_log.sql` *(new — append only)*
- `internal/core/domain/user/{entity,types,const,errors,port}.go` *(new)*
- `internal/core/domain/logincode/{entity,const,errors,port}.go` *(new)*
- `internal/core/domain/session/{entity,const,errors,port}.go` *(new)*
- `internal/core/usecase/gate.go` *(new — `Act`, `AuditEntry`, `AuditLog`, `Gate`)*
- `internal/core/usecase/signin.go` *(new — `Mailer`, `SignIn`)*
- `internal/core/usecase/const.go` *(`MaxCodeRequests`, `CodeRequestWindow`)*
- `internal/core/usecase/fakes_test.go` *(fakeهای user/code/session/mailer)*
- `internal/adapter/db/postgres/{user,logincode,session,audit}.go` *(new)*
- `internal/adapter/db/postgres/queries/{user,logincode,session,audit}.sql` *(new)*
- `internal/adapter/db/postgres/gen/*` *(sqlc، تولیدشده)*
- `internal/adapter/db/postgres/testing_test.go` *(truncate حالا users را هم پاک می‌کند)*
- `internal/adapter/mailer/{mailer,const}.go` *(new)*
- `internal/adapter/api/web/{web,session,render,signin,account,form,errors,const}.go` *(new)*
- `public/embed.go` *(new — تنها فایلِ Goیِ آن‌جا)*
- `public/templates/portal/{layout,signin,code,account}.html` *(new)*
- `public/static/portal/{portal.css,crane.svg}` *(new)*
- `internal/config/console.go`, `internal/config/settings/console.go` *(new)*
- `internal/registry/smtp.go` *(امضا: `time.Duration` به‌جای گروهِ settings)*
- `internal/bootstrap/console.go` *(new)*, `internal/bootstrap/const.go` *(`binaryConsole`)*
- `internal/bootstrap/dispatcher.go` *(caller اصلاح شد)*
- `cmd/console/main.go` *(new)*
- `Makefile` *(`console` در `BINARIES`، `build-console`، `run-console`)*
- `.env.console.example` *(new)*
- `docs/superpowers/plans/2026-08-28-portal-foundation.md` *(دو ULIDــِ نامعتبر)*
- `docs/CONFIG.md` *(پورت‌ها، کلیدهای `NOTIF_CONSOLE_*`، بخشِ تازهٔ «Pages and assets»)*
- `docs/ARCHITECTURE.md` *(تصمیمِ «دو سطح، یک باینری» و دلیلِ cookie)*

# Tests Run

- `go build ./...` — clean
- `make prepush` — pass
- `go test ./internal/core/domain/{user,logincode,session}/` — pass (۱۴ تست)
- `go test ./internal/core/usecase/ -run "Gate|SignIn|..."` — pass (۱۲ تست)
- `go test ./internal/adapter/mailer/` — pass (۵ تست)
- `go test ./internal/adapter/api/web/` — pass (۶ تست)
- `go test -tags=integration ./internal/adapter/db/postgres/` — pass
- روی دیتابیسِ واقعی، با خودِ باینری: کلِ جریان از `/signin` تا صفحهٔ حساب کار
  می‌کند، و `UPDATE users SET is_active = false` در همان request بعدی آدم را
  بیرون می‌اندازد.
- محافظ‌های جدول با دست امتحان شدند: نقشِ نامعتبر، ULIDــِ نامعتبر، و ایمیلِ تکراری
  هر سه رد می‌شوند.

# Follow-ups / Risks

- **پایِ SMTP امتحانِ واقعی نشده.** dialer اجباراً STARTTLS می‌خواهد و mailpit
  پیش‌فرض TLS ندارد، پس در امتحانِ محلی کد از خودِ جدولِ `login_codes` خوانده شد.
  خودِ ساختنِ پیام تستِ مستقیم دارد، ولی تحویلِ واقعی به یک mail server هنوز
  دیده نشده.
- **فونت‌ها stack سیستمی‌اند، نه فایل.** `portal.css` اول سراغِ Vazirmatn
  می‌رود و اگر نبود به فونتِ سیستم می‌افتد. هیچ درخواستی به بیرون نمی‌رود — که
  عمدی است — ولی تا وقتی دو فایلِ woff2 کنارِ stylesheet ننشیند، ظاهر همانی نیست
  که در طراحی تأیید شد.
- **غیرفعال‌کردنِ یک نفر ردیفِ session او را پاک نمی‌کند**، فقط cookie را در
  همان request پاک می‌کند. اگر بعداً دوباره فعالش کنیم و مرورگر cookie را نگه
  داشته باشد، با همان session برمی‌گردد. spec دربارهٔ این چیزی نگفته؛ اگر
  «غیرفعال یعنی همهٔ sessionها بمیرند»، یک `DELETE FROM sessions` لازم است.
- **`Gate` هنوز caller ندارد** — عمدی، و در planِ بعدی صدا زده می‌شود. تستِ
  integration دارد که با repositoryــِ واقعی یک ردیف می‌نویسد، پس بی‌آزمون نمانده.
- **دو تستی که `docs/ARCHITECTURE.md` واجب می‌داند هنوز نوشته نشده‌اند**، چون
  سطحِ ادمین هنوز وجود ندارد: اینکه هر مسیرِ ادمین روی handlerــِ پورتال ۴۰۴ بدهد،
  و اینکه sessionــِ یک customer را گاردِ ادمین رد کند. اولین کاری است که با
  `adminweb` باید انجام شود — بدونِ آن‌ها آن تصمیم یک توضیح است نه یک خاصیت.
- **در دیتابیسِ dev یک ردیفِ super_admin با آدرسِ `you@example.test` ساخته شد**
  تا جریان امتحان شود. اگر لازمش نداری پاکش کن.

# Instruction

«پیاده‌سازی کن» — یعنی planــِ فاز یکِ portal روی همین branchــِ `feat/portal-spec`
اجرا شود، ده task به ترتیب و با همان TDDــی که plan می‌گوید، با طراحیِ Lapis که
قبلش انتخاب شده بود. قرارِ صریح این بود که ورود و ثبت‌نام یک جریان باشند: کسی که
حساب دارد وارد شود، و کسی که ندارد همان‌جا برایش ساخته شود. commit و push جزوِ
دستور نبود.
