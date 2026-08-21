Branch: `feat/infra-layer`

# Summary

سومین قطعهٔ infra: پکیج `internal/infra/httpclient` که client بیرونی dispatcher را
می‌سازد — هم برای callback های webhook، هم برای API فرستنده‌ها.

بر خلاف postgres و nats، این پکیج `Connect` و `Ping` ندارد و دلیلش ماهوی است: تا
اولین درخواست چیزی باز نمی‌شود، و «سلامت یک http client» بی‌معنی است چون مقصد ثابتی
ندارد. پس `New(Config) (*http.Client, error)` و همین.

## حفره‌ای که بسته شد

مهم‌ترین چیز این commit این است. در `webhook/entity.go` بالای `validateURL` نوشته
شده بود:

> Shape only. A name that resolves to a private address passes here and must be
> checked again after DNS, at request time.

یعنی یک source می‌توانست `https://evil.example.com` را ثبت کند که DNS اش به
`169.254.169.254` — نقطهٔ metadata ابر — یا به `postgres` روی شبکهٔ خودمان اشاره
می‌کند. domain قبولش می‌کرد چون شکلش درست بود. آن چک دوم هیچ‌جا نبود.

حالا هست، در `net.Dialer.Control`. این تابع بعد از DNS و قبل از اتصال صدا زده
می‌شود، برای هر تلاش جداگانه، و آدرسِ resolve شده را می‌گیرد. سه چیز از این می‌آید که
یک چک روی خودِ URL نمی‌توانست بدهد:

- redirect هم چک می‌شود، چون هر hop یک dial تازه است.
- DNS rebinding هم چک می‌شود — اسمی که بار اول عمومی جواب می‌دهد و بار دوم داخلی.
- `Is4In6` گرفته می‌شود: `::ffff:10.0.0.1` یک آدرس RFC1918 است که کت IPv6 پوشیده،
  و بدون `Unmap()` از کنار هر تست دیگری رد می‌شد. این همان دور زدنی است که در چنین
  چک‌هایی جا می‌ماند.

و آدرسی که اصلاً resolve نشده رد می‌شود: اگر `Control` چیزی جز `ip:port` بگیرد،
فرضی که این چک رویش بنا شده غلط است و رد کردن جواب امن است.

## دو client، نه یکی

dispatcher دو جور تماس بیرونی دارد و نیازهایشان متضاد است. آدرس callback را source
انتخاب می‌کند، پس guard لازم دارد و redirect نباید دنبال شود. آدرس Telegram و Bale و
WhatsApp را خودمان انتخاب کرده‌ایم و ثابت است، پس guard رویشان مضر است — روزی ممکن
است یک API قانونی را ببندد.

یک client مشترک یعنی یا guard روی همه، یا از همه برداشته. پس `registry` دو تابع
دارد: `WebhookClient` و `SenderClient`.

نکته‌ای که ارزش دارد: `DenyPrivateAddresses` در `WebhookClient` از
`!w.AllowPrivateURL` می‌آید — همان کلیدی که `LoadWebhook` در production اجباراً
خاموشش می‌کند. یعنی یک تصمیم در دو جا اعمال می‌شود، نه دو تصمیم که ممکن است از هم
جدا بیفتند.

## چرخهٔ عمر بدون ready

`res.add` اینجا `ready` نمی‌دهد. این اولین باری است که آن شاخهٔ nil-پذیر در
`Resources.Ready` واقعاً استفاده می‌شود، و همان دلیلی که از اول برایش گذاشته شد:
چیزی که سلامت مستقلی ندارد نباید مجبور شود ادایش را در بیاورد.

ولی `close` واقعی است — `CloseIdleConnections` سوکت‌هایی را می‌بندد که به مقصد قبلی
باز مانده‌اند.

## config

گروه تازهٔ `settings.HTTPClient` با شش کلید، فقط برای dispatcher. عمداً از
`settings.HTTP` جداست، چون آن listener خودِ dispatcher است نه تماس بیرونی.

پنج کلید از شش‌تا معمولی‌اند. یکی دلیل واقعی دارد: `MAX_IDLE_PER_HOST`. پیش‌فرض Go
دو تاست، و با صد source یعنی تقریباً هر callback یک اتصال تازه.

# Files Changed

- `internal/infra/httpclient/client.go` *(`Config`، `validate`، `New`، و `denyPrivate`)*
- `internal/infra/httpclient/client_internal_test.go` *(تازه — جدول طبقه‌بندی آدرس، مستقیم روی `denyPrivate`)*
- `internal/infra/httpclient/client_test.go` *(تازه — رفتار واقعی با `httptest`: توقف درخواست روی loopback، عبور بدون guard، و redirect)*
- `internal/registry/httpclient.go` *(تازه — `WebhookClient`، `SenderClient` و `openHTTPClient` خصوصی)*
- `internal/config/settings/httpclient.go` *(تازه — شش کلید و دو `Check`)*
- `internal/config/dispatcher.go` *(گروه `HTTPClient` اضافه شد)*
- `.env.example`, `docs/CONFIG.md` *(همان کلیدها)*

# Tests Run

- `make prepush` — fmt-check، govet، arch-check، golangci-lint و `go test -race ./...` همه پاس

# Follow-ups / Risks

- کامنت بالای `validateURL` در `webhook/entity.go` هنوز می‌گوید این چک «باید» جای
  دیگری انجام شود. حالا انجام می‌شود، ولی متن به آن اشاره نمی‌کند. وقتی adapter
  webhook نوشته شد و این client واقعاً به آن وصل شد، آن کامنت باید به‌روز شود.
- `client_test.go` و `client_internal_test.go` هر دو لازم‌اند و دومی داخل پکیج است.
  این استثنای دوم بعد از `registry` است و دلیلش بالای فایل نوشته شده.
- `SenderClient` هنوز صدا زده نمی‌شود. تا وقتی فرستنده‌ها نوشته نشوند، فقط
  `WebhookClient` مصرف‌کننده خواهد داشت.
- شکل امضای HMAC و اینکه timestamp داخل امضا باشد یا نه، هنوز تصمیم‌گیری نشده. آن
  مال adapter است، نه این پکیج.

# Instruction

«برویم httpclient را بنویسیم» — با سه شرطی که قبل از نوشتن مطرح و تأیید شد: چک دوم
SSRF بعد از DNS در `net.Dialer.Control` انجام شود، دو client جدا ساخته شود چون
نیازهای callback و API فرستنده متضادند، و چون این پکیج سلامت مستقلی ندارد با
`ready: nil` در `Resources` ثبت شود.
