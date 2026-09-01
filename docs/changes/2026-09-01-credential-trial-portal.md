Branch: `feat/credential-trial`

# Summary

دکمه. task سه از چهار — و اولین قدمی که مشتری واقعاً می‌بیند.

```
POST /sources/:id/senders/:senderID/test
```

روی هر کارت در صفحهٔ senders یک «Send a test» نشست. می‌فرستد، و **حرفِ خودِ provider** را
روی همان صفحه نشان می‌دهد.

## چرا redirect نمی‌کند

مثل `keyHandler.issue`، لیست را دوباره render می‌کند به‌جای اینکه redirect کند. دلیلش
همان است: یک redirect جایی می‌خواهد که پیام را در این فاصله نگه دارد، و هر چنین جایی از
صفحه‌ای که برایش ساخته شده بیشتر عمر می‌کند.

## دو فیلد، نه یکی

`sendersPage` حالا هم `Problem` دارد هم `Result`. یکی با یک flag کنارش کافی نبود: صفحه
این دو را متفاوت نشان می‌دهد، و آن‌وقت template باید می‌پرسید این کدام‌شان است.

`listSendersWith` و `listSendersOK` هر دو به یک `renderSenders` می‌رسند، پس مسیرِ render
یکی است و فقط ورودی‌اش فرق می‌کند.

در CSS یک `.result` کنارِ `.problem` اضافه شد — همان شکل، با آبیِ برند به‌جای قرمز، تا از
یک نگاه بشود فرقشان را دید.

## دکمه روی هر هویت است، حتی خاموش‌ها

عمدی. یک هویتِ خاموش‌شده رد می‌شود — `Pick` همان‌طور که در production ردش می‌کند — و
مشتری همان جواب را می‌بیند. دکمه‌ای که فقط روی فعال‌ها باشد این را پنهان می‌کند، و
جوابِ «این هویت خاموش است» خودش اطلاعاتِ مفیدی است.

زیرِ لیست یک خط اضافه شد که می‌گوید تست به آدرسِ **پیش‌فرضِ همان کانال** می‌رود، تا کسی
مجبور نباشد حدس بزند پیام کجا رفت.

## `TrialPages` یک interface جدا است

نه یک متد روی `SenderPages`. پشتِ `SenderPages` را دو باینری می‌سازند و فقط console
می‌تواند بفرستد — همان استدلالی که در core باعث شد `Trials` type ــِ خودش باشد.

## سیم‌کشی

`buildIdentityCore` حالا `gate` را هم می‌گیرد و `usecase.NewTrials` را می‌سازد، با یک
`ratelimit.NewMemory(cfg.Console.TrialPerMinute, ...)` ــِ **جدا** از آنی که بالاتر ساخته
می‌شود. آن یکی سهمیهٔ فرستادن است و در process ــِ دیگری خرج می‌شود؛ یکی کردنشان یعنی یک
تست به‌نظر برسد که یک پیام از مشتری خورده.

# Files Changed

- `internal/adapter/api/web/portal_const.go` *(`pathSenderTest`)*
- `internal/adapter/api/web/portal_identity.go` *(`TrialPages`، `testSender`، `Result` روی صفحه، `renderSenders`)*
- `internal/adapter/api/web/portal.go` *(`PortalDeps.Trials`، چکِ nil، و mount شدنِ route)*
- `internal/adapter/api/web/portal_test.go` *(`memTrials` و چهار تست)*
- `internal/bootstrap/console.go` *(`core.trials`، سطلِ جدا، و `gate` که به `buildIdentityCore` رسید)*
- `public/templates/portal/senders.html` *(دکمه، خطِ نتیجه، و راهنمای زیرِ لیست)*
- `public/static/portal/portal.css` *(`.result`)*

# Tests Run

- `go build ./...` — clean
- `go test -count=1 ./...` — pass، شاملِ `TestNoAdminRouteAnswersOnThePortal` که جدولِ
  route ها را می‌خواند و با یک route ــِ تازه باید سبز بماند
- `make precommit` — pass
- `make prepush` — pass. دو چیز گرفت که `precommit` نگرفته بود و اصلاح شدند:
  یک امضای پارامتر که `gofumpt` کوتاه‌ترش می‌خواست، و `gosec` که
  `ActCredentialTest = "credential.test"` را راز حساب کرده بود (`//nolint` با دلیل).

# Follow-ups / Risks

- سقفِ `NOTIF_CONSOLE_TRIAL_PER_MINUTE` هنوز در `docs/CONFIG.md` ثبت نشده. task چهار.
- هیچ‌کدام از این تست‌ها یک provider ــِ واقعی را صدا نمی‌زنند. اینکه شش کانالِ ثبت‌شده
  واقعاً کار می‌کنند یا نه، همان چیزی است که این دکمه ساخته شد تا جوابش را بدهد — و تا
  کسی رویش فشار ندهد، جواب داده نشده.

# Instruction

قدمِ سومِ credential trial: دکمه‌ای در صفحهٔ senders ــِ پورتال که تست را می‌فرستد و
جوابِ خودِ provider را نشان می‌دهد.
