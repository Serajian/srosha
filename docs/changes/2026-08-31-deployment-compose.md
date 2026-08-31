Branch: `feat/deployment-stack`

# Summary

`deployment/app/docker-compose.yml` نوشته شد. task سه از چهار.

**چهار سرویس و نه بیشتر:** `gateway`، `dispatcher`، `console`، و `migrate` که
زیرِ profile است. `postgres` و `nats` **عمداً نیستند** — طبقِ
`docs/reference/srosha-infra-deploy.md` بخشِ ۲، آن دو از قبل هرکدام سرویسِ
Dokploy ــِ خودشان‌اند و «verified working». تعریفشان اینجا یعنی یک دیتابیسِ
دوم با volume ــِ خالی، کنارِ دیتابیسِ واقعی. نسخهٔ اولِ plan همین اشتباه را
داشت و قبل از نوشتنِ فایل اصلاح شد.

**Trap 1.** وقتی domain به یک compose service وصل می‌شود، Dokploy شبکهٔ سرویس را
با `dokploy-network` **جایگزین** می‌کند نه اضافه. سرویسی که فقط `srosha-net`
دارد، لحظه‌ای که domain بگیرد دیتابیس و broker اش را از دست می‌دهد — بدونِ هیچ
خطای deploy، فقط شکستِ زمانِ اجرا. پس `gateway` و `console` هر دو شبکه را صریح
دارند. برای console حیاتی‌تر است، چون **دو** domain می‌گیرد.

**سه router روی دو container.** `api.srosha.ir` روی gateway با `scheme=h2c` —
اجباری، چون gRPC پشتِ terminator همان HTTP/2 بدونِ TLS است و Traefik وگرنه
HTTP/1.1 حرف می‌زند. و `panel.srosha.ir` و `admin.srosha.ir` هر دو روی console،
که دو listener در یک process اند.

**دو `NOTIF_MQ_URL` جدا** از `${NOTIF_GATEWAY_MQ_URL}` و
`${NOTIF_DISPATCHER_MQ_URL}`. یکی نشدند: هر باینری کاربرِ NATS ــِ خودش را دارد
و طبقِ همان سند، یک credential ــِ gateway نمی‌تواند notification های کسی را
بخواند.

**`env_file` استفاده نشد.** نگاشتِ صریحِ `${...}` است، چون `.env` ــِ git-ignored
روی سرور وجود ندارد؛ مقادیر از Environment tab ــِ Dokploy می‌آیند.

`logging` با `max-size: 10m` و `max-file: 3` روی **هر** سرویس، حتی migrate.
لاگِ json ــِ بی‌حد رایج‌ترین راهی است که یک هاستِ کوچک دیسکش پر می‌شود، و وقتی
دیسک پر شود postgres هم با بقیه می‌میرد. سقفِ حافظه ۵۱۲M روی هر باینری.

# یک چیزِ ریز که موقعِ نوشتن گیر افتاد

پیش‌فرضِ `NOTIF_HTTP_ADDR` برابرِ `:8081` است، یعنی **مالِ dispatcher**. console
هم همان کلید را می‌خواند. اگر صریح داده نشود، health ــِ console روی پورتِ
dispatcher می‌نشیند. در compose از `${NOTIF_CONSOLE_HTTP_ADDR}` می‌آید و در
task 4 در CONFIG.md ثبت می‌شود.

# Files Changed

- `deployment/app/docker-compose.yml` *(جدید)*

# Tests Run

- `docker compose config -q` — معتبر. هشدارها فقط دربارهٔ متغیرهای ست‌نشده‌اند،
  که درست است: مقادیر در مخزن نیستند.
- شمارشِ سرویس‌ها: **سه** بدونِ profile، **چهار** با آن. اگر `postgres` یا
  `nats` ظاهر می‌شد، این فایل نسخهٔ دومی از زیرساختِ زنده را deploy می‌کرد.
- `grep 'ports:'` → صفر. `grep 'env_file'` → صفر.
- از روی JSON ــِ resolved شده خوانده شد (نه از روی متنِ فایل):
  - `gateway` و `console`: هر دو شبکه. `dispatcher` و `migrate`: فقط `srosha-net`.
  - هر سه باینری: `mem=536870912`، `log=10m`، و healthcheck ای که خودِ باینری را
    صدا می‌زند.
  - هر دو شبکه `external: true`.
- `docker compose build gateway` — موفق.

# Follow-ups / Risks

- **آنچه اینجا ثابت نشد:** stack بالا نیامد. سرویس‌ها روی این ماشین اجرا
  نمی‌شوند — کلیدهای واقعیِ crypto، اعتبارنامه‌های SMTP و NATS، و broker ای که
  روی سرور است را ندارند. چیزی که ثابت شد این است که فایل معتبر است، شبکه‌ها و
  محدودیت‌ها و healthcheck ها همان‌اند که باید، و image از طریقِ خودِ compose
  ساخته می‌شود. نه بیشتر.
- چهار نامِ تازه وارد شدند که دادهٔ مخزن‌اند و هنوز در `docs/CONFIG.md` نیستند:
  `NOTIF_GATEWAY_MQ_URL`، `NOTIF_DISPATCHER_MQ_URL`،
  `NOTIF_DISPATCHER_HTTP_ADDR`، `NOTIF_CONSOLE_HTTP_ADDR`. این‌ها کلیدِ
  برنامه نیستند، نامِ جایگزینی در compose اند. task 4 ثبتشان می‌کند.
- `nats-server.conf` هنوز در مخزن نیست و با Dokploy mount می‌شود. این compose
  به آن دست نمی‌زند، ولی broker ای که به آن وابسته است از همان‌جا کاربرانش را
  می‌گیرد.

# Instruction

task سه از plan ــِ deployment: compose ای که فقط اپ را deploy کند، به
زیرساختِ موجود وصل شود، و `dokploy-network` را فقط به سرویس‌هایی بدهد که domain
دارند.
