Branch: `feat/bootstrap`

# Summary

دو اصلاح در `internal/infra/messagequeue`، هر دو از اجرای واقعی سرویس پیدا شدند نه از
خواندن کد.

## خطا می‌گفت مهلت تمام شد، نمی‌گفت چرا

وقتی nats بالا نبود، این بیرون می‌آمد:

```
messagequeue: not reachable in 1s: messagequeue: context deadline exceeded
```

در حالی که postgres در همان وضعیت تمیز می‌گفت `connection refused`.

ریشه‌اش `RetryOnFailedConnect` است: خطای dial را داخل retry خودِ کتابخانه می‌بلعد،
پس `Ping` فقط تمام شدن مهلت را می‌بیند. اولین حدس — خواندن `conn.LastError()` — جواب
نداد و در سورس nats معلوم شد چرا: هنگام اتصال **اولیه**، خطا از `ReconnectErrCB`
گزارش می‌شود و نه از `LastError` و نه از handler قطع اتصال.

پس `ReconnectErrHandler` ثبت شد و آخرین دلیل را نگه می‌دارد. callback روی goroutine
خودِ کتابخانه اجرا می‌شود، پس یک mutex لازم شد.

حالا:

```
messagequeue: not reachable in 1s: dial tcp 127.0.0.1:7002: connect: connection refused
```

این وقتی واقعاً مهم می‌شود که URL رمز اشتباه داشته باشد: فرق بین «مهلت تمام شد» و
`authorization violation` همان فرق بین ندانستن و دانستن است.

## WARN بی‌مورد در هر خاموشی تمیز

لاگ خاموشی این را داشت:

```
level=WARN msg="nats disconnected" err=<nil>
```

`DisconnectErrHandler` در یک drain عمدی هم صدا زده می‌شود، با خطای nil. هشدار دادن
دربارهٔ چیزی که خودمان خواسته‌ایم، یک خط اضافه در هر خاموشی تمیز است. حالا وقتی خطا
nil است برمی‌گردد.

# Files Changed

- `internal/infra/messagequeue/nats.go` *(`noteError`، `reason`، `unreachable`، ثبت `ReconnectErrHandler`، و رد شدن از خطای nil در `DisconnectErrHandler`)*

# Tests Run

- `make prepush` — همه پاس، از جمله race detector روی mutex تازه
- هر دو مورد در اجرای واقعی دیده شدند: پیام تازه با nats خاموش، و نبودن آن WARN در
  خاموشی با SIGTERM

# Follow-ups / Risks

- nats خطای `connection refused` را داخل خودش به `ErrNoServers` تبدیل می‌کند وقتی
  چند سرور در URL باشد. با یک سرور — که حالت ماست — متن اصلی می‌رسد.
- `reason` آخرین خطا را می‌دهد نه همهٔ خطاها. با یک سرور همان کافی است.

# Instruction

از اجرای سرویس بیرون آمد، نه از یک درخواست. حین بالا آوردن اپ محلی، پیام خطای nats
هیچ چیز قابل استفاده‌ای نمی‌گفت و آن WARN بی‌مورد در لاگ خاموشی دیده شد.
