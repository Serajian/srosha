Branch: `chore/migrate-server-target`

# Summary

یک target تازه: `make migrate-server`، برای اجرای migration **روی سرور**.

```make
migrate-server:
	@test -f $(DOCKER_DIR)/.env || { ... exit 1; }
	@docker compose -f $(DOCKER_COMPOSE) --profile migrate run --rm migrate
```

# چرا `migrate-up` روی سرور کار نمی‌کند

دو دلیل، و دومی قطعی است:

۱. `migrate-up` یک `goose` ــِ نصب‌شده روی خودِ ماشین می‌خواهد. سرور نه goose
   دارد نه Go — کلِ دلیلِ گذاشتنِ goose داخلِ image همین بود.

۲. **از host به دیتابیس نمی‌رسی.** postgres ــِ deploy شده هیچ `ports:` ای
   ندارد، و `postgres:5432` نامی است که فقط داخلِ `srosha-net` معنی دارد. یک
   دستور روی host آن نام را اصلاً resolve نمی‌کند.

این توضیح به‌شکلِ یک کامنت بالای متغیرهای migration در خودِ Makefile هم نوشته
شد، تا کسی که فقط آنجا را می‌خواند هم بداند کدام target مالِ کجاست.

# دو ریزه‌کاری

**`--env-file` عمداً داده نشده.** target های `docker-*` ــِ موجود
`--env-file .env` می‌دهند که ریشهٔ مخزن است. روی سرور آن فایل وجود ندارد:
Dokploy فایل را **کنارِ compose** می‌سازد (`deployment/app/.env`)، و خودِ
docker compose آن را از همان‌جا برمی‌دارد. دادنِ `--env-file` این را می‌شکست.

**یک نگهبان.** بدونِ آن، اجرای اشتباهی روی لپ‌تاپ به یک خطای گنگ از goose
می‌رسید. حالا می‌گوید کجا باید اجرا شود و پیشنهاد می‌دهد `make migrate-up`.

# و یک کامنتِ کهنه که سرِ راه بود

`docker-build` می‌گفت «the single image that carries **both** binaries» و
پیامش «gateway/dispatcher are selected by command». سه باینری است. اصلاح شد.

# Files Changed

- `Makefile` *(target تازه، کامنتِ بالای متغیرهای migration، و دو جملهٔ کهنه در `docker-build`)*

# Tests Run

- `make help` — target در فهرست دیده می‌شود
- بدونِ `deployment/app/.env`: نگهبان می‌گیرد و پیامِ درست می‌دهد، `exit=2`
- با یک `.env` ــِ ساختگی: container روی `srosha-net` بالا آمد، goose اجرا شد و
  فقط روی hostname ــِ جعلی مرد —
  `lookup nowhere on 127.0.0.11:53: no such host`. یعنی کلِ مسیر کار می‌کند و
  تنها چیزِ غلط همان مقداری بود که عمداً غلط دادم. فایلِ ساختگی پاک شد.
- `git check-ignore` تأیید کرد `deployment/app/.env` ignored است، پس آن فایل
  هرگز commit نمی‌شود.
- `make precommit` — pass

# Follow-ups / Risks

- `make` روی سرور ممکن است نصب نباشد. اگر نبود، همان دستورِ داخلِ target را
  دستی بزن؛ target فقط کوتاه‌نویسی است، نه چیزی که سرور به آن وابسته شود.
- target های `docker-up` و `docker-down` روی compose ــِ deploy شده اجرا
  می‌شوند و از لپ‌تاپ عملاً بی‌معنی‌اند (شبکه‌های external، بدونِ راز). دست
  نخوردند چون از قبل بوده‌اند و این commit دربارهٔ migration است.

# Instruction

مالک گفت همان target ای که پیشنهاد داده بودم نوشته شود تا به‌جای دستورِ بلندِ
docker compose بتواند `make` بزند.
