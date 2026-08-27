Branch: `feat/webhook-secret`

# Summary

**رازِ امضای callback از متغیرِ محیطی به دیتابیس رفت.**

```
NOTIF_WEBHOOK_SECRETS = {"01K0SRC…":"whsec_abc", "01K0SRC…":"whsec_def"}
```

یک map از شناسهٔ source به راز، در `.env` ــِ dispatcher. دو مشکل داشت:

```
هر مشتریِ تازه یک redeploy   ورودی اضافه شود، .env عوض شود، dispatcher ری‌استارت
تحویل هیچ مسیری نداشت        کسی باید بیرون از سرویس به مشتری می‌گفت
```

و تا آن ری‌استارت، `notifier` عمداً هیچ callback ای نمی‌فرستاد — که درست است
(بهتر از فرستادنِ امضانشده) ولی یعنی مشتری در آن فاصله هیچ خبری نمی‌گرفت.

# و تصحیحِ حرفِ خودم

در follow-ups ــِ قبلی نوشته بودم «همان جنسِ شکافِ پنل، و احتمالاً همان‌جا حل
می‌شود». **غلط بود.**

`Webhooks.Register` از قبل یک تماسِ **احرازشده** است، از طرفِ همان source ای که
راز را می‌خواهد. پس همان‌جا ساخته و **یک بار** برگردانده می‌شود. پنل هیچ نقشی
ندارد.

# `RotateSecret` اختیاری نبود

این را حین کار فهمیدم و دامنه را عوض کرد.

امروز اگر مشتری رازش را گم کند، اپراتور یکی تازه در env می‌گذارد و redeploy
می‌کند. **اگر env را برداریم و راهِ صدور دوباره نگذاریم، گم شدنِ راز
غیرقابل‌جبران می‌شود** — چون آنچه ذخیره می‌شود سربسته است و هیچ چیز پسش نمی‌دهد.

پس `RotateSecret` بخشی از همین کار است، نه یک اضافهٔ بعدی.

# سه تصمیم

**۱ — سربسته، با همان keyring ــِ credential ها.** تهدید یکی است: یک dump از
دیتابیس به مهاجم اجازه می‌دهد callback ــِ جعلی برای آن مشتری بسازد.

AAD برابرِ `sourceID|webhookID` است، پس ciphertext ای که در سطرِ دیگری کپی شود
باز نمی‌شود — بدونِ اینکه کلیدی شکسته باشد. همان استدلالِ credential ها با
فیلدهایی که این entity دارد.

**۲ — `WebhookKeeper` یک نوعِ جدا در همان package است، نه متدهای بیشتر روی
`Keeper`.** آن یکی سراپا credential است — `Store` اش با credential حرف می‌زند و
reseal-on-read اش برای این است که credential در هر پیام خوانده می‌شود. راز یک
بار در هر callback خوانده می‌شود و مالِ entity دیگری است.

**۳ — ثبتِ دوباره راز را عوض نمی‌کند.** `Register` روی callback ای که از قبل
هست، فقط آدرس را جابه‌جا می‌کند و **رازِ خالی** برمی‌گرداند. عوض کردنش بی‌صدا،
هر گیرنده‌ای را که از قبل تأیید می‌کرد می‌شکست.

# چه چیزی حذف شد

```
NOTIF_WEBHOOK_SECRETS      از settings، از .env.dispatcher.example، از CONFIG.md
settings.Webhook.Secrets   و SecretFor اش
```

و یک تست جایش نشست که مطمئن می‌شود config راهی برای برگرداندنِ آن پیدا نکند:
محیطی که هنوز کلیدِ قدیمی را می‌گذارد، محیطی است که کسی مهاجرتش را تمام نکرده.

# Files Changed

- `migrations/00005_create_webhooks.sql` *(ستونِ `secret`)*
- `internal/adapter/secret/webhook.go`، `const.go` *(تازه — `WebhookKeeper`)*
- `internal/adapter/db/postgres/{webhook.go,queries/webhook.sql}` + `gen`
- `internal/core/usecase/register.go` *(`SigningSecrets`، `Register` سه‌مقداری، `RotateSecret`)*
- `internal/core/usecase/register_test.go` *(پنج تستِ تازه)*
- `api/proto/notification/v1/webhook.proto` + `gen` *(`secret`، `RotateSecret`)*
- `internal/adapter/api/grpcsrv/webhook.go`
- `internal/bootstrap/{gateway,dispatcher}.go`
- `internal/config/settings/webhook.go`، `internal/config/config_test.go`
- `sdk/go/srosha/{webhook,callback}.go` و تست‌ها و مثال
- `.env.dispatcher.example`، `docs/CONFIG.md`، هر دو README

# Tests Run

- `make prepush` — سبز
- `go vet -tags=integration ./...` — سبز
- دستی، end-to-end از یک ماژولِ بیرونی، با **هیچ رازی که از قبل جایی باشد**:

```
register     url=http://127.0.0.1:59865/srosha active=true
secret       whsec_gyXDtIbO…   (یک بار داده شد)
re-register  secret=""          (همانی که داری سرِ جایش است)
verified     telegram failed no_sender   ← callback رسید و امضایش خواند
rotate       whsec_jgGVegkH…   (متفاوت)
```

و سه چیزِ دیگر جدا سنجیده شد:

```
در DB سربسته است        v1.1.tN0XKRsuy2Enyov-.Fs…
هیچ rpc ای پسش نمی‌دهد   WebhookService/Get فیلدِ secret ندارد
rotate واقعاً عوض می‌کند  مقدارِ ذخیره‌شده قبل و بعد فرق کرد
```

هر دو باینری **بدونِ `NOTIF_WEBHOOK_SECRETS`** بالا آمدند، که تا امروز ممکن
نبود.

دادهٔ تست پاک شد.

# Follow-ups / Risks

- **سطرهای قدیمی راز ندارند.** ستون nullable است، و هر webhook ای که قبل از این
  ساخته شده باشد callback نمی‌گیرد تا وقتی `RotateSecret` صدا زده شود. چیزی
  چاپ نمی‌کند که این وضع را اعلام کند. امروز اهمیتی ندارد چون چیزی مستقر نشده،
  ولی اگر شده بود، یک مهاجرتِ داده لازم داشت.
- **`SecretFor` در notifier بدونِ context است**، چون پورتی که از قبل بود
  چنین است. `WebhookKeeper` داخلش `context.Background()` می‌زند و کوئری یک سطرِ
  index دار است. عوض کردنِ آن پورت کارِ خودش است.
- **`RotateSecret` هیچ فاصله‌ای نمی‌دهد.** لحظه‌ای که برمی‌گردد، رازِ قبلی مرده
  است. یک دورهٔ پذیرشِ همزمانِ هر دو (مثل دو کلیدِ API) این را نرم می‌کرد، ولی
  یعنی دو راز در یک سطر و منطقِ انقضا.
- **`RegisterResponse.secret` گاهی خالی است و گاهی نه.** در proto و در هر دو
  README نوشته شده، ولی یک فیلدِ گاهی-حاضر همیشه جایی است که کسی اشتباه می‌کند.

# Instruction

«یک برنچ بساز و بزن» — بعد از اینکه توضیح داده شد مشکل چیست: راز در env است، پس
هر مشتریِ تازه یک redeploy است و تحویلش هیچ مسیری ندارد.
