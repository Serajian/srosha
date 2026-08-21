Branch: `refactor/domain-services`

# Summary

`shared.Channel`، `delivery.Status` و `delivery.FailureReason` هم مثل `Priority`
موقع سریال‌شدن اعتبارسنجی می‌شوند — در **هر دو** جهت.

## مسئله

هر سه تایپ رشته‌ای‌اند، پس از قبل با متن سریال می‌شدند و به‌نظر درست می‌آمدند. ولی
هیچ‌کدام موقع خواندن چک نمی‌شدند:

```json
{ "channel": "carrier-pigeon" }
```

بی‌صدا قبول می‌شد و تبدیل می‌شد به `Channel` ای که هر `switch` در پایین‌دست باید
حدسش را می‌زد. همان چیزی که `default` در `ValidateAddress` برای بلندصدا شکستنش
گذاشته شده بود — ولی از این مسیر دورش می‌زد.

## راه‌حل

`MarshalJSON` و `UnmarshalJSON` روی هر سه.

`Status` و `FailureReason` هر دو `~string` با متد `Valid()` اند، پس یک تابع generic
هر دو را می‌گیرد:

```go
func decode[T interface{ ~string; Valid() bool }](b []byte, msg string, sentinel error) (T, error)
```

`Channel` از آن استفاده نمی‌کند چون `ParseChannel` از قبل خطای دقیق‌تری می‌دهد.

## نوشتن هم چک می‌شود، نه فقط خواندن

اگر مقداری از یک باگ به marshal برسد، به‌جای اینکه چیزی روی سیم برود که هیچ
خواننده‌ای قبولش نمی‌کند، همان‌جا خطا می‌دهد. `ErrInternal` است چون فقط کد خودمان
می‌تواند تولیدش کند.

## `FailureReason` هم اضافه شد

خواسته نشده بود، ولی در همان فایل `Status` است و دقیقاً همان الگو. نداشتنش فایل را
نیمه‌کاره می‌کرد.

# Files Changed

- `internal/core/shared/channel.go`، `channel_test.go`
- `internal/core/domain/delivery/status.go`، `status_test.go`، `errors.go`

# Tests Run

- `make prepush` — سبز
- رفت‌وبرگشت هر مقدار معتبر، و ردهای `"EMAIL"`، `"sent"`، `"DELIVERED"`،
  `"carrier-pigeon"`، `""`، `1`، `null`

# Follow-ups / Risks

- `webhook.Result.Reason` از نوع `string` ساده است نه `FailureReason`، چون
  `webhook` نباید `delivery` را import کند. پس marshaller تازه در مسیر callback
  استفاده نمی‌شود؛ usecase مقدار را با `.String()` تبدیل می‌کند و همان‌جا از قبل
  معتبر است.
- `shared.ID` عمداً اعتبارسنجی نگرفت. شناسه‌ای که موقع نوشتن معتبر بوده باید بعد از
  سخت‌تر شدن قاعده هم قابل بارگذاری بماند — همان استدلال `Restore`.
- lint یک غلط املایی گرفت (`recognise` در برابر `recognize`)، چون locale روی `US`
  است. یعنی آن تنظیم دارد کار می‌کند.

# Instruction

مالک خواست `Channel` و `Status` هم مثل `Priority` اعتبارسنجی شوند.
