Branch: `master`

# Summary

base branch پروژه از `main` به `master` تغییر کرد.

پیش از این، repository روی `main` بود ولی هر دو سند مرجع (`srosha-spec-draft.md`
و `srosha-infra-deploy.md`) می‌گفتند base branch `master` است و «هرگز برنچ `main`
نساز» — و جدول تنظیمات دوکپلوی هم `Branch: master` را ثبت کرده بود. یعنی
auto-deploy روی `master` تنظیم شده و merge به `main` هیچ deployای راه نمی‌انداخت.
این تعارض به مالک گزارش شد و تصمیم گرفت `master` درست است.

تغییر در سه جا اعمال شد: گیت push در `CLAUDE.md` و `docs/CONVENTIONS.md` (لینک
Merge Request حالا به `master` می‌خورد)، بخش base branch و قانون نام‌گذاری برنچ در
`docs/CONVENTIONS.md`، و در `Makefile` مقادیر `GOLINES_BASE`، هدف
`proto-breaking`، و کل target `sync`.

در `docs/CONVENTIONS.md` این خط هم اضافه شد: «`main` وجود ندارد، نسازش.»

برنچ محلی هم با `git branch -m main master` تغییر نام داد.

# Files Changed

- `CLAUDE.md` *(هدف Merge Request در گیت push)*
- `docs/CONVENTIONS.md` *(گیت push، بخش base branch، قانون نام‌گذاری برنچ)*
- `Makefile` *(`GOLINES_BASE`، `proto-breaking`، `sync`، `clean-branches`)*
- برنچ محلی *(`main` → `master`)*

# Tests Run

- `git branch -vv` — `* master 9224c49 [origin/main]`
- `make help` — سبز
- grep روی ارجاع‌های باقی‌ماندهٔ `main` — تنها موارد باقی‌مانده guardهای عمدی در
  `clean-branches` و `sync` هستند که اگر برنچی به نام `main` وجود داشته باشد هم
  آن را پاک نکنند.

# Follow-ups / Risks

- **روی GitHub هنوز `main` است.** برنچ محلی `master` دارد `origin/main` را track
  می‌کند. برای درست‌شدنش دو کار لازم است که هیچ‌کدام در این تغییر انجام نشد: یک
  push (که پشت گیت push است و دستور مشخص می‌خواهد)، و عوض‌کردن default branch در
  تنظیمات GitHub و بعد تنظیم branch در دوکپلوی روی `master`.
- تا آن موقع `make sync` کار می‌کند ولی عملاً از `origin/main` می‌کشد.

# Instruction

مالک بعد از دیدن گزارش تعارض گفت `main` به `master` تبدیل شود تا درست شود.
