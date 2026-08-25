Branch: `feat/list-and-update`

# Summary

دو شکافی که در API مانده بودند، و هر دو را چیزی که قبلاً ساختیم باز کرده بود.

```
NotificationService.List    «دیروز چه فرستادم؟»
CredentialService.Update    «سرور SMTP ام عوض شد»
```

# چرا `List` یک وعدهٔ نانوشته را می‌بست

`proto` خودِ webhook را این‌طور توصیف کرده:

> *«callback یک راحتی روی `Get` است، نه جایگزینش: یک بار فرستاده می‌شود و هرگز
> retry نمی‌شود، پس source ای که نباید نتیجه‌ای را از دست بدهد، **می‌پرسد**.»*

ولی پرسیدن یعنی `Get(id)` — و id همان چیزی است که callback می‌آورد. اگر callback
از دست برود و id ذخیره نشده باشد، آن «راه مطمئن» **بسته** است.

```
مشتری   «دیروز چه فرستادم؟»
srosha  «id اش را بده»
مشتری   «همان که نمی‌دانم چیست»
```

## تازه‌ترین اول، برخلاف هر لیست دیگری اینجا

```sql
ORDER BY id DESC
AND (after IS NULL OR id < after)
```

هر listing دیگری در این repo رو به جلو می‌رود. این یکی نه، و دلیلش سؤال است: کسی
که می‌پرسد «چه فرستادم» دنبال چیزی است که همین حالا فرستاده، نه چیزی که روز
ثبت‌نامش فرستاده. cursor هم عقب می‌رود — `id < after` — که چون ULID زمانی مرتب
است، یعنی عقب رفتن در زمان.

## پنجره، و اینکه چرا یک type است

```go
type Window struct{ From, Until *time.Time }
```

دو آرگومان `*time.Time` را می‌شود جابه‌جا پاس داد بدون اینکه کامپایلر چیزی بگوید.
و هر دو نیم‌بازند، چون «از دیروز» و «آن هفته در اسفند» هر دو سؤال واقعی‌اند و
هیچ‌کدام نباید مجبور شود کرانِ دیگری را از خودش دربیاورد.

`Until` انحصاری است، پس دو پنجره که به هم می‌رسند نمی‌توانند هر دو یک پیام را
برگردانند.

پنجره‌ای که نمی‌تواند چیزی داشته باشد **رد می‌شود**، نه اینکه خالی جواب بگیرد —
جواب خالی شبیه جواب است.

## و `List` عمداً delivery نمی‌دهد

```
با delivery ها  →  یا یک query به ازای هر ردیف، یا یک join که خودش صفحه‌بندی می‌خواهد
بدون            →  و Get دقیقاً همین کار را از قبل می‌کند
```

کار `List` برگرداندن همان id هایی است که `Get` با آن‌ها پرسیده می‌شود.

# چرا `Update` را email لازم کرد

تا قبل از email، `config` این بود:

```json
{ "parse_mode": "HTML" }
```

حالا این است:

```json
{ "host": "smtp.acme.test", "port": 587, "username": "acme", "from": "srosha@acme.test" }
```

و `Rotate` فقط راز را عوض می‌کند. پس عوض شدن سرور SMTP هیچ راهی نداشت جز ساختن
هویتی با **نام تازه** — و آن‌وقت هر پیامی که `sender: "transactional"` می‌گوید
`NO_SENDER` می‌گیرد. همان آسیبی که `Rotate` برای جلوگیری از آن ساخته شد، این بار
از سمت تنظیمات.

**نام عوض نمی‌شود، و این تصمیم است:** پیام هویت را به نام صدا می‌زند.

و آنچه می‌رسد **کل سند است، نه یک patch**. patch یعنی این لایه باید بداند یک
provider چه فیلدهایی دارد، و عمداً نمی‌داند.

# REST نوشته نشد، و نوشته شد که نمی‌شود

`CONFIG.md` از روز اول وعده داده بود:

