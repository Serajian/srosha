Branch: `master`
*(کار مستقیم روی base branch انجام شد، چون قانون برنچ در همین تغییر تازه نوشته شد.)*

# Summary

فایل `CLAUDE.md` ساخته شد و `docs/CONVENTIONS.md` نوشته شد. تقسیم کار بین این دو
عمدی است: `CLAUDE.md` فقط اشاره می‌کند و هیچ قانونی از خودش ندارد، و همهٔ قانون‌ها
در `docs/CONVENTIONS.md` می‌نشینند. خود `CLAUDE.md` این را به‌صورت یک قانون نوشته
است: هر چیزی که جنس convention دارد باید به آن فایل اضافه شود، نه به این یکی.

دو استثنا در `CLAUDE.md` نگه داشته شد: گیت commit و گیت push. دلیلش این است که
`CLAUDE.md` همیشه بارگذاری می‌شود ولی `CONVENTIONS.md` باید فعالانه خوانده شود، و
گیتی که جلوی commit بدون اجازه را می‌گیرد نباید به «یادم رفت آن فایل را بخوانم»
وابسته باشد. متن هر دو گیت در هر دو فایل یکی است.

قانون‌های `CONVENTIONS.md` شامل این‌هاست: گیت commit و push، operational knobs در
config، binding بودن معماری، گزارش تغییرات، skillهای مرجع Go، اندازه و شکل port،
اینکه adapter فقط fact برمی‌گرداند و core تصمیم می‌گیرد، context و cancellation،
مرز transaction، ممنوعیت dual-write، تست، سیاست شکست consumer، یک entity در هر
domain، جای کد جدید، ترفیع به `internal/core/shared/model/`، جای validation،
observability، خطاها، base branch، نام‌گذاری برنچ، و زبان پاسخ.

سه بخش از قالب اولیه حذف شد چون به این پروژه ربط نداشت: مالکیت یک دایرکتوری توسط
تیم دیگر، کار با issue tracker، و مرز `<AXIS>` (این پروژه چنین محوری ندارد؛
نزدیک‌ترین چیز `channel` است که طبق SPEC در sender adapter می‌نشیند نه در domain).

قانون نام‌گذاری برنچ هم نوشته شد. شکلش `<type>/<slug>` است با همان مجموعهٔ بستهٔ
Conventional Commits، و دو قید که ارزششان از خود الگو بیشتر است: slug برنچ و slug
گزارش تغییر یکی است، و یک برنچ یعنی یک تغییر.

`docs/changes/TEMPLATE.md` هم نوشته شد. نسبت به متنی که رسیده بود خط
`Task: <PROJECT>-<n>` حذف شد چون tracker نداریم، و سه دستور تست با مقدار واقعی
Go پر شد.

# Files Changed

- `CLAUDE.md` *(ساخته شد — اشاره‌گر، به‌علاوهٔ دو گیت commit و push)*
- `docs/CONVENTIONS.md` *(ساخته شد — همهٔ قانون‌ها)*
- `docs/changes/TEMPLATE.md` *(ساخته شد — قالب همین گزارش‌ها)*

# Tests Run

هیچ کد Go تغییر نکرد، پس build و lint و test موضوعیت نداشتند.

# Follow-ups / Risks

- چند قانون در `CONVENTIONS.md` با SPEC تعارض دارد و هنوز حل نشده: outbox در برابر
  dual-write با reconciler، نام دایرکتوری‌ها (`infrastructure/` در برابر
  `internal/infra/`)، ممنوعیت package به نام `shared` در حالی که
  `internal/core/shared/` وجود دارد، یک entity در هر domain در حالی که
  `notification` هم `Notification` دارد هم `Delivery`، و شکل port.
- واژگان «partition key» مال Kafka است؛ این پروژه روی NATS JetStream است.
- با حذف بخش tracker، هیچ قانونی برای اینکه کار از کجا شروع شود باقی نماند.

# Instruction

مالک پروژه متن قانون‌ها را داد و خواست در repository نوشته شود. جای نوشتن را خودم
تعیین کردم بر اساس قانونی که خودش در پیام قبلی نوشته بود («این فایل فقط اشاره
می‌کند»)، و تکرار دو گیت در `CLAUDE.md` را به‌عنوان یک تصمیم ایمنی توضیح دادم و
تأیید نشد که برداشته شود. placeholderهای قالب یکی‌یکی با او چک شد و مقدارشان را
او داد.
