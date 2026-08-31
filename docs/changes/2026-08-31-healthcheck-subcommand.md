Branch: `feat/deployment-stack`

# Summary

هر سه باینری حالا آرگومانِ `healthcheck` را می‌شناسند. task یک از چهار در plan ــِ
`2026-08-31-deployment-stack.md`.

`bootstrap.Probe(addr)` به `/readyz` ــِ خودِ process درخواست می‌زند و بسته به
جواب صفر یا خطا برمی‌گرداند. دلیلِ وجودش این است که image ــِ runtime قرار است
distroless باشد: نه shell دارد، نه wget، نه curl. پس healthcheck ــِ docker
باید **خودِ باینری** باشد.

سه تصمیمِ طراحی که دلیل دارند:

**در `bootstrap` نشست، نه در adapter.** `cmd` از قبل فقط `bootstrap` و `config`
را import می‌کند؛ این هیچ یالِ تازه‌ای اضافه نمی‌کند و `make arch-check`
دست‌نخورده ماند.

**خواندنِ آرگومان در `main` ماند.** خودِ همان فایل‌ها بالای مدیریتِ سیگنال نوشته‌اند
«signals are the one thing that is genuinely the process's own». argv هم همان
جنس است.

**Probe هیچ قضاوتی نمی‌کند.** فقط همان چیزی را گزارش می‌کند که `/readyz` گفت — و
آن endpoint از قبل وقتی یک dependency پایین است ۵۰۳ می‌دهد. قضاوتِ دوم یعنی دو
جواب که می‌توانند اختلاف پیدا کنند.

و بعد از لود شدنِ config چک می‌شود، عمداً: container ای که configش دیگر خوانده
نمی‌شود سالم هم نیست، و گفتنِ این حقیقت است نه false negative.

`dialable` کارِ کوچکی می‌کند که لازم است: سرور `:8080` را bind می‌کند یعنی «هر
interface»، و client نمی‌تواند آن را dial کند. چون probe داخلِ همان container
اجرا می‌شود، جواب همیشه loopback است.

`probeTimeout` طبقِ قانونِ مخزن در `const.go` نشست نه کنارِ کد. سه ثانیه، زیرِ
پنج ثانیه‌ای که compose به دستور می‌دهد — تا جوابِ کند خطای ما باشد نه kill ــِ
docker.

# Files Changed

- `internal/bootstrap/healthcheck.go` *(جدید — `Probe` و `dialable`)*
- `internal/bootstrap/healthcheck_test.go` *(جدید — چهار حالت)*
- `internal/bootstrap/const.go` *(`probeTimeout`)*
- `cmd/gateway/main.go` *(آرگومان، با `cfg.GRPC.HTTPAddr`)*
- `cmd/dispatcher/main.go` *(آرگومان، با `cfg.HTTP.Addr`)*
- `cmd/console/main.go` *(آرگومان، با `cfg.HTTP.Addr`)*
- `docs/superpowers/plans/2026-08-31-deployment-stack.md` *(نهایی شد: distroless
  دیگر یک سؤال نیست، یک تصمیمِ ثبت‌شده است)*

آدرسِ health در gateway از کلیدِ دیگری می‌آید (`NOTIF_GRPC_HTTP_ADDR`) تا دو تای
دیگر (`NOTIF_HTTP_ADDR`). یکی نشدند.

# Tests Run

- `go test -count=1 ./internal/bootstrap/` — چهار تست، pass
- `go build ./...` و `go test -count=1 ./...` — بدون شکست
- `make format-core`، `make precommit` — clean
- **مقابلِ یک process ــِ واقعی، هر دو نیمه:**
  - با gateway خاموش: `exit=1` و پیامِ
    `not ready: … dial tcp 127.0.0.1:8080: connect: connection refused`
  - با gateway بالا: `exit=0`
  - بعد از خاموش کردنش دوباره: `exit=1`

  نیمهٔ دوم لازم بود. اولین بار که امتحان کردم `head -3` کدِ خروجی را بلعید و
  `exit=0` نشان داد در حالی که چیزی بالا نبود — یعنی probe ای که همیشه سبز است
  و کسی نمی‌فهمد. بدونِ اجرای هر دو نیمه، آن اشتباه دیده نمی‌شد.

# Follow-ups / Risks

- این branch از `master` جدا شده و master هنوز
  `refactor/admin-on-its-own-host` را ندارد. قدمِ اولِ task 4 (کامنتِ
  `AdminAddr` که آن branch باطلش کرده) تا merge نشدنِ آن قابلِ انجام نیست.
- سندِ `docs/reference/srosha-infra-deploy.md` بخشِ ۱۰ برای gateway پروتکلِ
  `grpc.health.v1.Health` هم می‌خواهد، کنارِ `/healthz`. پیاده نشده و این
  compose به آن نیاز ندارد، چون خودِ باینری را صدا می‌زند نه gRPC را.

# Instruction

مالک plan ــِ بازنویسی‌شدهٔ B را تأیید کرد و گفت اجرا شروع شود، با distroless.
task یک: زیردستورِ `healthcheck` در هر سه باینری، تا healthcheck ــِ docker
بدونِ shell و بدونِ wget کار کند.
