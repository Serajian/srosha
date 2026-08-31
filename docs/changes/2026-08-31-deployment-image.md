Branch: `feat/deployment-stack`

# Summary

`deployment/app/Dockerfile` و `.dockerignore` نوشته شدند. task دو از چهار.

یک image با سه باینری به‌علاوهٔ goose و پوشهٔ `migrations/`. انتخابِ باینری با
`command` در compose است — سه image که فقط در entrypoint فرق کنند یعنی سه build
از یک کد و سه فرصت برای اینکه از commit های متفاوت ساخته شوند.

مرحلهٔ build روی `golang:1.26-alpine`، و runtime روی
`gcr.io/distroless/static-debian12:nonroot` — بدونِ shell، بدونِ package
manager، بدونِ wget. همان چیزی که task یک ممکنش کرد.

باینری‌ها static اند (`CGO_ENABLED=0`) و strip شده (`-ldflags="-s -w"`) و
`-trimpath` دارند تا مسیرهای ماشینِ build داخلشان نماند.

`public/` کپی **نمی‌شود**. `go:embed` قالب‌ها و asset ها را داخلِ باینریِ console
کامپایل می‌کند؛ یک کپیِ دوم یعنی چیزی که می‌تواند با اولی اختلاف پیدا کند.

# سه چیزی که plan غلط گفته بود و build گرفت

**۱. جای `.dockerignore`.** plan می‌گفت `deployment/app/.dockerignore`. docker
آن را از **ریشهٔ build context** می‌خواند نه از کنارِ Dockerfile، و context اینجا
`../..` است. آن فایل اصلاً خوانده نمی‌شد. به ریشه منتقل شد، با کامنتی که همین را
توضیح می‌دهد.

**۲. `sdk/` نباید حذف شود.** plan نوشته بود «ماژولِ جداست و هیچ باینری import اش
نمی‌کند». **هر دو نیمه غلط بود.** `go.mod` ــِ ریشه این را دارد:

```
replace github.com/Serajian/srosha/sdk/go => ./sdk/go
```

و کدِ تولیدشدهٔ protobuf که `internal/adapter/api/grpcsrv` مستقیم import می‌کند
داخلِ همان ماژول است. build با `open /src/sdk/go/go.mod: no such file or
directory` شکست، قبل از اینکه یک خط کامپایل شود.

**۳. لایهٔ dependency هم به `sdk/go/go.mod` نیاز داشت.** حتی بعد از اصلاحِ
`.dockerignore` باز شکست، چون در آن لایه فقط `go.mod` و `go.sum` کپی شده بودند.
یک `COPY sdk/go/go.mod sdk/go/go.sum ./sdk/go/` اضافه شد — فقط همان دو فایل، تا
لایه هنوز روی تغییرِ dependency کش شود نه روی هر ویرایشِ sdk.

هر سه در خودِ plan هم اصلاح شدند، نه فقط اینجا، تا اجراکنندهٔ بعدی همان اشتباه
را تکرار نکند.

# و یک چیز که موقعِ اندازه‌گیری دیده شد

goose ــِ ۵۳ مگابایتی از هر سه باینریِ ما بزرگ‌تر بود، چون `go install` از
ldflags ــِ ما استفاده نمی‌کند. `-ldflags="-s -w"` به آن هم داده شد: image از
۱۳۸MB به **۱۲۱MB** رسید.

انتظارِ اندازه در plan («tens of megabytes, not hundreds») هم یک حدس بود و با
عددِ واقعی جایگزین شد. سه باینریِ Go هرکدام ۲۴ تا ۲۸ مگابایت‌اند؛ distroless
base image را حذف می‌کند، نه باینری‌ها را.

# Files Changed

- `deployment/app/Dockerfile` *(جدید)*
- `.dockerignore` *(جدید — در ریشه، نه کنارِ Dockerfile)*
- `docs/superpowers/plans/2026-08-31-deployment-stack.md` *(سه اصلاحِ بالا)*

# Tests Run

- `docker build` — موفق
- `docker run --rm srosha:latest /app/goose -version` → `goose version: v3.27.3`
- `docker run --rm srosha:latest /app/gateway healthcheck` → `exit=1` با پیامِ
  «NOTIF_DB_DSN: is required» — یعنی باینری هست، اجرا می‌شود، و مسیرِ healthcheck
  کار می‌کند. صفر بودن اینجا **غلط** می‌بود: چیزی گوش نمی‌دهد.
- `docker image inspect` → `user=nonroot`، `size=121542586`
- محتوا با `docker cp` بیرون کشیده و شمرده شد: سه باینری، goose، و **۱۱** فایلِ
  migration.
- `docker run --entrypoint /bin/sh` → `stat /bin/sh: no such file or directory`.
  یعنی distroless واقعاً shell ندارد و healthcheck ــِ task یک لازم بوده.
- `make precommit` — pass

# Follow-ups / Risks

- `docker create` بدونِ دستور کار نکرد (`No command specified`) — که خودش تأییدِ
  تصمیمِ Dockerfile است که نه `ENTRYPOINT` دارد نه `CMD`. اگر روزی کسی یکی
  اضافه کند، این رفتار بی‌صدا عوض می‌شود.
- image روی این ماشین برای معماریِ خودش ساخته شد. سرور معماریِ دیگری دارد؛
  Dokploy خودش روی سرور build می‌کند، پس مسئله نیست — ولی این image ــِ محلی
  برای deploy مناسب نیست.

# Instruction

task دو از plan ــِ deployment: یک image با هر سه باینری و goose، روی runtime ــِ
distroless که مالک انتخاب کرد.
