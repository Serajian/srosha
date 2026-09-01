Branch: `feat/operator-alerts`

# Summary

هر یازده فعلی که audit می‌شود، حالا به گوشیِ operator هم می‌رسد. task چهار از
پنج.

`Gate` یک port ــِ تک‌متدی گرفت و بعد از موفقیتِ تغییر خبر می‌دهد:

```go
if err := fn(ctx); err != nil {
    return err
}
g.tell(ctx, entry)
```

**بعد، و نه قبل.** audit عمداً تلاش را ثبت می‌کند — «تغییری که کسی حسابش را پس
ندهد بدتر از تغییرِ ردشده است» — ولی اعلانی که بگوید یک source ثبت شد، اگر نشده
باشد صرفاً غلط است.

هیچ فیلتری روی فعل‌ها نیست. مالک هر یازده‌تا را انتخاب کرد و فیلتر بعداً یک
تغییرِ تنظیمات است نه یک شکلِ تازه.

# `nil` یعنی سکوت

`NewGate` یک آرگومان بیشتر گرفت، که امضای صادرشده را عوض می‌کند. هفت جا صدایش
می‌زدند و همه تست بودند جز یکی (`bootstrap/console.go`). هیچ‌کدام لازم نشد چیزی
دربارهٔ اعلان بداند — فقط یک `nil` گرفتند و همان‌طور کار کردند.

# ایمیل داخلِ پیام است

```go
detail := fmt.Sprintf("%s by %s", e.TargetID, e.ActorEmail)
```

تصمیمِ مالک بود و در کامنتِ کد نوشته شده که یعنی چه: هرکس token ــِ کانالِ
اعلان را دارد آدرسِ ایمیلِ مشتری‌ها را می‌بیند — همان دسترسی‌ای که `/audit`
دارد، و دقیقاً دلیلی که `/audit` فقط `super_admin` است.

# آزمونِ ترتیب واقعاً می‌گیرد

عمداً `tell` را قبل از `fn` بردم:

```
--- FAIL: TestTheGateAlertsOnlyAfterTheChangeSucceeds
    an operator was told about a change that did not happen: [source.create]
```

بدونِ آن تست، این جابه‌جایی بی‌صدا رد می‌شد و گوشیِ operator می‌گفت چیزی ثبت
شده که نشده بود.

# Files Changed

- `internal/core/usecase/gate.go` *(port ــِ `Alerter`، `tell`، ترتیب)*
- `internal/core/usecase/gate_test.go` *(چهار تستِ تازه)*
- `internal/bootstrap/console.go` *(alerter تا `buildConsoleCore` پاس می‌شود)*
- شش فایلِ تست در `usecase`، `postgres` و `web` *(یک `nil`)*

# Tests Run

- چهار تستِ تازه، pass
- `go test -count=1 ./...` — بدون شکست
- `make prepush` — pass
- خراب‌کردنِ عمدیِ ترتیب: تست قرمز شد با پیامِ خودش

# Follow-ups / Risks

- فقط console یک `Gate` می‌سازد، پس هر یازده فعل از همان‌جا می‌آیند. gateway و
  dispatcher چیزی audit نمی‌کنند و اعلانشان فقط lifecycle است.
- کلیدهای تنظیمات هنوز در `docs/CONFIG.md` نیستند. task پنج.
