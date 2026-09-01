Branch: `docs/short-passwords-went-unnoticed`

# Summary

هنگامِ اعمالِ bcrypt روی سرور معلوم شد رمزهای NATS ــِ `gateway` و `dispatcher`
**هشت کاراکتر** بوده‌اند. هر دو عوض شدند. این گزارش ثبتش می‌کند و درسش را در سند
می‌نویسد. **هیچ کدی عوض نشده.**

## چطور پیدا شد

اتفاقی. حلقه‌ای که رمزهای فعلی را می‌خواند و هش می‌کرد، برای دو تا از سه تا خروجیِ
خالی داد:

```
NATS_GATEWAY_PASSWORD=
NATS_DISPATCHER_PASSWORD=
NATS_ADMIN_PASSWORD=$$2a$$11$$Y1.Dfvw...
```

`nats server passwd` رمزِ زیرِ ده کاراکتر را رد می‌کند. اندازه‌گیری تأیید کرد:

```
GATEWAY     length=8
DISPATCHER  length=8
ADMIN       length=22
```

## چیزی که این را از یک خطای پیکربندی جدی‌تر می‌کند

سندِ همین repository، سه پاراگراف بالاتر از جایی که این نوشته شد، می‌گوید رمز را با
`openssl rand -hex 24` بساز. آن قانون از قبل نوشته شده بود و رعایت نشده بود.

**و هیچ‌چیز نگرفتش.** نه تستی قرمز شد، نه هشداری آمد، نه خطی در log. رمزِ کوتاه دقیقاً
به همان اندازهٔ رمزِ بلند ساکت است. اگر امروز سراغِ bcrypt نرفته بودیم، همان‌طور
می‌ماند.

این همان الگویی است که امروز چند بار دیدیم — `.env.example` که دو سوم کلیدها را نداشت،
`CONFIG.md` که از نگهبانی حرف می‌زد که وجود نداشت، سندِ infra که سرورِ قبلی را توصیف
می‌کرد. هر بار یک انضباط بوده که مکانیزمی پشتش نبوده.

## چه کاری شد

هر دو رمز با `openssl rand -hex 24` عوض شدند. چون رمزِ خام در URL ــِ کلاینت زندگی
می‌کند، همان لحظه `NOTIF_GATEWAY_MQ_URL` و `NOTIF_DISPATCHER_MQ_URL` هم به‌روز شدند و
هر دو سرویس پشتِ سرِ هم deploy شدند.

بعدش، روی سرور:

```
gateway     healthy
dispatcher  healthy    consumer وصل
console     healthy
nats        healthy    grep -ci plaintext → 0
```

`hex` عمداً: فقط `[0-9a-f]`، پس نه `#` ای که Dokploy ببرد، نه `$` ای که compose
تفسیر کند، نه کاراکتری که در URL باید percent-encode شود. همان دلیلی که سند از اول
`rand -hex` را پیشنهاد کرده بود.

## آنچه به سند اضافه شد

یک پاراگراف زیرِ «Password rules — learned the hard way»، که می‌گوید قانون از قبل
بوده و رعایت نشده و هیچ‌چیز نگرفتش — و یک عادت که ارزانش تنها دفاعِ موجود است:

> Treat a tool refusing a credential as information, not as an obstacle to work
> around. And when you touch a password here, check its length while you are
> looking at it — that is the only inspection this rule gets.

# Files Changed

- `docs/reference/srosha-infra-deploy.md` *(پاراگرافِ تازه در Password rules)*

# Tests Run

- `make precommit` — pass
- خودِ رخداد روی سرور تأیید شد: طولِ هر سه رمز، و سلامتِ هر چهار کانتینر بعد از چرخش

# Follow-ups / Risks

- **هنوز هیچ‌چیز طولِ رمز را چک نمی‌کند.** این گزارش یک عادت اضافه کرد، نه یک مکانیزم.
  اگر روزی خواستیم واقعی‌اش کنیم، جایش `config` است: یک `r.Check` روی طولِ بخشِ رمزِ
  `NOTIF_MQ_URL` باعث می‌شود باینری با رمزِ کوتاه بالا نیاید. همان کاری که `arch-check`
  برای معماری کرد.
- رمزِ `POSTGRES_PASSWORD` و `NOTIF_CONSOLE_SMTP_PASSWORD` با همین دقت نگاه نشده‌اند.
- تعویضِ این دو رمز یک وقفهٔ یکی‌دو دقیقه‌ای داشت، چون رمز باید در دو سرویس هم‌زمان عوض
  شود. با nkeys این هزینه هم از بین می‌رود.

# Instruction

موقعِ اعمالِ bcrypt معلوم شد دو رمز هشت کاراکتری‌اند. عوضشان کن و ثبتش کن.
