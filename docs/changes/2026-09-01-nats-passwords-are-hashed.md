Branch: `fix/nats-passwords-are-hashed`

# Summary

سه متغیرِ `NATS_*_PASSWORD` از این به بعد **هشِ bcrypt** نگه می‌دارند نه رمز. خودِ
`nats-server.conf` عوض نشد — همان `$VAR` هاست — چیزی که عوض می‌شود مقداری است که در
آن متغیرها گذاشته می‌شود، و همین است که باید نوشته شود وگرنه نفرِ بعدی رمزِ خام
برمی‌گرداند. **هیچ کدِ Go ای عوض نشده.**

## چقدر واقعاً می‌ارزد

nats در هر بار start می‌گفت `Plaintext passwords detected, use nkeys or bcrypt`.
دستاوردش کمتر از آن است که آن هشدار القا می‌کند و باید صریح گفته شود:

**می‌برد:**

- رمزِ `admin` — هیچ باینری‌ای نگهش نمی‌دارد، پس بعد از این **هیچ نسخهٔ خامی از آن روی
  این ماشین نیست**. برای این یکی کامل است.
- env ــِ سرویسِ nats هش می‌شود، پس `docker inspect` روی آن کانتینر دیگر اعتبارنامهٔ
  کارآمد نمی‌دهد.
- هشداری که همیشه هست به آدم یاد می‌دهد هشدارها را نبیند.

**نمی‌برد:** رمزِ `gateway` و `dispatcher`. آن دو داخلِ URL احراز می‌شوند
(`settings/mq.go:11` — «URL is a secret: each binary connects as its own user,
password included»)، پس `NOTIF_GATEWAY_MQ_URL` رمزِ خام دارد و
`docker inspect <gateway>` هنوز یک کلیدِ کارآمد می‌دهد. bcrypt قفل را سفت می‌کند در
حالی که کلید کنارش افتاده.

**راه‌حلِ واقعی nkeys است** — سرور فقط کلیدِ عمومی دارد و کلاینت با seed یک چالش را
امضا می‌کند؛ هیچ رازِ مشترکی وجود ندارد. ولی آن یک تغییرِ کد است: `nats.Connect` در
`messagequeue/nats.go:106` باید گزینهٔ `nats.Nkey(...)` بگیرد، یک کلیدِ تازه در config
لازم است، و `NOTIF_MQ_URL` بخشِ userinfo اش را از دست می‌دهد. ثبت شد، انجام نشد.

## یک تلهٔ واقعی که وسطِ کار پیدا شد

هشِ bcrypt پر از `$` است، و `$` در فایلِ محیطیِ compose یعنی «متغیر». اندازه‌گیری شد:

```
گذاشته شد   $2a$11$ZrJMkGPv9rFsvd9K1C2SSOIo9DMU0tDBlIIzQxzvkYhOan5CI92sq
کانتینر گرفت $2a$11
```

از سومین `$` به بعد بریده شد. `env_file:` هم نجات نمی‌دهد — آن هم interpolate می‌شود،
تست شد.

تنها راهِ کارآمد **دوبل کردنِ هر `$`** موقعِ گذاشتن در Dokploy است:

```
$$2a$$11$$ZrJMkGPv9rFsvd9K1C2SSOIo9DMU0tDBlIIzQxzvkYhOan5CI92sq
```

این دقیقاً از خانوادهٔ همان `#` است که یک بار رمزی را در همین Dokploy برید. به بخشِ
«Password rules — learned the hard way» ــِ سند اضافه شد، چون آنجا جای این خانواده
است.

خوبی‌اش این است که خطایش **بلند** است: هشِ بریده دیگر bcrypt معتبر نیست، پس nats
همان `[WRN] Plaintext passwords detected` را می‌دهد و هیچ کلاینتی هم وصل نمی‌شود.

# Files Changed

- `deployment/infra/nats/nats-server.conf` *(سرصفحه‌ای که قرارداد و تلهٔ `$` را می‌گوید، و یادداشتی سرِ کاربرِ admin دربارهٔ اینکه چرا فقط آن یکی کامل محافظت می‌شود)*
- `deployment/infra/nats/docker-compose.yml` *(سرصفحهٔ تنظیماتِ Dokploy)*
- `deployment/infra/README.md`
- `docs/reference/srosha-infra-deploy.md` *(بخشِ Password rules)*

# Tests Run

کلِ روش از سرتاسر، با همان چیدمانِ Dokploy (`code/` کنارِ `files/`، با `.env`):

- کانتینر healthy شد
- `docker logs | grep -i plaintext` — **هیچ‌چی**
- `docker exec ... env` — هشِ کامل، unescape شده توسطِ compose
- هر سه کاربر با رمزِ **خام** احراز شدند: `gateway` و `dispatcher` با `stream ls`،
  `admin` با `server check connection`
- رمزِ غلط: `Authorization Violation`
- و جداگانه تأیید شد که بدونِ escape، کانتینر `$2a$11` می‌گیرد
- `make precommit` — pass

# Follow-ups / Risks

- **این هنوز روی سرور اعمال نشده.** سه هش باید ساخته و escape شوند و در تبِ
  Environment ــِ سرویسِ nats بنشینند، بعد Deploy. رمزهای خام عوض **نمی‌شوند** —
  همان‌هایی که در `NOTIF_*_MQ_URL` هستند سرِ جایشان می‌مانند.
- `nats server passwd` رمزِ کمتر از ۱۰ کاراکتر را رد می‌کند. در تست به این خوردم؛
  رمزهای واقعیِ ما از این بلندتراند.
- **nkeys کارِ اصلی است و این نیست.** تا وقتی gateway و dispatcher رمز را در URL
  حمل کنند، نصفِ مسئله سرِ جایش است.
- بعد از deploy، آن سه متغیر دیگر رمز نیستند و کسی که برای عیب‌یابی نگاهشان کند این
  را نمی‌داند مگر همین‌جا را بخواند. تنها دفاع همان یک خطِ log است.

# Instruction

از فهرستِ کارهای باقی‌مانده، موردِ چهارم: رمزهای nats را به bcrypt ببر.
