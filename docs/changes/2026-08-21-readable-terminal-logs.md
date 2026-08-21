Branch: `feat/bootstrap`

# Summary

لاگ روی ترمینال خوانا نبود. یک خط JSON کامل در یک ترمینال باریک سه بار wrap می‌شد:

```
{"time":"2026-08-21T21:16:21.948536+03:30","level":"INFO","msg":"http listening","service":"srosha","binary":"gateway","addr":"[::]:8080"}
```

حالا:

```
time=21:19:16.543 level=INFO msg="http listening" addr=[::]:8080
```

سه تغییر، هر سه کوچک.

## پیش‌فرض قالب حالا به محیط بستگی دارد

`NOTIF_TELEMETRY_LOG_FORMAT` از قبل بود، ولی پیش‌فرضش `json` بود و کسی یادش نمی‌ماند
عوضش کند. حالا `LoadTelemetry(r, production)` — همان الگویی که `LoadWebhook` از قبل
داشت: JSON در production که collector پارسش می‌کند، متن هر جای دیگر که یک آدم
می‌خواندش. تنظیم صریح همچنان برنده است.

در `.env.example` آن خط comment شد، وگرنه هر کسی که کپی‌اش کند دوباره در همین دام
می‌افتد.

## قالب متن چیزی را که فقط ماشین لازم دارد نمی‌نویسد

`service` و `binary` از خروجی متن حذف شدند و timestamp فقط ساعت شد. آن دو صفت وجود
دارند تا یک collector بتواند دو process را از هم جدا کند؛ کسی که ترمینال را نگاه
می‌کند می‌داند کدام باینری را بالا آورده و امروز چه روزی است.

یعنی دو قالب **عمداً** فیلدهای متفاوتی حمل می‌کنند، و در کد نوشته شد چرا: JSON
**رکورد** است، متن یک **نما** از آن. با `NOTIF_APP_ENV=production` امتحان شد و JSON
دقیقاً مثل قبل است.

## و `make run-*`

آن `make: *** [run-gateway] Error 1` بعد از یک Ctrl-A تمیز مال `go run` بود که وقتی
بچه‌اش سیگنال می‌گیرد یک برمی‌گرداند. در commit قبلی درست شد؛ اینجا فقط ثبت می‌شود که
دو ایراد یک اسکرین‌شات بودند.

# Files Changed

- `internal/config/settings/telemetry.go` *(`LoadTelemetry` حالا `production` می‌گیرد)*
- `internal/config/gateway.go`, `dispatcher.go` *(هر دو `app.IsProduction()` را می‌دهند)*
- `internal/infra/telemetry/logger.go` *(`forAPerson` روی handler متنی)*
- `internal/infra/telemetry/const.go` *(`attrService`، `attrBinary`، `clockOnly`)*
- `internal/infra/telemetry/logger_test.go` *(تست قبلی برعکس شد؛ یکی تازه برای شکل ساعت)*
- `.env.example` *(کلید قالب comment شد با توضیح)*

# Tests Run

- `make prepush` — همه پاس
- اجرای واقعی در هر دو حالت: بدون `APP_ENV` خروجی متنیِ کوتاه، و با
  `NOTIF_APP_ENV=production` همان JSON قبلی با هر دو صفت سر جایشان

# Follow-ups / Risks

- `TestEveryLineNamesTheServiceAndTheBinary` روی JSON است و باید همان‌جا بماند —
  آن دو صفت در رکورد اجباری‌اند، فقط در نما نیستند.
- اگر روزی خروجی متنی جایی جز ترمینال برود، این تصمیم باید بازبینی شود.

# Instruction

«لاگ‌ها خیلی توی هم و بد است، می‌شود بهترش کرد؟» — به‌همراه اسکرین‌شاتی که هم JSON
پیچیده در ترمینال را نشان می‌داد و هم `make: *** [run-gateway] Error 1` را.
