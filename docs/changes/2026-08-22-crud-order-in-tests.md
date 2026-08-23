Branch: `feat/postgres-repositories`

# Summary

دنبالهٔ همان دستور «ترتیب همه را بر اساس CRUD بگذار»، این بار در `db/postgres`.

commit قبلی متدهای repository ها و statement های فایل‌های query را مرتب کرد ولی
**تست‌ها را جا انداخت**. دو فایل هنوز خارج از ترتیب بودند:

**`delivery_test.go`** — `TestTheSecondWorkerIsToldItLost` یک تست `Update` است ولی
دوم نشسته بود، وسط تست‌های خواندن:

```
قبل:  RoundTrip · SecondWorker · ListStale ×2 · Paging · Missing
                   └─ U وسط R ها
بعد:  RoundTrip · ListStale ×2 · Paging · Missing · SecondWorker
      C           R  R  R  R                        U
```

**`source_test.go`** — `TestChangingToTheStateItIsAlreadyIn` دو بار `Deactivate`
می‌زند، یعنی D این جدول، و قبل از `TestUpdatingASourceThatIsNotThere` که U است
نشسته بود. جای‌شان عوض شد.

`notification_test.go` و `credential_test.go` از قبل درست بودند. `uow_test.go`
دست نخورد — تست‌هایش رفتار transaction است نه CRUD. `postgres.go` هم زیرساخت است و
CRUD ندارد.

هیچ خطی از محتوای تست‌ها عوض نشد؛ با مقایسهٔ خطوط مرتب‌شدهٔ قبل و بعد چک شد که فقط
ترتیب بلوک‌ها جابه‌جا شده.

# Files Changed

- `internal/adapter/db/postgres/delivery_test.go` *(تست update رفت آخر)*
- `internal/adapter/db/postgres/source_test.go` *(U قبل از D)*

# Tests Run

- `go test -tags integration ./internal/adapter/db/postgres/` — ۲۹ تست، همه سبز
- `golangci-lint run --build-tags=integration` — صفر ایراد
- `gofmt -l` — تمیز

# Follow-ups / Risks

- ندارد. `webhook.go` همچنان stub است و قدم بعدی همان است.

# Instruction

«در db/postgres، ترتیب همه را بر اساس CRUD بگذار.»