```
| gateway | 8080 | REST via grpc-gateway, and /healthz |
```

این حذف شد. srosha را سرویس‌های دیگر صدا می‌زنند نه مرورگر، و gRPC همان چیزی است
که آن‌ها حرف می‌زنند. یک سطح دوم یعنی یک contract دوم که باید اولی را در برابرش
صادق نگه داشت — برای صداکننده‌هایی که وجود ندارند.

و روشن شد که پورت سلامت **اصلاً API نیست**: `/healthz` مالِ پلتفرم است تا تصمیم
بگیرد کانتینر زنده است یا نه، و هیچ‌چیزش وعده‌ای به مشتری نیست.

# Files Changed

- `internal/adapter/db/postgres/queries/{notification,credential}.sql` + `gen/`
- `internal/adapter/db/postgres/{notification,credential}.go` *(`PageBySource`، `UpdateConfig`)*
- `internal/adapter/db/postgres/{notification,credential}_test.go` *(۵ تست integration)*
- `internal/core/domain/notification/{port,service,types,errors}.go` *(`Window`، `Page`)*
- `internal/core/usecase/query.go` *(`List`)*
- `internal/core/usecase/credential.go` *(`Update`، پورت `CredentialSettings`)*
- `internal/core/usecase/{query,credential,fakes}_test.go`
- `api/proto/notification/v1/{notification,credential}.proto` *(۲ rpc)* + `gen/`
- `internal/adapter/api/grpcsrv/{notification,credential,mapper}.go`
- `internal/bootstrap/gateway.go`
- `docs/CONFIG.md` *(REST حذف شد، پورت‌های سلامت روشن شدند)*

هیچ migration و هیچ کلید کانفیگی لازم نشد.

# Tests Run

- `make prepush` — سبز
- `go test -tags=integration ./internal/adapter/db/postgres/` — پاس
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، روی gateway واقعی:

```
List limit=2      →  msg 3 · msg 2   next=…
با next           →  msg 1           next=(none)
from > until      →  InvalidArgument: the time window ends before it starts
from=یک ساعت پیش   →  ۳ پیام
                        ↓
Update            →  نام "transactional" ماند
postgres          →  config عوض شد، secret دست‌نخورده (v1.1.…)
```

# Follow-ups / Risks

- **هیچ‌چیز هیچ‌چیز را پاک نمی‌کند.** نه ستون انقضایی هست نه job ای؛ `notifications`
  و `deliveries` تا ابد رشد می‌کنند. کارِ بعدی که ثبت شد: scheduler در دوره‌های
  مشخص پیام‌های کهنه‌تر از یک ماه را پاک کند، و delivery ها با `ON DELETE CASCADE`
  خودشان می‌روند.

  **ولی شرطش نباید فقط سن باشد.** یک پیام کهنه که هنوز delivery ــِ `PENDING`
  دارد، با پاک شدن، کارِ نفرستاده را بی‌صدا دور می‌ریزد. شرط باید «همه settled» را
  هم داشته باشد.

- **`List` روی وضعیت و کانال فیلتر نمی‌کند.** هر دو روی delivery اند نه
  notification، پس یعنی join و یک index تازه. برای جدولی که قرار است با آن job
  کوچک بماند، زود است.

- **`config` هنوز فقط `json.Valid` می‌شود**، همان‌طور که `Register` هم می‌کرد.
  اینکه یک تنظیمات SMTP قابل استفاده است یا نه، در dispatcher و در لحظهٔ ساختن
  sender معلوم می‌شود — که `NO_SENDER` می‌دهد و به source گزارش می‌شود، ولی دیرتر
  از آنکه می‌شد.

# Instruction

«۴ را انجام بده» با سه تصمیم: `List` با cursor و بازهٔ زمانی، فقط پیام‌ها بدون
delivery، و `Update` فقط روی `config`. و «ج» — REST — رد شد، چون srosha فقط
بین سرویس‌هاست.
