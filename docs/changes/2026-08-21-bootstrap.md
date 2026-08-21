Branch: `feat/bootstrap`

# Summary

`internal/bootstrap` و هر دو `main.go` نوشته شدند. برای اولین بار هر دو باینری build
می‌شوند و اجرا می‌شوند.

باید صادق بود دربارهٔ اینکه چه چیزی تحویل داده شد: باینری‌ای که بالا می‌آید، به
postgres و nats وصل می‌شود، `/healthz` و `/readyz` را سرو می‌کند، و با سیگنال تمیز
پایین می‌آید. **هیچ notification ای نمی‌پذیرد و هیچ چیز نمی‌فرستد** — زنجیرهٔ
repository به use case هنوز وجود ندارد، چون `internal/adapter/db/postgres/` هنوز
فایل‌های خالی است.

ولی تمام چرخهٔ عمری که این چند روز نوشته شد، حالا واقعاً اجرا می‌شود.

## App

`Run` بلاک می‌شود تا یکی از دو چیز:

```
select {
case <-ctx.Done():          // به ما گفتند بایست
case err := <-server.Err(): // چیزی که قرار بود بماند، خودش ایستاد
}
```

شاخهٔ دوم لازم است: بدون آن، listener ای که می‌میرد یعنی process می‌نشیند «سالم» و به
هیچ چیز جواب نمی‌دهد.

`Close` همان `Resources.Close` است و بس.

## سه چیزی که راحت اشتباه می‌شوند

**۱ - `Close` یک context تازه می‌خواهد.** وقتی `Run` برمی‌گردد، `ctx` از قبل لغو
شده. اگر همان را به `Close` بدهیم، هر `step` قبل از اینکه کاری بکند شکست می‌خورد و
drain اصلاً اتفاق نمی‌افتد:

```
shutdown, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
```

`NOTIF_APP_SHUTDOWN_TIMEOUT` که از روز اول در config بود، بالاخره مصرف‌کننده پیدا
کرد.

**۲ - شکست وسط بالا آمدن نباید چیزی جا بگذارد.** اگر postgres باز شد و nats شکست
خورد، pool باید بسته شود. `abandon` همین است، و هر مرحله از آن رد می‌شود.

**۳ - سیگنال مال process است، نه bootstrap.** `signal.NotifyContext` در `main.go`
می‌ماند. `bootstrap` نباید بداند که در یک process زندگی می‌کند.

## و یکی که نمی‌شود اشتباه کرد

اگر `LoadGateway()` شکست بخورد، هنوز logger نداریم — چون خودِ config است که می‌گوید
logger چه شکلی باشد. آن یک خطا به `os.Stderr` چاپ می‌شود و `os.Exit(1)`. بعد از آن
نقطه، همه‌چیز از logger رد می‌شود.

## اجرا شد

مسیر شکست واقعاً اجرا شد:

```
level=WARN msg="database not ready yet" service=srosha binary=gateway attempt=1 of=2 err="..."
level=WARN msg="database not ready yet" service=srosha binary=gateway attempt=2 of=2 err="..."
database: not reachable after 2 attempts: ...
exit status 1
```

logger با `service` و `binary` کار می‌کند، حلقهٔ retry کار می‌کند، خطا تمیز به stderr
می‌رود، exit code یک است، و رمز عبور در هیچ‌کدام نیست.

# Files Changed

- `internal/bootstrap/app.go` *(تازه — `App` با `Run`/`Close`، و کمکی‌های خصوصی `logger`، `healthServer`، `abandon`)*
- `internal/bootstrap/gateway.go`, `dispatcher.go` *(تازه — گراف هر باینری)*
- `internal/bootstrap/const.go` *(تازه — نام دو باینری)*
- `cmd/gateway/main.go`, `cmd/dispatcher/main.go` *(از stub به کد واقعی)*

# Tests Run

- `make prepush` — همه پاس
- `go run ./cmd/gateway` با یک DSN که به هیچ‌جا نمی‌رود: خروجی بالا، exit code یک

# Follow-ups / Risks

- **مسیر موفق امتحان نشده.** postgres و nats می‌خواهد و Docker روی این ماشین بالا
  نبود. اینکه واقعاً بالا بیاید و `curl /readyz` جواب بدهد، هنوز دیده نشده. اولین
  کاری است که باید با Docker انجام شود.
- `bootstrap` هیچ تستی ندارد. هر تابعش به postgres و nats واقعی وصل می‌شود، پس
  تستش یک integration test است و جایش `tests/integration/` است.
- gateway هنوز سرور gRPC ندارد و dispatcher هنوز consumer ندارد. `App.Run` تا آن
  موقع فقط منتظر می‌ماند.
- `nats.Name(...)` هنوز پر نشده، در حالی که حالا `bootstrap` نام باینری را می‌داند و
  می‌تواند بدهدش.

# Instruction

«برویم bootstrap را بنویسیم»، با چهار تصمیمی که قبلش تأیید شد: `Run` و `Close` جدا
باشند؛ سیگنال در `main.go` بماند نه در bootstrap؛ `Close` یک context تازه با بودجهٔ
`ShutdownTimeout` بگیرد؛ و خطای قبل از ساخته شدن logger به stderr برود.
