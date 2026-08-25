Branch: `feat/bale-sender`

# Summary

**کانال دوم.** Bale می‌فرستد، و برای اولین بار یک delivery روی کانالی جز telegram
تعیین تکلیف شد.

# چرا یک package جدا، و نه یک پارامتر روی telegram

این تنها تصمیم واقعی این commit است، و از قبل گرفته شده بود.

API ــِ Bale امروز هم‌شکلِ Telegram است — همان `/bot<token>/sendMessage`، همان
پاکت `{ok, result, error_code, description}`. وسوسه‌اش این است که یکی را
پارامتری کنیم و base URL را عوض کنیم.

دو چیز جلویش را می‌گیرد:

**۱ — `ARCHITECTURE.md` صریحاً گفته:** *«هرچه یک provider دربارهٔ خودش می‌داند در
پوشهٔ خودش می‌ماند و جای دیگری نه. provider دوم یک پوشهٔ دوم است، نه یک شاخه در
کد مشترک.»*

**۲ — `make arch-check` راه دیگر را می‌بندد.** یک زیرپکیج adapter نمی‌تواند
زیرپکیج دیگری را import کند:

```
sender/bale  →  sender/telegram      ✗
sender/bale  →  sender/botapi        ✗   (botapi زیرمجموعهٔ bale نیست)
sender       →  sender/{bale,telegram}  ✓
```

پس تکرار امروز، در برابر واگرایی فردا. و آن‌ها **واگرا خواهند شد**: طول پیامی که
می‌پذیرند، parse mode هایی که دارند، و اینکه یک ۴۲۹ زمان انتظارش را می‌گوید یا نه
— هر کدام مالِ خودشان است که تغییر دهند.

# شکل API حدس نیست

قبل از نوشتن، مستقیم از خودش پرسیدم:

```
POST https://tapi.bale.ai/bot123456:AAFAKE-token/sendMessage
     {"chat_id":"-100999","text":"probe"}
                    ↓
HTTP 403
{"ok":false,"error_code":403,"description":"Bad Request: Token not found"}
```

همان پاکت، و ۴۰۳ برای توکن بد — که `classify` دائمی حساب می‌کند. پس نه شکل
حدس زده شد و نه طبقه‌بندی.

نکته‌ای که به آن تکیه نکردم: `parameters.retry_after`. اگر باشد خوانده می‌شود،
اگر نباشد یک انتظار از خودمان جایش می‌نشیند — چون فرستادن دوباره بلافاصله بعد از
۴۲۹ همان کاری است که محدودیتی را که داشت پاک می‌شد دوباره به دست می‌آورد.

# `buildOwn`

`ours` قبلاً برای telegram توکن خالی را خودش چک می‌کرد. با کانال دوم آن شاخه
تکراری می‌شد، پس یک خط شد:

```go
func (r *Registry) buildOwn(c shared.Channel, token string) (delivery.Sender, error) {
	if token == "" {
		return nil, noSender(c)
	}
	return r.build(c, nil, token)
}
```

توکن خالی همین‌جا رد می‌شود نه در پکیج provider، تا «روی این کانال هویتی نداریم»
و «هویتی که داریم قابل استفاده نیست» برای کسی که delivery را می‌خواند یک جمله
بماند.

# Files Changed

- `internal/adapter/sender/bale/{sender,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/bale/{sender,export}_test.go` *(تازه — ۱۵ تست)*
- `internal/adapter/sender/registry.go` *(`Fallback.BaleToken`، `buildOwn`، شاخهٔ bale)*
- `internal/adapter/sender/registry_test.go` *(هر دو کانال از یک جدول عبور می‌کنند)*
- `internal/bootstrap/dispatcher.go` *(`BaleToken` از settings)*

هیچ کلید تازه‌ای در کانفیگ نیست: `NOTIF_SENDER_BALE_TOKEN` از روز اول در
`CONFIG.md` و `.env.dispatcher.example` بود و تا امروز کسی نمی‌خواندش.

# Tests Run

- `make prepush` — سبز
- `golangci-lint run ./...` — صفر ایراد
- دستی، هر دو باینری، با تماس واقعی به `tapi.bale.ai`:

```
RegisterCredential  CHANNEL_BALE، توکن جعلی ولی خوش‌فرم  →  سربسته ذخیره شد
Submit              routes: [bale]
                            ↓
dispatcher  consume → credential → Material → tapi.bale.ai → 403
                            ↓
postgres    bale · FAILED · PERMANENT · attempts=1
            last_error: "Bad Request: Token not found"
```

آن `last_error` کلمات خودِ Bale است، که برای اپراتور نگه داشته می‌شود و هرگز به
مشتری نمی‌رسد — `Reason` چیزی است که او می‌بیند.

`TestEveryWiredChannelResolves` هم حالا هر دو کانال را از یک جدول رد می‌کند: مالِ
خودشان، مالِ ما وقتی چیزی ثبت نکرده‌اند، و NO_SENDER وقتی هیچ‌کدام نیست. کانال
سوم یک ردیف در آن جدول است.

# Follow-ups / Risks

- **هیچ ارسال موفقی آزموده نشده**، همان‌طور که برای telegram هم نشده. توکن واقعی
  لازم دارد.
- `maxTextLen` برای Bale ۴۰۹۶ فرض شده، به قیاس با Telegram. اگر کمتر باشد، پیامِ
  بلند یک ۴۰۰ می‌گیرد که دائمی حساب می‌شود — همان نتیجه، از راه کندتر.
- `email` و `whatsapp` هنوز `NO_SENDER` می‌دهند.

# Instruction

«bale را بنویس» — بعد از telegram، و روی همان برنچ سیم‌کشی dispatcher چون
`Fallback` آنجاست.
