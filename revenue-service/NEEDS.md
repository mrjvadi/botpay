# revenue-service — چه چیزی نیاز داریم (بررسی ۲۰۲۶-۰۷-۰۴)

## وضعیت فعلی (خلاصه‌ی واقعی، نه قدیمی)

سرویس کوچک (۶ فایل، ۱۰۳۰ خط) با منطق تمیز: `RevenueRule` بر اساس نوع
درآمد درصد owner/platform تعیین می‌کند، `Earning` رکورد pending/processing/
done/failed دارد، idempotency روی `RefID` رعایت شده
(`internal/engine/engine.go:170-181`)، و تنها فایل تست از بین سه سرویس
بدون تست دیگر اینجا وجود دارد: `internal/engine/engine_test.go`. ورودی از
NATS (`earning.created`, با `QueueSubscribe` برای load balancing —
`internal/api/api.go:60`) و از REST (`POST /api/v1/revenue/earn`، پشت
`is_admin` middleware) هر دو به همان `CreateAndProcess` می‌رسند.

## چیزی که واقعاً کم است (با file:line برای هر ادعا)

### یافته‌ی بحرانی (تأیید‌شده، نه فرضی): مسیر پرداخت واقعی هرگز کار نمی‌کند

**ادعا:** `revenue-service` برای واریز واقعی سهم owner/platform یک HTTP
client به سمت botpay دارد که آدرس‌هایی می‌زند که در botpay اصلاً وجود
ندارند — یعنی هر earning که پردازش می‌شود در عمل به `EarningFailed` ختم
می‌شود و هیچ پولی جابه‌جا نمی‌شود.

**شواهد دقیق:**

1. `revenue-service/cmd/main.go:136-167` — `type botpayClient struct` با
   دو متد `AddCredit` (خط ۱۵۲، POST به `/api/v1/pay/credit/add`) و
   `Deduct` (خط ۱۶۰، POST به `/api/v1/pay/deduct`)، هر دو از طریق
   `b.post(...)` که یک `http.Client` معمولی است (`main.go:169-207`).
2. `.env:39` تأیید می‌کند این در دیپلوی واقعی فعال است، نه خاموش:
   `BOTPAY_URL=http://botpay:8087` — یعنی شرط `if b.baseURL == ""` در
   `main.go:170-172` (که در آن صورت بی‌صدا موفق برمی‌گرداند) اینجا صدق
   نمی‌کند؛ درخواست واقعاً ارسال می‌شود.
3. بازخوانی کامل و تازه‌ی `botpay/cmd/main.go` و کل `botpay/internal/**`
   (`grep -rn "gin\.\|net/http\|ListenAndServe" botpay --include="*.go"`)
   نشان می‌دهد **تنها** استفاده از `net/http` در کل سرویس در
   `botpay/internal/ton/watcher.go` است و آن هم یک `http.Client` خروجی
   برای گرفتن تراکنش‌های TON از یک API بیرونی است، نه یک سرور. هیچ
   `gin.Engine`، `http.Server` یا `ListenAndServe` در کل botpay وجود ندارد.
4. `botpay/cmd/main.go:127-129` این را با کامنت خودش صریحاً تأیید می‌کند:
   «همه‌ی سرویس‌ها برای موجودی/پرداخت فقط از این طریق [NATS] با botpay حرف
   می‌زنند. **REST API حذف شده** — ارتباط بین‌سرویسی کاملاً روی NATS است.»
   `botpay/internal/payresponder/responder.go:70-80` نشان می‌دهد مسیر
   واقعی معادل این دو عملیات، `pay.deduct` و `pay.credit` روی NATS
   request/reply است (`protocol.SubjPayDeduct`, `protocol.SubjPayCredit`)
   نه REST.

**نتیجه:** هر بار که `revenue-service/internal/engine/engine.go:86` یا
`engine.go:105` یا `engine.go:215`/`230` (`e.pay.AddCredit(...)`) صدا زده
شود، درخواست HTTP به `http://botpay:8087/api/v1/pay/credit/add` می‌رود —
جایی که هیچ سرور HTTP گوش نمی‌دهد، پس اتصال رد می‌شود
(connection refused). در `ProcessEarning`
(`engine.go:47-118`) این خطا در خط ۸۶-۹۰ باعث می‌شود
`e.store.MarkFailed(ctx, earning.ID, ...)` صدا زده شود
(`internal/store/store.go:115-123`)، وضعیت به `EarningFailed` تغییر
می‌کند، و چون `ListPendingEarnings` (`store.go:87-94`) فقط رکوردهای
`EarningPending` را برمی‌گرداند، این earning دیگر هرگز دوباره تلاش
نمی‌شود — **برای همیشه failed می‌ماند، بدون هیچ واریز واقعی.**

این یعنی هم مسیر ads-bot (`earning.created` برای درآمد تبلیغ کانال) و هم
مسیر community-service (سهم owner/platform گروه/کانال) که هر دو به
revenue-service متکی‌اند، در نهایت وارد یک صف "failed" دائمی می‌شوند —
صاحبان کانال/گروه در عمل هیچ‌وقت این سهم را در کیف‌پول واقعی خود
نمی‌بینند، هرچند از دید ads-bot/community-service به نظر می‌رسد «earning
ثبت و پردازش شد».

### سایر یافته‌ها

1. **راه‌حل ساده وجود دارد ولی پیاده نشده:** `revengine.PayClient`
   (`internal/engine/engine.go:14-18`) یک interface ساده با دو متد است؛
   جایگزینی پیاده‌سازی HTTP آن با یک کلاینت NATS request/reply (شبیه
   `shared/natspayclient` که ads-bot از آن استفاده می‌کند —
   `ads-bot/internal/tgbot/handler.go:12`) کاملاً امکان‌پذیر است بدون
   تغییر در `Engine`. این تنها کاری است که برای رفع این باگ لازم است.
2. اگر `BOTPAY_URL` به‌جای مقدار فعلی خالی گذاشته شود (که در محیط دیگری
   ممکن است پیش بیاید)، `main.go:170-172` بی‌صدا `""` و `nil` برمی‌گرداند
   — یعنی `ProcessEarning` فکر می‌کند پرداخت موفق بوده (چون `err == nil`)
   و earning را `EarningDone` علامت می‌زند با `owner_tx_id=""`. این حالت
   بدتر است چون حتی رکورد failed هم برای بررسی باقی نمی‌ماند — این ریسک
   configuration را هم مستند کردیم چون در `post()` (`main.go:169-172`)
   واقعاً وجود دارد.

## این‌ها را در سرویس‌های دیگر هم نوشتم (اگر مرتبط بود)

- **botpay/NEEDS.md** — همین یافته را آنجا هم از زاویه‌ی «مصرف‌کننده‌ای
  با انتظار غلط از API شما وجود دارد» ثبت کردم، چون رفع این مشکل احتمالاً
  باید در سمت revenue-service انجام شود (افزودن NATS client)، ولی botpay
  هم باید از وجود این مصرف‌کننده‌ی درحال‌شکست باخبر باشد.

## به‌روزرسانی ۲۰۲۶-۰۷-۰۶: جداسازی دیتابیس
`revenue-service` حالا دیتابیس مخصوص خودش (`revenue`) را دارد — دیگر با بقیه‌ی سرویس‌های مرکزی
مشترک نیست.
