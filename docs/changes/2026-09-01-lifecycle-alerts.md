Branch: `feat/operator-alerts`

# Summary

هر سه باینری حالا می‌گویند بالا آمده‌اند، و هر مسیرِ شکستِ startup خودش خبر
می‌دهد. task دو از پنج.

`settings.Alert` اضافه شد و به هر سه config رفت. خالی یعنی خاموش — روی لپ‌تاپ
کسی Gotify ندارد و این نباید آنجا هزینه‌ای داشته باشد.

`bootstrap.alerts()` سازندهٔ gotify را صدا می‌زند و پشتِ port ــِ alert
می‌گذارد. اینجا و نه داخلِ پکیجِ alert، چون یک adapter نمی‌تواند adapter ــِ
دیگری را import کند و bootstrap تنها جایی است که هر دو را می‌بیند.

`registry.AlertClient` یک http client ــِ **جدا** باز می‌کند، نه مالِ sender.
دلیلش در کامنتش نوشته شده: کلِ نکتهٔ یک اعلان این است که وقتی برسد که مسیرِ
ارسال همان چیزی است که خراب شده — پس نباید سرنوشتِ مشترک داشته باشند.

# ۲۴ مسیرِ شکست، با یک تغییر

`abandon` تنها قیفی است که همهٔ شکست‌های startup از آن رد می‌شوند. حالا خودش
خبر می‌دهد، و **قبل** از بستن — چون بستن همان چیزی است که صف را خالی می‌کند.

```
gateway      ۵ مسیر
dispatcher  ۱۱ مسیر
console      ۸ مسیر
```

deploy ای که وسطِ بالا آمدن می‌میرد، همان چیزی است که operator بیشتر از همه
می‌خواهد بشنود — و لاگش داخلِ container ای است که دارد از بین می‌رود.

# فرضِ `appid` بالاخره جواب گرفت — و غلط بود

قدمِ ۵ ــِ plan می‌گفت مقابلِ Gotify واقعی امتحان شود. یک نمونه با docker بالا
آوردم و سه حالت را زدم:

| درخواست | جواب | پیام کجا نشست |
| --- | --- | --- |
| `?token=…&appid=1` | ۲۰۰ | `appid=1` |
| `?token=…` بدونِ appid | ۲۰۰ | `appid=1` |
| `?token=…&appid=999` (وجود ندارد) | ۲۰۰ | `appid=1` |

**Gotify پارامترِ `appid` را نادیده می‌گیرد. token است که application را
انتخاب می‌کند.** این همان چیزی است که ماه‌ها در کامنتِ `(*Sender).endpoint`
به‌عنوانِ حدسِ مستند نوشته شده بود.

نتیجه‌اش: `NOTIF_ALERT_GOTIFY_APP_ID` حذف شد. کلیدی که operator باید عددی
برایش پیدا می‌کرد و هیچ کاری نمی‌کرد. حالا فقط آدرس و token. `bootstrap` یک
مقدارِ ثابت پاس می‌دهد، چون sender آدرسِ خالی را رد می‌کند — با کامنتی که
می‌گوید چرا آن مقدار بی‌معنی است.

# و کدِ خودمان جلوی probe را گرفت

تلاش کردم از طریقِ خودِ sender بفرستم و این را گرفتم:

```
INVALID_INPUT: gotify server url must use https
```

`Config.validate` آدرسِ `http://` را رد می‌کند چون token در query string
می‌رود. محافظتِ درستی است و سرِ جایش کار کرد؛ به همین دلیل probe با curl کامل
شد، که **دقیقاً همان URL ای را می‌سازد** که sender می‌سازد.

پس آنچه ثابت شد: شکلِ درخواست کار می‌کند و `appid` بی‌اثر است. آنچه ثابت نشد:
عبورِ همان درخواست از باینریِ خودمان روی https — که سرورِ واقعیِ مالک به‌هرحال
https است.

# Files Changed

- `internal/config/settings/alert.go` *(جدید)*
- `internal/config/gateway.go`, `dispatcher.go`, `console.go` *(فیلدِ Alert)*
- `internal/config/config_test.go` *(سه تست)*
- `internal/bootstrap/alert.go` *(جدید)*
- `internal/bootstrap/app.go` *(`abandon` خبر می‌دهد)*
- `internal/bootstrap/gateway.go`, `dispatcher.go`, `console.go` *(ساخت و اعلام)*
- `internal/bootstrap/const.go` *(`gotifyIgnoredAppID`)*
- `internal/registry/alert.go` *(جدید — جای alerter در ترتیبِ خاموشی)*
- `internal/registry/httpclient.go`, `const.go` *(`AlertClient`)*

# Tests Run

- `go test -count=1 ./...` — بدون شکست
- `make prepush` — pass
- مقابلِ Gotify واقعی: سه درخواست، سه بار ۲۰۰، و هر سه در همان application

# Follow-ups / Risks

- **سندِ SDK به مشتری‌ها می‌گوید آدرسِ Gotify همان application id است. نیست.**
  یک token یعنی یک application و آن آدرس تزئینی است. `sdk/go/README` و کامنتِ
  `(*Sender).endpoint` و بخشِ Gotify در `docs/CONFIG.md` هر سه باید اصلاح شوند
  — جدا از این branch، چون دربارهٔ کانالِ مشتری است نه اعلان.
- هنوز هیچ اعلانی از رویدادهای audit نمی‌آید. task چهار.
- readiness هنوز polled نمی‌شود. task سه.
