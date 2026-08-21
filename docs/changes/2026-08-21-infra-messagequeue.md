Branch: `feat/infra-layer`

# Summary

دومین قطعهٔ infra: پکیج `internal/infra/messagequeue` که اتصال به nats را باز
می‌کند، سلامتش را جواب می‌دهد و drain اش می‌کند. مثل `database`، هیچ چیز از srosha
نمی‌داند — نه stream ای می‌شناسد، نه subject ای، نه consumer ای.

تایپ `NATS` همان الگوی `Postgres` را دارد: `New` فقط config را validate می‌کند و به
هیچ چیزی دست نمی‌زند، `Connect` وصل می‌شود و تا وقتی JetStream جواب نداده برنمی‌گردد،
`Ping` سلامت را می‌گوید، و `Drain` می‌بندد.

## مرزی که با adapter کشیده شد

`Config` پنج میدان دارد: `URL` و چهار پارامتر اتصال. `Stream` و `DuplicateWindow`
عمداً وارد این پکیج نمی‌شوند و در `settings.MQ` می‌مانند تا adapter برشان دارد.

دلیلش این است که ساختن stream یعنی دانستن اسمش (`NOTIFY`)، subject هایش
(`notify.>`) و پنجرهٔ تکراری‌اش. این‌ها واژگان srosha هستند نه nats. اگر infra
استریم را می‌ساخت، همان چیزی می‌شد که در `database` از آن پرهیز کردیم: زیرساختی که
می‌داند این سرویس چه کار می‌کند. پس `internal/adapter/mq/nats/` استریم و consumer ها
را می‌سازد و این پکیج فقط دو دسته می‌دهد: `Conn()` و `JetStream()`.

## Drain به جای Close

`nats.Conn` دو راه بستن دارد. `Close` سوکت را همان لحظه می‌بندد و پیامی که handler
وسط پردازشش است بی‌ack می‌ماند. `Drain` اول subscription ها را می‌بندد، منتظر می‌ماند
تا آنچه در دست است تمام شود، بعد اتصال را می‌بندد.

چون at-least-once هستیم، `Close` پیام را گم نمی‌کند — دوباره می‌آید — ولی یعنی هر بار
deploy، چند پیام دوباره فرستاده می‌شوند. `Drain` این را برمی‌دارد.

یک نکته که موقع نوشتن پیدا شد: `conn.Drain()` **قبل از** تمام شدن کار برمی‌گردد. تنها
راه منتظر ماندن، `ClosedHandler` است که nats بعد از تمام شدن drain صدا می‌زند. پس
`Connect` یک کانال می‌سازد و آن handler می‌بنددش، و `Drain` روی همان کانال منتظر
می‌ماند تا `DrainTimeout`. بعد از آن `Close` حرف آخر است.

## بدون حلقهٔ retry دستی

برخلاف postgres، اینجا حلقهٔ تلاش مجدد ننوشتیم: `nats.RetryOnFailedConnect(true)`
همان کار را می‌کند و بهتر از دوباره نوشتنش است. بدون آن، اولین dial ناموفق کشنده است
و broker ای که فقط کند بالا می‌آید، process را با خودش می‌برد.

ولی این گزینه یک دام دارد: `nats.Connect` با آن **بلافاصله و بدون خطا** برمی‌گردد در
حالی که هنوز وصل نشده. پس `Connect` باید خودش منتظر بماند — `waitReady` تا
`ConnectTimeout` صبر می‌کند و به ctx گوش می‌دهد.

## Ping چه چیزی را می‌زند

`js.AccountInfo(ctx)`، نه سوکت. دو حالت هست که «متصل» به نظر می‌رسند و هیچ‌کدام
نمی‌توانند پیام حمل کنند: اتصالی که هنوز در حال dial است، و broker ای که JetStream
روی حسابش خاموش است. سرویس با JetStream API حرف می‌زند، پس چک هم باید به همان برسد.
همان استدلالی که در `database` پشت `select 1` بود.

## registry

`registry.NATS` نوشته شد و `step` را با `ready: mq.Ping` و `close: mq.Drain` ثبت
می‌کند. اینجاست که ترتیب معکوس `Resources.Close` معنی پیدا می‌کند: nats بعد از
postgres باز می‌شود، پس قبل از آن drain می‌شود — وگرنه handler ای که دارد تمام
می‌شود، pool را از زیر پایش کشیده‌ایم.

# Files Changed

- `internal/infra/messagequeue/nats.go` *(تازه — `Config`، `validate`، `NATS` با `New`/`Connect`/`Ping`/`Drain`/`Conn`/`JetStream` و متدهای خصوصی `waitReady` و `redact`)*
- `internal/infra/messagequeue/const.go` *(تازه — `reconnectForever`)*
- `internal/infra/messagequeue/nats_test.go` *(تازه — نه تست)*
- `internal/registry/mq.go` *(تازه — `NATS(ctx, settings.MQ, *Resources)`)*
- `internal/registry/registry_test.go` *(یک تست برای رد شدن URL خالی)*
- `internal/config/settings/mq.go` *(چهار کلید تازه و دو `Check`)*
- `.env.example`, `docs/CONFIG.md` *(همان کلیدها)*
- `go.mod`, `go.sum` *(`github.com/nats-io/nats.go`)*

# Tests Run

- `make prepush` — fmt-check، govet، arch-check، golangci-lint و `go test -race ./...` همه پاس

# Follow-ups / Risks

- `nats.Name(...)` نوشته نشد. اسم اتصال روی سرور موقع دیباگ به کار می‌آید («کدام
  باینری این subscription را دارد؟»)، ولی پر کردنش یعنی `registry` باید بداند کدام
  باینری است، و امروز نمی‌داند. `settings.App` هم چنین میدانی ندارد.
- stream و consumer ها هنوز ساخته نشده‌اند. `internal/adapter/mq/nats/` خالی است، و
  تصمیم `Nats-Msg-Id` هم هنوز گرفته نشده.
- تست‌ها همه بدون broker اجرا می‌شوند. تست واقعی JetStream یک integration test است و
  جایش `tests/integration/` است، نه اینجا.

# Instruction

«برویم messagequeue» — با سه شرطی که قبل از نوشتن مطرح و تأیید شد: مرز infra و
adapter طوری باشد که stream و subject به infra نشت نکنند، خاموشی با `Drain` باشد نه
`Close`، و حلقهٔ retry دستی نوشته نشود چون کتابخانهٔ nats خودش دارد.
