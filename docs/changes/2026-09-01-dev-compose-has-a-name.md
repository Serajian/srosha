Branch: `chore/dev-compose-has-a-name`

# Summary

`deployment/app/docker-compose.dev.yml` یک خطِ `name: srosha-dev` گرفت. **هیچ کدی
عوض نشده و هیچ سرویسی رفتارش فرق نکرده.**

## مسئله

compose وقتی نامی نداشته باشد، نامِ پروژه را از **پوشه‌ای که فایل در آن است**
برمی‌دارد. آن پوشه `deployment/app/` است، پس نامِ پروژه می‌شد `app`:

```
$ docker compose ls
NAME       STATUS        CONFIG FILES
app        running(2)    …/srosha/deployment/app/docker-compose.dev.yml
kharazmi   running(7)
mercury    running(3)
odin       running(7)
wishly     running(8)
```

روی ماشینی که پنج stack ــِ دیگر دارد، `app` هیچ‌چیز نمی‌گوید. و ولوم‌ها هم همان نام
را می‌گرفتند — `app_pgdata` و `app_natsdata` — یعنی جایی که هیچ‌کس دنبالشان نمی‌گردد.

نام در خودِ فایل نشست و نه به‌شکلِ `-p` روی دستور، تا هر راهِ ورودی یکی بگوید: هدف‌های
Makefile، `docker compose ls`، و یک `docker compose -f …` که کسی دستی تایپ می‌کند.

## اعمالش ترتیب داشت

عوض شدنِ نامِ پروژه، ولوم‌های قبلی را **یتیم** می‌کند نه اینکه منتقلشان کند. و بدتر:
از لحظه‌ای که فایل نام می‌گیرد، `make dev-down` دیگر چیزی برای پایین آوردن پیدا
نمی‌کند، چون دنبالِ پروژه‌ای به نامِ تازه می‌گردد و کانتینرهای در حالِ اجرا زیرِ نامِ
قدیمی ثبت‌اند.

پس ترتیبِ درست این بود و همین انجام شد:

```bash
docker compose -f … -p app down -v     # با نامِ قدیمی، وگرنه یتیم می‌شوند
docker compose -f … up -d --wait       # با نامِ تازه
```

قبل از `-v` نگاه شد که چه از دست می‌رود: پایگاه دادهٔ محلی **هیچ ردیفِ زنده‌ای**
نداشت و JetStream یک stream با صفر پیام. چیزی نبود.

# Files Changed

- `deployment/app/docker-compose.dev.yml` *(یک خطِ `name:`، و کامنتی که می‌گوید چرا در فایل و نه روی دستور)*

# Tests Run

- `docker compose -f … config` — valid، و `name: srosha-dev` در خروجی
- پایین و بالا آورده شد: هر دو کانتینر healthy، ولوم‌ها `srosha-dev_*`، و هیچ
  `app_*` ای باقی نماند
- `make precommit` — pass

# Follow-ups / Risks

- **پایگاه دادهٔ محلی خالی است.** `make migrate-up` schema را برمی‌گرداند. این هزینهٔ
  یک‌بارهٔ عوض کردنِ نام است و دوباره تکرار نمی‌شود.
- این فایل هیچ image ای نمی‌سازد — `postgres` و `nats` را از بالادست می‌کشد. آن
  image های `<none>` که روی این ماشین جمع شده بودند مالِ `make docker-build` اند، نه
  اینجا. ارزش دارد کسی که دنبالِ منشأشان می‌گردد این را بداند.

# Instruction

compose ــِ dev را تمیز کن و بهش اسم بده، بعد پایین بیاور و دوباره تمیز بالا بیاور.
