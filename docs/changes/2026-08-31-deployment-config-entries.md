Branch: `feat/deployment-stack`

# Summary

`docs/CONFIG.md` با آنچه سه commit قبل ساخته شد یکی شد. task چهار از چهار،
**ناتمام** — قدمِ اولش انجام نشد و دلیلش پایین آمده.

**جدولِ Deployment.** دو سطر غلط بود: «one image, **both** binaries» (سه‌تاست،
به‌علاوهٔ goose و `migrations/`) و «Domain on the **gateway service only**»
(console دو host دارد). جایشان سه host به‌عنوانِ داده نشست، به‌همراهِ دو سطرِ
تازه: جای `.dockerignore` (ریشهٔ مخزن، چون build context آنجاست) و runtime base
که distroless است و shell ندارد — که همان دلیلِ وجودِ زیردستورِ `healthcheck`
است.

و یک جملهٔ صریح اضافه شد که compose **فقط اپ** را deploy می‌کند، چون این دقیقاً
همان اشتباهی بود که نسخهٔ اولِ plan داشت.

**جدولِ Networks.** می‌گفت `srosha-net` مالِ «both binaries» است و
`dokploy-network` «Gateway only». هر دو کهنه بودند. مهم‌تر اینکه دلیلش نوشته
نشده بود: صریح نوشتنِ هر دو شبکه سلیقه نیست — وصل شدنِ domain باعث می‌شود
Dokploy شبکهٔ سرویس را **جایگزین** کند نه اضافه، پس سرویسی که فقط `srosha-net`
دارد دیتابیس و broker اش را از دست می‌دهد بدونِ هیچ خطای deploy.

**بخشِ Migrations.** مکانیزمِ واقعی ثبت شد: سرویسِ `migrate` زیرِ profile، با
دستورِ `docker compose --profile migrate run --rm migrate`، از همان image، با
goose ــِ pin شده روی `v3.27.3`. و اینکه باید **قبل از** merge شدنِ کدی اجرا شود
که به آن migration وابسته است — با auto-deploy هیچ پنجره‌ای نیست که مطمئن باشیم
کدِ قدیمی رفته. تفاوتش با `make setup-dev` که `@latest` نصب می‌کند هم نوشته شد.

**چهار نامِ تازه** که در compose ساخته شدند بخشِ خودشان را گرفتند، با این قید که
کلیدِ برنامه **نیستند** — نامِ جایگزینی‌اند، چون یک کلیدِ برنامه به ازای هر
سرویس مقدارِ متفاوتی می‌خواهد.

# قدمی که انجام نشد

قدمِ ۱ ــِ این task قرار بود کامنتِ `AdminAddr` در
`internal/config/settings/console.go` را اصلاح کند، که هنوز می‌گوید
«Never published» و «staying off the network is a property of the process».

**روی این branch آن کامنت هنوز درست است.** این branch از `master` جدا شده و
`refactor/admin-on-its-own-host` هنوز merge نشده، پس guard ــِ loopback در
master هنوز سرِ جایش است. اصلاحِ کامنت اینجا آن را **دروغ** می‌کند، نه راست.

پس دست نخورد. بعد از merge شدنِ آن branch باید انجام شود.

# Files Changed

- `docs/CONFIG.md` *(Deployment، Networks، Migrations، و بخشِ تازهٔ نام‌های compose)*

# Tests Run

- `make precommit` — pass *(سند است)*

# Follow-ups / Risks

- **کامنتِ `AdminAddr` هنوز باید اصلاح شود**، بعد از merge شدنِ
  `refactor/admin-on-its-own-host`. تا آن موقع در master درست است و در آن
  branch غلط.
- جدولِ پورت‌ها در همین فایل توسطِ آن branch عوض شده (host ها اضافه شده‌اند).
  بخش‌های متفاوت‌اند، پس merge باید تمیز باشد، ولی هر دو یک فایل را دست می‌زنند.
- `nats-server.conf` همچنان بیرون از مخزن است.

# Instruction

task چهار از plan ــِ deployment: `docs/CONFIG.md` حقیقت را دربارهٔ deployment
بگوید — سه باینری، سه host، و compose ای که زیرساخت را دوباره deploy نمی‌کند.
