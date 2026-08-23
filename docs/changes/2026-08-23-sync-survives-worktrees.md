Branch: `feat/api-key`

# Summary

`make sync` بعد از هر merge با خطا تمام می‌شد:

```
error: cannot delete branch 'chore/mit-license' used by worktree at
       '/Users/.../srosha-license-wt'
Deleted branch feat/postgres-repositories (was 20f6ddc).
make: *** [sync] Error 1
```

دو چیز اینجا اتفاق می‌افتاد.

**اول، git حق داشت.** یک branch که یک worktree دیگر آن را checkout کرده قابل حذف
نیست — اگر حذف می‌شد، آن worktree روی یک ref مرده می‌ماند.

**دوم، Makefile بد واکنش نشان می‌داد.** حذف با `xargs -n1 git branch -d` انجام
می‌شد، و شکست روی یک branch کل target را می‌کشت:

```
xargs → git branch -d A   ✓
        git branch -d B   ✗  ← worktree
        git branch -d C   ✓
                          ↓
        exit != 0  →  make: *** [sync] Error 1
```

بقیهٔ کار درست انجام شده بود (`feat/postgres-repositories` پاک شد) ولی خروجی
می‌گفت شکست خورد، و مرحلهٔ بعدی — گزارش branch های gone — اصلاً اجرا نشد.

## راه حل

`sync` حالا قبل از حذف می‌پرسد کدام branch ها در worktree ای checkout شده‌اند و
آن‌ها را کنار می‌گذارد:

```
git worktree list --porcelain  →  branch refs/heads/<name>
                    ↓
merged  منهای  held  =  deletable   →  حذف می‌شود
merged  اشتراک  held  =  inuse      →  گزارش می‌شود، حذف نمی‌شود
```

و برای هرکدام دستور دقیقش را چاپ می‌کند:

```
⚠️  Merged, but a worktree has them checked out, so git cannot delete them:
     chore/mit-license
   Drop the worktree first, then run make sync again:
   git worktree remove /Users/.../srosha-license-wt
```

worktree خودِ مخزن از این پیشنهاد کنار گذاشته می‌شود — بعد از `git checkout
master` آن همیشه `master` است و از قبل فیلتر شده، ولی اگر روزی نبود، پیشنهاد
«worktree اصلی را حذف کن» غلط بود.

# Files Changed

- `Makefile` *(بلوک حذف branch در target `sync`)*

# Tests Run

- بلوک با `sh -c` جدا اجرا شد (چون `sync` روی working tree کثیف عمداً متوقف
  می‌شود): `deletable` خالی، `inuse` هر دو branch را گرفت، و مسیر worktree درست
  چاپ شد
- `zsh` تقسیم کلمه روی `$inuse` انجام نمی‌دهد ولی make با `/bin/sh` اجرا می‌کند،
  که می‌کند — تست با `sh` همین را تأیید کرد

# Follow-ups / Risks

- روی `feat/api-key` commit شد، نه یک branch جدا: تغییر یک بلوک از Makefile است و
  کشیدن یک branch برای آن، خودش یک worktree دیگر و همان مشکل بود.
- `chore/mit-license` merge شده و worktree اش تمیز است، پس آن worktree کارش تمام
  است و می‌شود حذفش کرد. این تصمیم صاحب مخزن است، نه این تغییر.

# Instruction

«چرا هر بار merge می‌کنم و `make sync` می‌زنم این خطا را می‌دهد؟»
