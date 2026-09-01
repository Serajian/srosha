Branch: `docs/gotify-address-is-not-an-application-id`

# Summary

چهار جا دربارهٔ آدرسِ Gotify چیزِ غلطی می‌گفتند. اصلاح شدند. هیچ رفتاری عوض
نشده — فقط کامنت‌ها و سندها.

# چه چیزی غلط بود

همه‌جا نوشته شده بود آدرسِ Gotify «شناسهٔ application» است، یعنی چیزی که تعیین
می‌کند پیام کجا برود. و همه‌جا صادقانه علامت زده بود که این یک **فرضِ
تأییدنشده** است.

دیروز تأیید شد و غلط بود. یک Gotify ــِ استانداردِ ۲٫۶٫۳ با docker بالا آمد و
سه بار پرسیده شد:

| درخواست | جواب | کجا نشست |
| --- | --- | --- |
| `?token=…&appid=1` | ۲۰۰ | `appid=1` |
| `?token=…` بدونِ appid | ۲۰۰ | `appid=1` |
| `?token=…&appid=999` (وجود ندارد) | ۲۰۰ | `appid=1` |

**token است که application را انتخاب می‌کند.** آن پارامتر نادیده گرفته می‌شود.

# ولی برداشتنِ آدرس اشتباه می‌بود — و نزدیک بود این کار را بکنم

پیشنهادِ اولم این بود که چون آدرس «هیچ کاری نمی‌کند»، از مشتری نخواهیمش. قبل از
دست زدن به کد، این را دیدم:

```sql
CREATE UNIQUE INDEX deliveries_notification_channel_address_key
    ON deliveries (notification_id, channel, address);
```

اگر آدرسِ Gotify خالی می‌شد، دو مسیرِ Gotify در یک پیام همان کلید را می‌ساختند
و دومی **بی‌صدا** به‌عنوانِ تکراری حذف می‌شد:

```go
srosha.Gotify().From("ops"),      // address ""
srosha.Gotify().From("oncall"),   // همان کلید → گم می‌شود
```

دو application روی سرورِ مشتری، یکی‌شان بدونِ هیچ خطایی ناپدید.

**پس آدرس کاری می‌کند، فقط نه آنی که فکر می‌کردیم:** Gotify با آن مسیریابی
نمی‌کند، ولی srosha با آن دو تحویل را از هم تشخیص می‌دهد. سندی که فقط نیمهٔ
اول را بگوید، کسی را به همان اشتباهی می‌برد که خودم داشتم می‌کردم.

# چهار جا، هر دو نیمه

- `internal/core/shared/channel.go` — «شکل است نه مقصد» → «برای ما تفکیک
  می‌کند، برای Gotify آدرس نمی‌دهد»
- `internal/adapter/sender/gotify/sender.go` — `ASSUMPTION, unverified` →
  `VERIFIED`، با شرحِ همان سه درخواست
- `sdk/go/README.md` و `README.fa.md` — جدولِ آدرس‌ها و یادداشتِ زیرش
- `docs/CONFIG.md` — بندِ Gotify

در هر چهار جا **خودِ آزمایش** نوشته شد نه فقط نتیجه‌اش، تا کسی که فردا شک کند
مجبور نشود دوباره سرور بالا بیاورد.

# Files Changed

- `internal/core/shared/channel.go` *(کامنتِ اعتبارسنجی)*
- `internal/adapter/sender/gotify/sender.go` *(کامنتِ `endpoint`)*
- `sdk/go/README.md`, `sdk/go/README.fa.md`
- `docs/CONFIG.md`

# Tests Run

- `go build ./...` و `go test -count=1 ./...` — بدون شکست
- `make prepush` — pass

هیچ کدی عوض نشده، پس چیزی برای تست کردن اضافه نشد. آنچه ثابت شد دیروز مقابلِ
سرورِ واقعی ثابت شد.

# Follow-ups / Risks

- **آن duplicate guard حتی امروز هم برای Gotify کاملاً درست نیست.** مقصدِ
  واقعی، credential است نه آدرس، پس این دو هم برخورد می‌کنند:

  ```go
  srosha.GotifyTo("1").From("ops"),
  srosha.GotifyTo("1").From("oncall"),
  ```

  یک آدرس، دو هویت، دو application — و دومی حذف می‌شود. اشکالی است که از قبل
  بوده و کشفِ دیروز فقط نورش انداخت. درست کردنش یعنی نامِ فرستنده هم در آن
  index بیاید: یک migration و یک تصمیمِ جدا.
- `sdk/go` تگِ تازه‌ای نمی‌خواهد — فقط README عوض شده، نه کد.

# Instruction

مالک گفت موردِ یک از فهرستِ باز انجام شود: جاهایی که سند دربارهٔ Gotify غلط
می‌گوید. بعد گفت آن نکتهٔ آخر هم اصلاح شود.
