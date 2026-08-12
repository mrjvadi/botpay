# botpay — چه چیزی نیاز داریم (بررسی ۲۰۲۶-۰۷-۰۴، فقط بخش مرتبط با revenue-service)

> این فایل توسط بازبینی‌کننده‌ی سرویس‌های ads-bot / admanager-bot /
> community-service / fraud-engine / revenue-service نوشته شده — تمرکز
> صرفاً روی یک یافته‌ی متقابل بین botpay و revenue-service است، نه یک
> ممیزی کامل خود botpay.

## وضعیت فعلی (خلاصه‌ی واقعی، نه قدیمی)

botpay طبق طراحی عمداً REST API ندارد. `botpay/cmd/main.go:127-129` این را
با کامنت خودش تأیید می‌کند: «همه‌ی سرویس‌ها برای موجودی/پرداخت فقط از این
طریق [NATS request/reply] با botpay حرف می‌زنند... REST API حذف شده —
ارتباط بین‌سرویسی کاملاً روی NATS است.» بازبینی فرش کل درخت
`botpay/internal/**` این را تأیید می‌کند:
`grep -rn "gin\.\|net/http\|ListenAndServe" botpay --include="*.go"` تنها
یک نتیجه برمی‌گرداند (`botpay/internal/ton/watcher.go`) که یک
`http.Client` خروجی برای واکشی تراکنش‌های TON از یک API بیرونی است، نه یک
سرور HTTP. مسیر واقعی موجودی/کسر/اعتبار از طریق
`botpay/internal/payresponder/responder.go:70-80` روی NATS subjects
(`protocol.SubjPayBalance`, `SubjPayAuthorize`, `SubjPayDeduct`,
`SubjPayCredit`, ...) پیاده‌سازی شده. این معماری خودش سالم و به‌طور
مستقل کامل است.

## چیزی که واقعاً کم است (با file:line برای هر ادعا)

**مصرف‌کننده‌ای در مونورپو وجود دارد که هنوز فرض می‌کند botpay یک REST API
روی `/api/v1/pay/*` دارد — و آن مصرف‌کننده به‌طور واقعی و فعال، در
دیپلوی جاری، درخواست‌های ناموفق می‌فرستد.**

`revenue-service/cmd/main.go:136-167` یک `botpayClient` دارد که POST به
`http://botpay:8087/api/v1/pay/credit/add` و
`http://botpay:8087/api/v1/pay/deduct` می‌زند (آدرس از `.env:39`:
`BOTPAY_URL=http://botpay:8087`). چون botpay هیچ سرور HTTP روی هیچ پورتی
اجرا نمی‌کند (فقط `metrics.ServeMetrics(":9091")` در
`botpay/cmd/main.go:181` که مسیر متفاوتی است، صرفاً Prometheus)، این
درخواست‌ها با connection refused شکست می‌خورند. جزئیات کامل زنجیره‌ی اثر
(چطور این باعث می‌شود سهم owner/platform در revenue-service برای همیشه
`EarningFailed` بماند) در `revenue-service/NEEDS.md` نوشته شده — اینجا
فقط از زاویه‌ی «شما (botpay) یک مصرف‌کننده‌ی شکست‌خورده دارید» ثبت می‌شود.

**پیشنهاد رفع (مسئولیت اصلی سمت revenue-service است، نه botpay):**
`revengine.PayClient` در `revenue-service/internal/engine/engine.go:14-18`
یک interface دومتدی ساده است (`Deduct`, `AddCredit`)؛ جایگزینی
پیاده‌سازی HTTP آن با `shared/natspayclient` (همان چیزی که ads-bot
در `ads-bot/internal/tgbot/handler.go:12` استفاده می‌کند) کافی است — نیازی
به تغییر در خود botpay یا اضافه‌کردن دوباره‌ی REST API نیست. تنها نکته‌ی
سمت botpay: مطمئن شوید `revenue-service` هم مثل بقیه‌ی سرویس‌های مرکزی در
لیست سرویس‌های مجاز `ServiceHMACSecret`/`ComputeServiceKey` قرار دارد،
همان‌طور که قبلاً برای ads-bot یک‌بار فراموش شده بود (رجوع به CLAUDE.md،
بخش «لیست سرویس‌های مجاز در botpay هاردکد است»).

## این‌ها را در سرویس‌های دیگر هم نوشتم (اگر مرتبط بود)

- **revenue-service/NEEDS.md** — نسخه‌ی کامل این یافته با زنجیره‌ی اثر
  کامل (کد دقیق، رفتار `MarkFailed`/`ListPendingEarnings`، و حالت بدتر
  اگر `BOTPAY_URL` خالی باشد) آنجا نوشته شده.

---

## نیازهای خودِ botpay (اضافه‌شده در بررسی امنیتی ۲۰۲۶-۰۷-۰۲، هنوز باقی‌مانده)

این موارد این‌جا تکرار می‌شوند تا کسی که فقط repository
botpay را باز می‌کند هم ببیندشان:

1. **`CreateWithdraw` یک TOCTOU race دارد** (`botpay/internal/store/withdraw_repo.go`): چک موجودی
   (`HasEnough`) در `wallet.RequestWithdraw` و افزایش `frozen` در `CreateWithdraw` در یک تراکنش/قفل
   واحد نیستند — دو درخواست برداشت هم‌زمان از یک کاربر می‌توانند هر دو رد شوند و `frozen` را بیشتر از
   موجودی واقعی بالا ببرند. رفع: چک موجودی باید داخل همان تراکنشی باشد که `frozen` را با
   `SELECT ... FOR UPDATE` قفل می‌کند (همان الگویی که `Deduct`/`Transfer` از قبل دارند).
2. **`TONToNano` (`internal/wallet/wallet.go`) مقدار را truncate می‌کند، نه round** — برای مبالغ خیلی
   دقیق (چند رقم اعشار) می‌تواند چند nano-TON گم کند. `validAmount` (اضافه‌شده در رفع باگ ServiceKey) فقط
   سقف/متناهی‌بودن را چک می‌کند، نه دقت گرد‌کردن.
3. **NATS بدون ACL سطح-subject** — بزرگ‌ترین ریسک ریشه‌ای باقی‌مانده‌ی کل پلتفرم، نه فقط botpay؛ چون
   botpay مقصد نهایی همه‌ی پول است، بیشترین آسیب را از این ضعف می‌بیند. رجوع به
   سیاست مدیریت secretهای همین repository.

## به‌روزرسانی ۲۰۲۶-۰۷-۰۶: جداسازی دیتابیس
`botpay` حالا دیتابیس مخصوص خودش (`botpay`) را دارد — دیگر با `botmanager`/`ads-bot`/
`community-service`/`revenue-service`/`license-service`/`image-registry` مشترک نیست (همان سرور
Postgres، دیتابیس جدا؛ رجوع `deploy/migrations/000_create_databases.sql`). این تغییر فقط
`POSTGRES_DSN` است — چون قانون «بدون کوئری متقاطع» از قبل هم رعایت می‌شد، هیچ کوئری‌ای در کد botpay
نیازی به تغییر نداشت.
