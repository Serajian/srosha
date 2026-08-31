Branch: `feat/admin-panel`

# Summary

`audit_log` از Task 2 همیشه نوشته می‌شد و هیچ‌وقت خوانده نمی‌شد. این تغییر مسیر
خواندنش را باز می‌کند -- کوچک‌ترین کار در طرح، و همان‌قدر کوچک ماند.

`usecase.AuditLog` (در `gate.go`) یک متد گرفت:

```go
List(ctx context.Context, limit int32) ([]AuditEntry, error)
```

و روی `Operators` (در `operator_read.go`، کنار `Messages` و `Deliveries`)
متدی به همان شکل:

```go
func (o *Operators) Audit(ctx context.Context, actor *user.User) ([]AuditEntry, error)
```

`Audit` مثل `Messages` و `Deliveries` از `mayOperate` رد می‌شود، نه از چک
`super_admin` -- خواندنِ لاگ کارِ معمولِ اپراتور است، نه اداره‌ی آدم‌ها. سقفش
`MaxOperatorAudit = 200` است، دقیقاً کنار `MaxOperatorMessages` در `const.go`
و با همان استدلال: یک صفحه که آدم می‌خواند، نه چیزی که ماشین صفحه‌به‌صفحه بگیرد.

سمتِ adapter: `queries/audit.sql` یک `ListAudit` گرفت (`SELECT * ... ORDER BY at
DESC LIMIT @row_limit`)، `make sqlc` کد را زد، و `AuditRepository.List` در
`audit.go` سطرهای `gen.AuditLog` را به `usecase.AuditEntry` نگاشت می‌کند.

## تصمیمی که طرح باز گذاشته بود: فیلتر یا نه

`Audit` امروز جدیدترین N سطر را بدون هیچ فیلتری برمی‌گرداند -- نه به‌ازای
source، نه به‌ازای actor. عمداً همین‌طور ماند: امضای پورت را خودِ brief همین‌طور
نوشته بود (`List(ctx, limit)`، بدون پارامتر فیلتر)، و امروز هیچ صفحه‌ای سوال
«چه اتفاقی برای این source افتاد» را نمی‌پرسد -- `Queue`، `AllSources` و
`Source` از قبل یک source به دست اپراتور می‌دهند و یک صفحه گشتن با چشم رویش
کافی است. فیلتر اضافه‌کردن حالا حدس زدنِ کوئری بعدی است، نه جواب دادن به
سوالی که کسی الان پرسیده -- همان اشتباهی که قاعده‌ی «پورتی که یک متد به‌ازای هر
کوئری می‌گیرد» در `CONVENTIONS.md` هشدارش را می‌دهد. دلیلش در کامنتِ بالای
`Audit` نوشته شده تا روزی که کسی این فیلتر را خواست، همان‌جا باشد.

## عریض‌شدنِ `AuditLog`

چون `AuditLog` عریض شد، هر دو fake ای که پیاده‌اش می‌کردند باید `List` می‌گرفتند:
`auditLog` در `gate_test.go` و `memAudit` در `portal_test.go`. هر دو کپی
برمی‌گردانند، جدیدترین اول، نه handle زنده به storage -- همان قاعده‌ای که این
پکیج بعد از سه باگِ aliasing این هفته روی همه‌ی fake هایش گذاشته.

`Operators` هم یک فیلد تازه گرفت (`audit AuditLog`) و `NewOperators` یک
پارامتر تازه -- تنها caller اش امروز `newOperatorRig` در `operator_test.go`
است؛ چیز دیگری در بوت‌استرپ صدایش نمی‌زند چون سرویس ادمین هنوز به هیچ‌جا وصل
نشده.

# Files Changed

- `internal/core/usecase/gate.go` *(`AuditLog.List`)*
- `internal/core/usecase/const.go` *(`MaxOperatorAudit`)*
- `internal/core/usecase/operator.go` *(فیلد و پارامترِ `audit AuditLog`)*
- `internal/core/usecase/operator_read.go` *(متدِ `Audit`)*
- `internal/core/usecase/gate_test.go` *(`auditLog.List`)*
- `internal/core/usecase/operator_test.go` *(rig پارامترِ تازه‌ی `NewOperators` را می‌دهد)*
- `internal/adapter/api/web/portal_test.go` *(`memAudit.List`)*
- `internal/adapter/db/postgres/queries/audit.sql` *(کوئری `ListAudit`)*
- `internal/adapter/db/postgres/gen/audit.sql.go` *(`make sqlc`، تولیدشده)*
- `internal/adapter/db/postgres/audit.go` *(`AuditRepository.List` و نگاشتش)*
- `internal/adapter/db/postgres/audit_test.go` *(دو تستِ integration تازه)*

# Tests Run

- `go build ./...` -- سبز
- `go test -count=1 ./...` -- سبز
- `go test -count=1 -tags=integration ./internal/adapter/db/postgres/` -- سبز
- `make prepush` -- سبز (fmt، vet، arch-check، sqlc-check، buf lint، golangci-lint، race tests، sdk)
- شکستنِ عمدی: `Note: row.Note` را از نگاشتِ `AuditRepository.List` برداشتم.
  `TestListRoundTripsNoteAndActorEmail` قرمز شد با
  `note = "", want it to survive the round trip`. برگرداندم، دوباره سبز شد.

# Follow-ups / Risks

- **صفحه‌ای که `Audit` را صدا بزند هنوز نیست.** این تسک فقط مسیرِ خواندن را باز
  کرد؛ وصل‌کردنش به یک route ادمین کارِ یک تسکِ دیگر در همین طرح است.
- **بدون فیلتر، عمداً.** اگر روزی صفحه‌ای «چه اتفاقی برای این source افتاد»
  خواست، `List` باید یک پارامترِ فیلتر بگیرد -- امضای پورت امروز آن روز را
  نمی‌شکند، فقط یک پارامتر اضافه می‌شود.
- **امضای `NewOperators` عوض شد.** امروز فقط `operator_test.go` صدایش می‌زند،
  پس چیزی نشکست؛ وقتی بوت‌استرپ ادمین وصل شود همان‌جا هم باید این پارامتر را
  بدهد.

# Instruction

اجرای Task 6 از طرحِ admin panel طبق
`.superpowers/sdd/2026-08-30-admin-panel/task-6-brief.md`: مسیرِ خواندنِ
audit_log باز شود -- `usecase.AuditLog.List`، `Operators.Audit`، و نیمه‌ی
adapter در postgres -- بدون دست‌زدن به چیزی از Task های ۱ تا ۵ فراتر از همین
نیاز.
