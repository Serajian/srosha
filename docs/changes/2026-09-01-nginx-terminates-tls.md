Branch: `fix/nginx-terminates-tls`

# Summary

روی سرورِ تازه، **nginx** پورت‌های ۸۰ و ۴۴۳ را دارد، نه Traefik. کلِ compose بر این
فرض نوشته شده بود که Traefik جلوی کار است. سه router به entrypoint ای وصل بودند که
هیچ‌کس درش را نمی‌زند. **هیچ کدِ Go ای عوض نشده.**

## علامت، و چرا گمراه‌کننده بود

`panel.srosha.ir` جوابِ `404` می‌داد در حالی که هر چیزی که نگاه می‌کردیم سالم بود:

```
router  srosha-panel@docker   Host(`panel.srosha.ir`)   status: enabled
service srosha-panel@docker   http://10.0.1.227:8090    UP
اپ       GET / روی 8090        303 See Other → /signin
```

router بالا، سرویس بالا، backend زنده، اپ درست. و هیچ ترافیکی نمی‌رسید.

جوابش در `docker port dokploy-traefik` بود:

```
80/tcp -> 127.0.0.1:8000
```

Traefik اصلاً ۴۴۳ را منتشر نکرده، و ۸۰ را هم فقط روی loopback. `ss -tlnp` گفت روی
۸۰ و ۴۴۳ **nginx** نشسته. یعنی معماریِ واقعیِ این میزبان این است:

```
اینترنت ──► nginx :443 ──► 127.0.0.1:8000 ──► Traefik(web) ──► کانتینر
            TLS اینجا تمام می‌شود
```

نشانه‌ای که از اول جلوی چشم بود و ندیدمش: از بینِ ده‌ها router روی این سرور، **فقط
مالِ ما روی `websecure` بود**. بقیه همه `web` اند، چون TLS را nginx تمام می‌کند.

## چه چیزی عوض شد

**console** — دو router از `websecure` به `web`، و `tls.certresolver` حذف شد. گواهی
کارِ nginx است؛ یک resolver اینجا جوابِ دومی بود به سؤالی که جواب دارد.

**gateway** — router ــِ Traefik کلاً برداشته شد و به‌جایش:

```yaml
ports:
  - "127.0.0.1:50051:50051"
```

چون `grpc_pass` ــِ nginx با HTTP/2 ــِ خام به backend وصل می‌شود و entrypoint ــِ
`web` ــِ Traefik فقط HTTP/1.1 است. روشن کردنِ h2c رویش یعنی دست بردن در
`traefik.yml` ای که با هر اپِ دیگری روی این میزبانِ مشترک شریک است. پس `api` از
Traefik رد نمی‌شود.

آدرسِ کانتینر گزینهٔ دیگر بود و رد شد: با هر redeploy عوض می‌شود و پیکربندیِ nginx
بی‌صدا کهنه می‌شود.

**gateway از `dokploy-network` برداشته شد.** آنجا بود تا Traefik بهش برسد؛ دیگر
هیچ‌چیز از آنجا بهش نمی‌رسد، و شبکه‌ای که لازم نیست یعنی یک راهِ اضافه از هر اپِ
دیگری روی این میزبانِ مشترک. حالا مثلِ dispatcher فقط `srosha-net` دارد.

## `docs/CONFIG.md`

سه چیزِ غلط شده بود:

- «Nothing publishes a host port» — دیگر درست نیست، یکی هست و عمدی است
- سطرِ `api.srosha.ir` می‌گفت «the router sets `scheme=h2c`» — دیگر router ای نیست
- هیچ‌جا ننوشته بود nginx وجود دارد، که مهم‌ترین واقعیتِ مسیرِ ورود است

جدولِ hostname ها یک ستونِ «Through» گرفت و زیرش نوشته شد که `websecure` روی این
میزبان دری است که کسی نمی‌زند — تا نفرِ بعدی همان بعدازظهر را از دست ندهد.

# Files Changed

- `docker-compose.yml` *(entrypoint ها، حذفِ router ــِ api، پورتِ loopback، شبکهٔ gateway، و سرصفحه‌ای که مسیرِ واقعی را می‌کشد)*
- `docs/CONFIG.md` *(جدولِ hostname با ستونِ مسیر، و جدولِ پورت‌ها)*

# Tests Run

- `docker compose config` — valid
- `make precommit` — pass
- خودِ تشخیص روی سرور تأیید شد: `docker port`، `ss -tlnp`، و API ــِ Traefik

# Follow-ups / Risks

- **این تغییر به‌تنهایی کافی نیست.** nginx هنوز هیچ server block ای برای این سه نام
  ندارد، و بدونِ آن درخواست اصلاً به Traefik نمی‌رسد. گواهیِ certbot و سه server
  block کارِ بعدی است — و بیرون از این repository، که خودش یک ریسک است: نصفِ مسیرِ
  ورود جایی زندگی می‌کند که هیچ commit ای لمسش نمی‌کند.
- `docs/reference/srosha-infra-deploy.md` هنوز میزبانِ قبلی را توصیف می‌کند، جایی که
  Traefik جلوی کار بود و tunnel در مسیر. کلِ بخشِ شبکه‌اش باید بازنویسی شود.
- توگلِ gRPC ــِ Cloudflare و حالتِ SSL هنوز چک نشده‌اند. بدونِ اولی هر درخواستِ gRPC
  جوابِ `403` می‌گیرد و بدونِ حداقل `Full` اصلاً کار نمی‌کند.

# Instruction

سرور عوض شد و `panel.srosha.ir` جوابِ ۴۰۴ می‌داد. علتش را پیدا کن و compose را با
میزبانی که واقعاً روی آن اجرا می‌شود یکی کن.
