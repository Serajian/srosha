Branch: `feat/migrate-with-lock`

# Summary

سه چیزی که `make prepush` گرفت و `make precommit` نگرفته بود.

**یک ثابتِ مرده.** `lockProbeSeconds` در `internal/bootstrap/const.go` مانده بود
از وقتی که صدا زدنِ goose از bootstrap به `internal/infra/migrations` منتقل شد.
آنجا `lockProbe` هست و کار می‌کند؛ این یکی فقط جا مانده بود و هیچ‌کس نمی‌خواندش.

**دو غلطِ املایی**، هر دو در `cmd/migrate/main.go`: `cancelling` و `behaviours`.
`misspell` املای آمریکایی را اجبار می‌کند.

**دو فایلِ بدونِ ترتیبِ import** — `bootstrap/migrate.go` و `registry/db.go` —
که هر دو موقعِ اضافه کردنِ import ــِ alias دار (`sqlfiles`) به هم ریخته بودند.

آخری دردسر داشت: کامنتی که توضیح می‌داد چرا alias لازم است **داخلِ** بلوکِ
import نشسته بود، و `golines` و `gci` سرِ آن با هم دعوا داشتند — یکی خطِ خالی
قبلش می‌خواست، دیگری برش می‌داشت، و `make fix` بینشان نوسان می‌کرد. کامنت به
محلِ استفاده منتقل شد و دعوا تمام شد.

`go.mod` هم عوض شد: `make fix` وابستگیِ goose را از indirect به direct برد، که
درست است — حالا کدِ خودمان مستقیم صدایش می‌زند.

# چیزی که این دوباره نشان داد

هیچ‌کدام از این‌ها در `precommit` دیده نمی‌شوند. `misspell`، `unused` و `gci`
همه داخلِ `golangci-lint` اند و آن فقط در `prepush` اجرا می‌شود.

این دومین بار امروز است. دفعهٔ قبل هم یک `dialled` تا لحظهٔ push رفت. قاعده
همان است که آن بار نوشتم: **سبز بودنِ precommit یعنی «چیزی نشکسته»، نه
«آمادهٔ push»**. قبل از گفتنِ «آماده است» باید `make prepush` زد.

# Files Changed

- `internal/bootstrap/const.go` *(ثابتِ مرده و import ــِ `time` که با آن بی‌مصرف شد)*
- `cmd/migrate/main.go` *(دو کلمه)*
- `internal/bootstrap/migrate.go`, `internal/registry/db.go` *(ترتیبِ import)*

# Tests Run

- `make lint` — `0 issues`
- `go build ./...` و `go test -count=1 ./...` — بدون شکست
- `make precommit` — pass

# Follow-ups / Risks

- None. هیچ رفتاری عوض نشده.

# Instruction

push رد شد چون hook سه ایراد گرفت. اصلاح شد تا push بتواند برود.
