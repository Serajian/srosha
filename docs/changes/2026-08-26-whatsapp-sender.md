Branch: `feat/whatsapp-sender`

# Summary

**کانال چهارم، و اولین کانالی که همیشه نمی‌تواند هرچه بخواهد بگوید.**

`whatsapp` از روز اول در enum بود با یک stub خالی پشتش — تنها کانالی که نیمه‌کاره
بود نه غایب. و نیمه‌کاره بود چون API اش چیزی می‌خواست که پیام حمل نمی‌کرد. دیروز
آن درز ساخته شد؛ امروز اولین مصرف‌کننده‌اش را پیدا کرد.

# ارزان‌ترین کانال ممکن، چون هیچ‌چیزِ دیگری لازم نداشت

```
shared/channel.go     ChannelWhatsApp · Valid · ValidateAddress(E.164)   از قبل ✓
common.proto          CHANNEL_WHATSAPP                                    از قبل ✓
grpcsrv/mapper.go     هر دو جهت                                           از قبل ✓
migrations            هر دو CHECK                                         از قبل ✓
```

نه enum، نه proto، نه migration. فقط یک package و دو شاخه در registry.

# `metadata` تصمیم را به source می‌دهد

WhatsApp بیرون از پنجره‌ای که گیرنده باز کرده فقط template تأییدشده قبول می‌کند،
داخلش متن آزاد هم:

```
metadata خالی   →  متن
metadata دارد   →  template + parameters
```

و **source تصمیم می‌گیرد، نه ما.** او می‌داند گیرنده اخیراً نوشته یا نه؛ ما
نمی‌دانیم — و دانستنش یعنی گرفتن webhook از Meta و نگه‌داشتن وضعیت گفت‌وگو، که
`ARCHITECTURE.md` صریحاً ردش کرده.

سه کلید، و هیچ‌کدام در core تعریف نشده‌اند — **این adapter دنبالشان می‌گردد و هیچ
کانال دیگری تحت تأثیر نیست:**

```
template            نام
template_language   پیش‌فرض en_US
template_params     آرایهٔ JSON
```

## چرا پارامترها JSON اند

`metadata` نوعش `map[string]string` است ولی template پارامترِ **ترتیب‌دار**
می‌خواهد:

```
کلیدهای شماره‌دار   param_10 قبل از param_2 مرتب می‌شود
جدا با کاما         اولین پارامتری که کاما دارد می‌شکند
آرایهٔ JSON         داده ساختار دارد، پس encode می‌شود        ← این
```

# نشتی‌ای که فقط تماس واقعی نشانش داد

قبل از نوشتن، از خودِ API پرسیدم که شکل خطایش چیست. جواب این بود:

```json
{"error":{"message":"Malformed access token EAAG-not-a-real-token","code":190}}
```

**Meta توکن را در پیام خطای خودش پس می‌دهد** — و ما آن پیام را در `last_error`
می‌نویسیم. همان کلاس باگی که تلگرام داشت، از جهت مخالف:

```
telegram   درخواست نشت می‌داد   توکن در path است، net/http نقلش می‌کند
whatsapp   پاسخ نشت می‌دهد      توکن در header است، ولی Meta پسش می‌دهد
```

بسته شد، و روی سرویس زنده ثابت شد: `last_error` می‌گوید
`Malformed access token [REDACTED]` و توکن در کل جدول صفر بار است.

# طبقه‌بندی: status قاعده است، کدها استثنا

Meta صدها کد دارد و شل مستندشان کرده. پس http status بیشتر تصمیم را می‌گیرد و
فقط چهار کد استثنا هستند — **که همان بخشی است که به‌احتمال زیاد روزی باید عوض
شود، و برای همین استثناست نه قاعده:**

```
131047  بیرون از پنجره       →  NOT_REACHABLE
131026  روی واتساپ نیست      →  NOT_REACHABLE
130429 · 131056 · 429        →  transient
5xx                          →  transient
4xx                          →  permanent
```

آن دو تای اول اولین چیزی‌اند که در تولید `NOT_REACHABLE` تولید می‌کنند — تا دیروز
فقط ۴۰۳ در telegram و bale بود.

# Config تایپ‌دار، مثل mail

```go
whatsapp.New(client, token, Config{PhoneNumberID: "..."})
whatsapp.ParseConfig(raw)   // برای مسیر per-source
```

مثل telegram و bale نه، چون تنظیماتش **اجباری** است — همان دلیلی که email هم
این شکل است. و `phone_number_id` در **مسیر URL** است، پس الفبایش چک می‌شود؛
توکن لازم ندارد چون در header است.

# Files Changed

- `internal/adapter/sender/whatsapp/{sender,config,api,errors,const}.go` *(تازه)*
- `internal/adapter/sender/whatsapp/{sender,export}_test.go` *(تازه — ۱۶ تست)*
- `internal/adapter/sender/registry.go` *(`Fallback.WhatsApp`، دو شاخه)*
- `internal/adapter/sender/registry_test.go` *(whatsapp در جدول مشترک؛ و تستِ توخالی جایگزین شد)*
- `internal/config/settings/sender.go` *(`WhatsApp{Token, PhoneNumberID}`)*
- `internal/bootstrap/dispatcher.go`
- `.env.dispatcher.example`، `docs/CONFIG.md`

# Tests Run

- `make prepush` — سبز
- `golangci-lint run --build-tags=integration ./...` — صفر ایراد
- دستی، با تماس واقعی به `graph.facebook.com`:

```
Register  CHANNEL_WHATSAPP + phone_number_id  →  سربسته ذخیره شد
Submit    subject فارسی + metadata ــِ template
                    ↓
dispatcher → Cloud API → 401 code=190
                    ↓
whatsapp · FAILED · PERMANENT · attempts=1
last_error: Malformed access token [REDACTED]
توکن در جدول: ۰ بار
```

`PERMANENT` درست است و نه `NOT_REACHABLE`: توکن بد کانفیگِ ماست، نه گیرنده‌ای که
ما را رد کرده.

# Follow-ups / Risks

- **ارسال موفق آزموده نشد**، مثل سه کانال دیگر. یک business account واقعی
  می‌خواهد.
- **`NOT_REACHABLE` زنده از این کانال دیده نشد** — برای ۱۳۱۰۴۷ باید توکن معتبر و
  گیرنده‌ای که پنجره‌اش بسته است داشت. در تست پوشش دارد.
- **کدهای خطا شکننده‌ترین بخش‌اند.** چهار عدد، و Meta بدون اطلاع عوضشان می‌کند.
  status قاعده است تا وقتی یکی از این چهار غلط شود، جوابْ بدتر از «نامعلوم» نباشد.
- **`api_version` ثابت است.** ارتقا یک release است — که صادق است، ولی یعنی روزی
  که Meta نسخه را بازنشسته کند، این باید قبلش عوض شده باشد.

# Instruction

«whatsapp را بزن» — با سه تصمیمی که تأیید شد: template از metadata وقتی هست،
پارامترها به‌صورت آرایهٔ JSON، و نسخهٔ API ثابت در کد.
