Branch: `fix/compose-at-the-project-root`

# Summary

`deployment/app/docker-compose.yml` به `docker-compose.yml` در **ریشهٔ مخزن**
منتقل شد، و `context: ../..` شد `context: .`.

# چرا

Dokploy دستورِ deploy را عوض کرد. قبلاً:

```
docker compose -p srosha-app-lj3hqn -f deployment/app/docker-compose.yml up …
```

حالا:

```
docker compose -p srosha-app-lj3hqn \
  --project-directory /etc/dokploy/compose/srosha-app-lj3hqn/code \
  -f deployment/app/docker-compose.yml up …
```

آن `--project-directory` مبنای مسیرهای نسبی است. و **دو چیز** را با هم شکست:

**۱. build context.** `../..` قبلاً از `deployment/app/` حساب می‌شد و به ریشهٔ
مخزن می‌رسید. حالا از ریشهٔ مخزن حساب می‌شود و بیرونِ checkout می‌افتد:

```
resolve : lstat /etc/dokploy/compose/deployment: no such file or directory
```

**۲. جایگزینیِ `${...}`.** compose آن را فقط از `.env` ــِ داخلِ project
directory می‌خواند، و Dokploy آن فایل را **کنارِ compose** می‌سازد. با فایلی که
یک سطح پایین‌تر است، این دو پوشهٔ متفاوت‌اند — و همهٔ ۲۸ متغیر رشتهٔ خالی شدند:

```
warning msg="The \"NOTIF_DB_DSN\" variable is not set. Defaulting to a blank string."
```

# چرا این راه‌حل و نه راهِ ساده‌تر

راهِ ظاهراً ساده‌تر این بود که `env_file` بگذاریم به‌جای `${...}`. دو دلیل رد شد:

**جایگزینی و `env_file` یک چیز نیستند.** اولی در زمانِ تجزیهٔ فایل انجام
می‌شود، دومی متغیرها را داخلِ container می‌ریزد. ما به اولی نیاز داریم، چون یک
نامِ برنامه باید در دو سرویس دو مقدار داشته باشد:

```yaml
gateway:     NOTIF_MQ_URL: ${NOTIF_GATEWAY_MQ_URL}
dispatcher:  NOTIF_MQ_URL: ${NOTIF_DISPATCHER_MQ_URL}
```

**و `env_file` جداییِ رازها را می‌شکند.** هر container کلِ فایل را می‌گیرد، یعنی
gateway توکن‌های فرستندهٔ dispatcher را هم می‌بیند. `docs/CONFIG.md` صریح
می‌گوید gateway نباید آن‌ها را داشته باشد.

بردنِ فایل به ریشه تنها گزینه‌ای بود که هر دو را نگه می‌دارد: پوشهٔ compose،
project directory، و جایی که `.env` ساخته می‌شود، همه یکی می‌شوند — همان چیزی
که خودِ compose به‌طور پیش‌فرض فرض می‌کند.

# چیزی که نزدیک بود جا بیفتد

**Watch paths.** با رفتنِ فایل به ریشه، تغییرش دیگر زیرِ `deployment/app/**`
نیست و auto-deploy راه نمی‌افتاد. `docker-compose.yml` به فهرست اضافه شد.

و همان‌جا دو کهنگیِ دیگر هم اصلاح شد که از قبل در فهرست بودند: `gen/**` که
هرگز وجود نداشته (buf در `sdk/go` تولید می‌کند) و نبودنِ `migrations/**` که حالا
با `go:embed` داخلِ باینری می‌رود و باید deploy را راه بیندازد.

# Files Changed

- `docker-compose.yml` *(منتقل‌شده از `deployment/app/`؛ `context` و توضیحِ دلیل در خودِ فایل)*
- `deployment/app/docker-compose.dev.yml` *(کامنتش می‌گوید آن یکی کجاست)*
- `Makefile` *(`DOCKER_COMPOSE`)*
- `docs/CONFIG.md` *(مسیر، دلیلش، و watch paths)*

# Tests Run

- `docker compose -f docker-compose.yml config -q` — معتبر
- context ــِ resolve شده برای هر چهار سرویس: ریشهٔ مخزن
- **شبیه‌سازیِ دقیقِ دستورِ سرور** با `--project-directory`: هر دو
  `NOTIF_MQ_URL` جدا و درست پر شدند — یعنی جداییِ رازها سرِ جایش است
- بدونِ `--env-file`، یعنی همان بارگذاریِ خودکار: متغیرها از `.env` ــِ ریشه پر
  شدند. مکانیزمی که روی سرور لازم است، اینجا ثابت شد.
- `make precommit` — pass

# Follow-ups / Risks

- **در Dokploy باید Compose Path به `docker-compose.yml` عوض شود.** بدونِ آن،
  deploy همان فایلِ قدیمی را می‌گردد و پیدا نمی‌کند.
- `docs/reference/srosha-infra-deploy.md` هنوز مسیرِ قدیمی را نشان می‌دهد. آن
  سند «facts about the running system» است و بعد از اولین deploy موفق باید
  به‌روز شود — با تأییدِ مالک، چون سندِ اوست.
- اگر Dokploy روزی `--project-directory` را بردارد، این دوباره می‌شکند — این
  بار برعکس. کامنتِ بالای فایل می‌گوید چرا آنجاست، تا کسی که آن روز نگاهش
  می‌کند بداند چه چیزی را نباید «مرتب» کند.

# Instruction

deploy با خطای build context شکست و همهٔ متغیرها خالی شدند. علت یک تغییر در
خودِ Dokploy بود. مالک گفت فایل به ریشه منتقل شود.
