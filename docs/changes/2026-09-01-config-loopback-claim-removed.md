Branch: `docs/config-matches-the-code`

# Summary

`docs/CONFIG.md` دربارهٔ یک guard امنیتی حرف می‌زد که دیگر وجود ندارد. حذف شد و جایش
آنچه واقعاً درست است نوشته شد. **هیچ کدی عوض نشده.**

## چه چیزی غلط بود

دو پاراگراف، و هر دو همان استدلالِ باطل‌شده را داشتند:

> **In production the console refuses to start unless `NOTIF_ADMIN_ADDR` binds
> loopback** — `127.0.0.1`, `::1` or `localhost`.

و بالاترش:

> ... so the admin port staying unreachable from outside is a property of the
> process and not only a fact about how it is deployed.

آن check در `2026-08-31-admin-listener-guard-removed.md` حذف شد و `bindsLoopback`
دیگر در کد نیست — چک شد، هیچ‌جای `internal/` نیست. یعنی سندی که قرار است تنها منبعِ
حقیقتِ این repository باشد، دربارهٔ یک مرزِ امنیتی چیزی می‌گفت که هیچ‌کس اجرا نمی‌کند.

بدترین شکلِ غلط‌بودن هم همین است: کسی که این را بخواند فکر می‌کند یک نگهبان جلوی
اشتباهش را می‌گیرد، و نمی‌گیرد.

## چه چیزی جایش نشست

سه چیز:

**پیش‌فرض یک پیش‌فرض است.** `127.0.0.1:8092` برای لپ‌تاپ درست است و در container
**غلط** — loopback آنجا namespace ــِ خودِ container است و به هیچ‌جا نمی‌رسد. همان
اشتباهی که یک بار پنل را غیرقابل‌بازکردن کرد: هیچ source ای تأیید نمی‌شد، پس هیچ پیامی
فرستاده نمی‌شد.

**هیچ‌چیز این مقدار را چک نمی‌کند، و این عمدی است.** نوشته شد که guard وجود داشت و چرا
برداشته شد، نه اینکه فقط جمله پاک شود — نفر بعدی که فکر کند «اینجا یک چک لازم است»
باید دلیلِ نبودنش را همان‌جا ببیند.

**آنچه واقعاً مشتری را بیرون نگه می‌دارد**، که شبکه نیست: cookie ای که به
`admin.srosha.ir` محدود است، role ای که در هر request از ردیفِ زنده خوانده می‌شود، و سه
handler بدونِ mux ــِ مشترک. با ارجاع به `docs/ARCHITECTURE.md` که استدلالِ کاملش آنجاست.

# Files Changed

- `docs/CONFIG.md` *(دو پاراگرافِ غلط با سه پاراگرافِ درست جایگزین شدند)*

# Tests Run

- `make precommit` — pass
- `grep -rn "bindsLoopback" internal/` — هیچ. تأیید اینکه ادعا واقعاً باطل بود.
- هیچ ادعای loopback ای در `CONFIG.md` و `ARCHITECTURE.md` باقی نمانده

# Follow-ups / Risks

- این وقتی پیدا شد که برای task ــِ credential trial داشتم همین فایل را ویرایش می‌کردم،
  نه از یک بازبینیِ منظم. یعنی جاهای دیگری هم که یک تصمیم برگشته و سند نمانده ممکن است
  باشد و کسی نمی‌داند. راهی جز خواندن نیست.

# Instruction

آن دو غلطی که در گزارشِ credential trial به‌عنوان follow-up ثبت شدند را درست کن. این
اولی است: ادعای `CONFIG.md` دربارهٔ guard ــِ loopback که در کد نیست.
