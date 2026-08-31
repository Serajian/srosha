Branch: `refactor/admin-on-its-own-host`

# Summary

یک کامنت که commit ــِ `a862f85` باطلش کرده بود و جا مانده بود.

کامنتِ فیلدِ `AdminAddr` در `internal/config/settings/console.go` هنوز می‌گفت:

> Never published — it defaults to the loopback interface rather than every
> interface, so **staying off the network is a property of the process** and
> not only a deployment fact.

آن جمله دقیقاً چیزی را توصیف می‌کرد که همان commit حذفش کرد. guard ــِ loopback
دیگر وجود ندارد و پنل عمداً روی `admin.srosha.ir` عمومی است.

جایش نوشته شد که چه چیزی واقعاً مانع است — cookie ای که به همان host اسکوپ شده،
به‌علاوهٔ خواندنِ زندهٔ نقش — و اینکه پیش‌فرضِ loopback هنوز درست است ولی **فقط
یک پیش‌فرض** است، برای لپ‌تاپ.

موقعِ نوشتنِ plan ــِ deployment پیدا شد، نه موقعِ خودِ آن task. دو commit دیرتر
از آنچه باید.

# Files Changed

- `internal/config/settings/console.go` *(یک کامنت)*

# Tests Run

- `go build ./...` — clean
- `make precommit` — pass

# Follow-ups / Risks

- None. تنها جای دیگری که همین ادعا را داشت — `docs/ARCHITECTURE.md` و جدولِ
  پورت‌ها در `docs/CONFIG.md` — در `6697890` اصلاح شده بود.

# Instruction

مالک گفت موردِ یک از فهرستِ باقی‌مانده انجام شود: همان کامنتِ جامانده. روی این
branch انجام شد و نه روی `feat/deployment-stack`، چون فقط اینجاست که guard حذف
شده و کامنت واقعاً غلط است.
