Branch: `refactor/domain-services`

# Summary

سه متد در `delivery.Service` اسم گویاتری گرفتند.

```
Open    → Create
Sent    → RecordSent
Failed  → RecordFailure
```

## `Open` چیزی نمی‌گفت

«باز کردن» چه چیزی؟ عملیات واقعاً همان است که هست: یک delivery به ازای هر گیرنده
می‌سازد و ذخیره می‌کند. اختراع فعلی مثل `Plan` یا `Accept` معنایی اضافه نمی‌کرد و
فقط باید یاد گرفته می‌شد.

و با `notification.Service.Create` یکدست شد، پس دو خط پشت سر هم در `Submit` یک
الگو دارند:

```go
n, err  = s.notifs.Create(ctx, origin(src), request(cmd))
ds, err = s.deliveries.Create(ctx, n.ID, recipients, cmd.Senders)
```

تضادی با قاعدهٔ «repository زبان CRUD، service زبان کسب‌وکار» ندارد: متد repository
اش `CreateByList` است و این یکی اعتبارسنجی مجموعه را هم انجام می‌دهد.

## `Sent` و `Failed` صفت بودند نه فعل

`deliveries.Sent(...)` بیشتر شبیه یک سؤال خوانده می‌شد تا یک دستور.

و آنچه این متدها می‌کنند **ثبت نتیجه** است نه انجام کار — ارسال قبلاً اتفاق افتاده.
`RecordSent` این را می‌گوید.

کامنت‌ها هم با اسم‌ها هماهنگ شدند، و یکی از آن‌ها چیزی را ثبت می‌کند که راحت
فراموش می‌شود:

> A transient one is not recorded at all: the delivery stays pending and the
> broker retries it.

# Files Changed

- `internal/core/domain/delivery/service.go`
- `internal/core/usecase/submit.go` *(محل صدازدن)*

# Tests Run

- `make prepush` — سبز

# Follow-ups / Risks

- `RecordSent` و `RecordFailure` هنوز از هیچ‌جا صدا زده نمی‌شوند؛ منتظر `dispatch`
  اند. برای همین هیچ تستی با این تغییر نشکست.

# Instruction

مالک گفت `Open` اسم گویایی نیست. `Sent` و `Failed` هم به همان دلیل بررسی و عوض
شدند.
