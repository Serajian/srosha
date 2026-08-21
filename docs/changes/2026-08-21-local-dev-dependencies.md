Branch: `feat/bootstrap`

# Summary

بالا آوردن سرویس روی ماشین محلی، بدون اینکه هر بار دستورهای docker را از حفظ بزنی.

## compose

`deployment/app/docker-compose.dev.yml` — فقط وابستگی‌ها. باینری‌ها روی خود ماشین با
`make run-gateway` اجرا می‌شوند.

عمداً یک فایل **جدا** از `docker-compose.yml` است. آن یکی استقرار است، هیچ پورتی
publish نمی‌کند، و در branch مخصوص خودش نوشته می‌شود. اگر همان را برای dev هم به کار
می‌بردیم، بعداً بی‌صدا معنی‌اش عوض می‌شد.

| | |
| --- | --- |
| postgres | `127.0.0.1:7001` → 5432، `postgres:18-alpine` |
| nats | `127.0.0.1:7002` → 4222، `nats:2.14-alpine`، با `-js` |

دو نکته که در خود فایل هم کامنت شده‌اند:

- **پورت‌ها روی loopback اند.** این تنها جایی است که `ports:` استفاده می‌شود، و
  فقط چون `go run` روی همین ماشین باید برسد. `docs/CONFIG.md` می‌گوید هرگز
  `ports:`؛ آن قانون برای استقرار است و استثنایش همین‌جا ثبت شد.
- **`-js` اختیاری نیست.** بدون JetStream، nats وصل می‌شود و ping ساده هم جواب
  می‌دهد، ولی `AccountInfo` شکست می‌خورد — که دقیقاً همان چیزی است که `Ping` ما
  می‌زند و دقیقاً دلیلی که آن را به‌جای ping ساده نوشتیم.

image nats به `2.14-alpine` قفل شد تا با سرور یکی باشد، و همان healthcheck
(`8222/healthz`) گذاشته شد. یک broker محلی روی minor دیگر، broker ای است که می‌تواند
با production اختلاف داشته باشد.

## دستورهای make

گروه تازهٔ `dev-*`. عمداً `docker-*` را دست نزدم: آن‌ها به فایل استقرار اشاره
می‌کنند و اگر به فایل dev وصلشان می‌کردم بعداً معنی‌شان بی‌صدا عوض می‌شد.

`dev-up` فقط `up -d` نمی‌زند؛ **منتظر می‌ماند تا هر دو `healthy` بدهند**، وگرنه
بلافاصله بعدش `go run` می‌خورد به postgres ای که هنوز بالا نیامده.

`dev-ready` از هر باینری می‌پرسد آماده است یا نه، و پورت‌ها را از `.env.gateway` و
`.env.dispatcher` می‌خواند تا اگر عوضشان کردی دنبال کند.

## و `make run-*` دیگر `go run` نمی‌زند

`go run` وقتی بچه‌اش سیگنال می‌گیرد خودش یک برمی‌گرداند، پس یک Ctrl-C تمیز به شکل
`make: *** [run-gateway] Error 1` در می‌آمد در حالی که سرویس صفر بیرون آمده بود.
حالا build می‌کند و خود باینری را اجرا می‌کند، پس exit code مال سرویس است.

# Files Changed

- `deployment/app/docker-compose.dev.yml` *(تازه — postgres و nats)*
- `Makefile` *(متغیر `DOCKER_COMPOSE_DEV`، پورت‌های سلامت از env، هفت هدف `dev-*`، و `run-*` که باینری ساخته‌شده را اجرا می‌کند)*
- `docs/CONFIG.md` *(بخش «Local development»)*

# Tests Run

- `make dev-up` روی یک محیط تمیز: هر دو کانتینر `healthy`
- هر دو باینری اجرا شدند و `make dev-ready` این را داد:
  `gateway {"binary":"gateway","status":"ready",...}` و همان برای dispatcher
- `make prepush` — همه پاس

# Follow-ups / Risks

- **nats محلی احراز هویت ندارد.** سرور `nats-server.conf` دارد با یک کاربر برای هر
  باینری و permission جدا. یعنی اگر permission را اشتباه بنویسیم، محلی کار می‌کند و
  فقط روی سرور می‌شکند. با گرفتن آن فایل بسته می‌شود.
- `Dockerfile` هنوز نوشته نشده، پس خود srosha داخل compose نیست و
  `make docker-build` شکست می‌خورد. مال branch استقرار است.
- migration ها هنوز نیستند. امروز لازم نبودند چون کد فقط `select 1` می‌زند.

# Instruction

«یک docker compose بنویس، توش postgres باشد، فعلاً app بالا بیاید؛ بعد در branch
مخصوص deployment compose را درست می‌کنیم.» و بعدش: «nats هم مثل postgres یک image
بالا بیاور» — چون nats سرور از بیرون در دسترس نیست. و: «دستورات make را درست کن که
خودم بتوانم انجام بدهم.»
