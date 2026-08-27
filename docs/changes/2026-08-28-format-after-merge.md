Branch: `fix/format-after-merge`

# Summary

**master بعد از merge قرمز شد، و علتش خواندنی است.**

```
feat/webhook-secret   خطوطِ بلند به register_test.go اضافه کرد
                      merge شد اول -- بررسیِ golines هنوز وجود نداشت

chore/format-check    بررسیِ golines را اضافه کرد
                      merge شد دوم -- آن خطوط را هرگز ندیده بود
```

هرکدام جدا سبز، با هم قرمز. هیچ‌کدام نمی‌توانست این را ببیند.

این دقیقاً کاری است که CI می‌کند — و CI نداریم. تا وقتی که ندارد، هر جفتِ برنچی
که همزمان باز باشند همین را می‌توانند بسازند.

# و یک ادعای غلط که همین اتفاق آشکارش کرد

دیروز در کامنتِ `fmt-check` نوشتم:

> sdk/ is absent on purpose: it is its own module, and `make sdk` checks it
> with golangci-lint, whose formatters cover the same ground.

**غلط بود.** فرمترهای golangci-lint در این مخزن `gofumpt` و `gci` اند و
**هیچ‌کدام خط نمی‌شکنند**. یعنی `sdk/` دقیقاً همان شکافی را داشت که تازه برای
ماژولِ اصلی بسته بودم — و `make format` که از ریشه اجرا می‌شود آنجا را هم عوض
می‌کرد، پس تفاوتِ «چه چیزی عوض می‌شود» و «چه چیزی بررسی می‌شود» سرِ جایش مانده
بود.

پیدا شدنش هم اتفاقی نبود: بعد از اصلاحِ `register_test.go` دوباره `make format`
زدم تا مطمئن شوم درخت در نقطهٔ ثابت است، و **دو فایلِ دیگر عوض شدند**.

`make sdk` حالا خودش `golines -l` می‌زند، و آزموده شد که می‌گیردشان:

```
❌ lines over 100 in sdk/go:
   srosha/callback.go
   srosha/callback_test.go
```

# Files Changed

- `internal/core/usecase/register_test.go` *(خطوطِ بلندِ merge)*
- `sdk/go/srosha/{callback,callback_test}.go` *(همان، در ماژولِ SDK)*
- `Makefile` *(`golines` در `make sdk`، و کامنتِ غلط اصلاح شد)*

# Tests Run

- `make prepush` — سبز
- بررسیِ SDK با فایل‌های اصلاح‌نشده آزموده شد و گرفتشان
- `make format` دوباره — صفر تغییر، این بار در هر دو ماژول

# Follow-ups / Risks

- **پنج خط در `register_test.go` هنوز بالای ۱۰۰ کاراکترند** و `golines` عمداً
  دست نمی‌زندشان — داخلِ `if ...; err != nil` اند. بررسی «نقطهٔ ثابتِ golines» را
  می‌سنجد نه «هر خط زیر ۱۰۰»، که همان معنیِ `make format` است. اگر روزی حدِ سخت
  خواستیم، `lll` در golangci-lint کارِ دیگری است.
- **این تا وقتی CI نیاید دوباره می‌افتد.** هوکِ محلی برنچِ خودش را می‌بیند، نه
  نتیجهٔ merge. دو برنچِ سبز هنوز می‌توانند master را بشکنند.

# Instruction

«همه merge شد» — و بلافاصله معلوم شد master سبز نیست.
