Branch: `feat/deployment-stack`

# Summary

یک کلمه: `dialled` شد `dialed`.

در پیامِ خطای `TestProbeDialsLoopbackForABarePort` نوشته شده بود، و از همان
plan آمده بود که خودم نوشتم — پس در هر دو جا اصلاح شد، کد و plan.

`misspell` املای آمریکایی را اجبار می‌کند و `dialled` بریتانیایی است.

# چیزی که این نشان داد

`make precommit` این را نگرفت و `make prepush` گرفت. دلیلش این است که
`misspell` در precommit نیست؛ فقط `golangci-lint` که در prepush اجرا می‌شود
آن را می‌بیند. یعنی چهار commit با این غلط رد شدند و اولین باری که معلوم شد،
لحظهٔ push بود.

این ایراد نیست — precommit عمداً سبک است (~۱ ثانیه). ولی دانستنش مهم است: سبز
بودنِ precommit یعنی «چیزی نشکسته»، نه «آمادهٔ push است».

# Files Changed

- `internal/bootstrap/healthcheck_test.go` *(یک کلمه در یک پیامِ خطا)*
- `docs/superpowers/plans/2026-08-31-deployment-stack.md` *(همان کلمه در کدِ نمونه)*

# Tests Run

- `make lint` — `0 issues`
- `make precommit` — pass

# Follow-ups / Risks

- None.

# Instruction

push رد شد چون hook یک غلطِ املایی گرفت. اصلاحش شد تا push بتواند برود.
