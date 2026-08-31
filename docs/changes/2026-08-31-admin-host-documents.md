Branch: `refactor/admin-on-its-own-host`

# Summary

task سه از سه: دو سندی که هنوز شکلِ قدیمی را استدلال می‌کردند، با آنچه دو commit
قبل ساخته شد یکی شدند. هیچ کدی عوض نشده.

**`docs/ARCHITECTURE.md`**، بخشِ *Two surfaces in one binary*:

- جدولِ listener ها سه سطر شد و host هم گرفت. `:8092` دیگر
  «private, never published» نیست؛ `admin.srosha.ir` است، و زیرش نوشته شد که
  این **عمدی** است و چرا: در container، private بودن یعنی غیرقابلِ دسترس، و
  پنلی که باز نمی‌شود یعنی هیچ source ای تأیید نمی‌شود.
- بندِ آخرِ زیربخشِ *Cookies are not scoped by port* حذف نشد، تمام شد. آن بند به
  «the check below» ارجاع می‌داد که همان guard ــِ حذف‌شده بود. حالا همان
  مشخصاتِ cookie را از سمتِ دیگر می‌خواند: cookie با port اسکوپ نمی‌شود ولی با
  host **می‌شود**، و برای همین جداکننده host شد.
- جملهٔ «The admin port is never published» جایش را به توضیحِ cookie و به این
  داد که مرز حالا **چهار** چیز است نه یکی: سه handler بدونِ mux مشترک، خواندنِ
  زندهٔ نقش، cookie به ازای هر host، و تست‌ها.
- فهرستِ «What must be tested» از دو تست به چهار رسید، به‌همراه دلیلِ آن درسی که
  دو بار یاد گرفته شد: تستِ اولِ cookie مکانیزم را می‌سنجید و سیم‌کشی را نه.

**`docs/CONFIG.md`**، جدولِ پورت‌ها: هر سه host به سطرهایشان اضافه شد
(`api.srosha.ir`، `panel.srosha.ir`، `admin.srosha.ir`)، و آن پاراگراف که
می‌گفت فقط portal از بیرون قابلِ دسترس است و «Its admin port stays on the
private network only» بازنویسی شد.

# Files Changed

- `docs/ARCHITECTURE.md` *(چهار جای بخشِ Two surfaces)*
- `docs/CONFIG.md` *(سه سطر از جدولِ پورت‌ها و پاراگرافِ زیرش)*

# Tests Run

- `make precommit` — pass *(سند است)*

# Follow-ups / Risks

- **یک سند پیدا شد که قبلاً ندیده بودم:** `docs/reference/srosha-infra-deploy.md`
  با ۶۵۶ خط، که `docs/CONFIG.md` در سطرِ ۱۳ می‌گوید «facts about the running
  system» و «infrastructure that is already deployed and verified». فهرستش
  بخش‌هایی دارد به‌نامِ *What is already live*، *Networking model and its traps*،
  یک `docker-compose.yml`، یک Dockerfile، و *Migrations under auto-deploy*.
  یعنی postgres و NATS از قبل روی سرور بالا هستند و تله‌های شبکه از قبل کشف
  شده‌اند. **plan ــِ قسمت B باید قبل از شروع با این سند بازبینی شود** — بخشی از
  آنچه به‌عنوانِ کارِ تازه نوشتم شاید از قبل تصمیم گرفته یا حتی انجام شده باشد.
  این را به مالک گزارش کردم و دست به plan نزدم.
- `docs/CONFIG.md` بخشِ Deployment هنوز می‌گوید «one image, **both** binaries» و
  «Domain on the gateway service only». عمداً دست‌نخورده: plan ــِ B آن را
  همراهِ خودِ compose اصلاح می‌کند، و حالا با سندِ بالا هم باید تطبیق داده شود.

# Instruction

آخرین task از plan ــِ `refactor/admin-on-its-own-host`: سندهایی که برای شکلِ
قبلی استدلال می‌کردند با وضعیتِ جدید یکی شوند.
