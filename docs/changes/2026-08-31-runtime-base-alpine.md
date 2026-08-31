Branch: `fix/runtime-base-reachable`

# Summary

image ــِ runtime از `gcr.io/distroless/static-debian12:nonroot` به
`alpine:3.22` عوض شد.

اولین deploy ــِ واقعی روی سرور شکست:

```
gcr.io/v2/distroless/static-debian12/manifests/nonroot: 403 Forbidden
```

سرور به **gcr.io** دسترسی ندارد. Docker Hub کار می‌کند — همان build مرحلهٔ
`golang:1.26-alpine` را بدونِ مشکل آورد — ولی رجیستریِ گوگل نه.

انتخابِ distroless روی merits درست بود: بدونِ shell، بدونِ package manager،
سطحِ حمله‌ی کمتر. ولی base image ای که سرورِ deploy نمی‌تواند pull کند، انتخاب
نیست. این تصمیم را شبکه گرفت، نه معماری.

# چیزی که از دست رفت و چیزی که نرفت

**از دست رفت:** نبودنِ shell. حالا `docker exec … sh` کار می‌کند، که هم برای
اشکال‌زدایی خوب است و هم یک در است که قبلاً بسته بود.

**از دست نرفت:** زیردستورِ `healthcheck`. دلیلِ اصلی‌اش هیچ‌وقت «alpine ــِ
wget ندارد» نبود — دلیلش این بود که همان `/readyz` ای را می‌پرسد که یک
orchestrator می‌پرسد، پس چک نمی‌تواند از معنیِ واقعیِ readiness جدا بیفتد.
`wget` ــِ busybox فقط نیمهٔ ضعیف‌ترش را برمی‌گرداند.

`ca-certificates` و `tzdata` حالا با `apk` نصب می‌شوند — distroless خودش
داشتشان. کاربرِ غیرroot هم دستی ساخته می‌شود (`adduser -D -u 10001 srosha`).

اندازه: ۱۲۱MB → **۱۲۹MB**. هشت مگابایت بابتِ یک سیستمِ فایلِ کامل.

# Files Changed

- `deployment/app/Dockerfile` *(مرحلهٔ runtime)*
- `docs/CONFIG.md` *(سطرِ Runtime base)*
- `docs/superpowers/plans/2026-08-31-deployment-stack.md` *(بندِ «Distroless, decided» با دلیلِ برگشت)*

# Tests Run

- `docker build` — موفق
- `/app/goose -version` → `v3.27.3`
- `/app/gateway healthcheck` → `exit=1` با «NOTIF_DB_DSN: is required»
- `docker image inspect` → `user=srosha`، ۱۲۸۹۷۵۵۹۵ بایت
- محتوا شمرده شد: سه باینری، goose، و **۱۱** فایلِ migration
- `id` داخلِ container → `uid=10001(srosha)`، یعنی root نیست
- `make precommit` — pass

# Follow-ups / Risks

- اگر روزی distroless لازم شد، راهش mirror کردنِ آن image در رجیستری‌ای است که
  سرور می‌بیند — نه برگرداندنِ `FROM`.
- حالا داخلِ container shell هست. چیزی را نمی‌شکند، ولی یک فرضِ امنیتی که در
  spec نوشته شده بود دیگر برقرار نیست و باید همان‌جا هم اصلاح شود اگر کسی به آن
  تکیه کرد.

# Instruction

deploy روی سرور شکست چون base image قابلِ pull نبود. مالک لاگ را فرستاد و
اصلاحش خواسته شد.
