[English](./README.md) | [فارسی](./README.fa_IR.md)

<p align="center">
  <img src="./media/tl-studio-logo.svg" width="360" alt="TL Studio">
</p>

<p align="center">
  یک محیط توسعه‌ی سریع و لوکال با AI داخلی.
</p>

<p align="center"><strong>Development branch: 0.3.0-alpha.23</strong> · نسخه Stable همچنان v0.2.1 است.</p>

<p align="center">
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/v/release/pouramin/TL-Studio?sort=semver" alt="Release"></a>
  <a href="https://github.com/pouramin/TL-Studio/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/pouramin/TL-Studio/ci.yml?branch=main&label=CI" alt="CI"></a>
  <a href="https://github.com/pouramin/TL-Studio/releases"><img src="https://img.shields.io/github/downloads/pouramin/TL-Studio/total" alt="Downloads"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

**TL Studio** یک محیط توسعه‌ی لوکال در مرورگر است که هم خودتان می‌توانید داخلش کد را بخوانید و ویرایش کنید و هم در کنار آن از Agent کمک بگیرید. Project را باز کنید، فایل‌ها را مدیریت و ویرایش کنید، در کل کد جست‌وجو کنید، Command اجرا کنید، Preview بگیرید، Model و Provider انتخاب کنید و هرجا خواستید کار را به Agent بسپارید.

برای استفاده‌ی معمول نیازی به VS Code، JetBrains، Cursor، Docker، Backend ابری TL Studio یا Database جداگانه نیست.

## شروع سریع

### نسخه‌ی Portable

فایل مناسب سیستم خود را از **[GitHub Releases](https://github.com/pouramin/TL-Studio/releases)** دانلود و Extract کنید، سپس اجرا کنید:

```text
Windows:  tl-studio.exe
Linux:    ./tl-studio
macOS:    ./tl-studio
```

Runtime لوکال سازگار از قبل داخل Release قرار دارد.

## قابلیت‌ها

- **محیط توسعه‌ی مستقل و لوکال** — Editor، File Explorer، Search، Terminal، Preview و Agent در یک Workspace مرورگری.
- **انتخاب مستقیم Project** — بازکردن فولدر با Folder Picker خود سیستم‌عامل.
- **انتخاب Agent و Model** — تغییر Agent و مدل‌های Providerها از داخل Composer.
- **Custom Provider** — اتصال Endpointهای سازگار با OpenAI، OpenAI Responses و Anthropic با Credential خود کاربر.
- **File attachment** — ارسال تصویر، PDF و فایل‌های متنی/کد؛ همراه با Multi-select، Drag & Drop و Paste از Clipboard.
- **اجرای Native Agent متعلق به TL Studio** — برای Custom Providerهای پشتیبانی‌شده، حلقه‌ی Model/Tool/Model، توقف، Loop guard، Session persistence و Live Event مستقیماً توسط TL Studio اجرا می‌شود؛ مسیر Hosted Kilo همچنان از Adapter سازگاری استفاده می‌کند.
- **Tool Executor خود TL Studio** — Toolهای اصلی کدنویسی شامل Read/List/Write/Edit فایل، Project Search و Terminal Command با Handlerهای خود TL Studio، محدودیت Project، Validation، Cancellation و Permission اجرا می‌شوند.
- **Tool Registry خود TL Studio** — Toolها نام، Category، Capability، Permission class، Schema و Presentation metadata متعلق به TL Studio دارند.
- **نمایش زنده‌ی فعالیت Agent** — نمایش Reasoning و Toolها همراه با وضعیت Run و semantic metadata خود TL Studio.
- **Permission Policy و Question متعلق به TL Studio** — Allow یک‌باره، ذخیره‌ی Ruleهای غیرحساس به‌صورت Project-scoped، Reject و پاسخ به سؤال‌های تعاملی؛ Questionها از قرارداد `/local/questions*` خود TL Studio عبور می‌کنند و UI به Route خام Runtime وابسته نیست.
- **Stop و Recovery** — توقف Run فعال و بازیابی Sessionهای گیرکرده یا خطاهای Retryable.
- **Session Read Model خود TL Studio** — Session، Message، Activity، Status، Usage metadata و Changes از Routeهای semantic متعلق به Launcher در `/local/sessions*` خوانده می‌شوند و UI دیگر برای Sessionهای فعلی به envelope خام Runtime وابسته نیست.
- **Session Command Contract خود TL Studio** — ساخت، تغییر نام، حذف، اجرای Prompt/Run و توقف Session از Routeهای semantic متعلق به Launcher در `/local/sessions*` انجام می‌شود و Adapter موتور فعال آن‌ها را به API خصوصی همان موتور ترجمه می‌کند.
- **Session Persistence متعلق به TL Studio** — metadata، transcript، activity/usage و changes نشست‌ها در State محلی TL Studio mirror می‌شوند؛ اگر history موتور از بین برود، تاریخچه همچنان قابل خواندن است و Session ذخیره‌شده را می‌توان محلی Rename یا Delete کرد.
- **Credential Vault متعلق به TL Studio** — API Key مربوط به Custom Provider داخل `providers.json` یا Browser storage ذخیره نمی‌شود. روی Windows از DPAPI، روی macOS از Keychain و روی Linux از Secret Service در صورت وجود استفاده می‌شود؛ fallback محلی نیز به‌صورت رمز‌شده نگه‌داری می‌شود.
- **مدیریت Session** — ساخت، ادامه، تغییر نام، حذف و جابه‌جایی Sessionها بین Projectهای اخیر.
- **Project Usage** — نمایش مصرف هر Turn و مجموع Project شامل Token، Request، Time، Reasoning و Cache.
- **Changes panel** — مشاهده‌ی فایل‌های تغییرکرده، تعداد خطوط اضافه/حذف‌شده و Patch.
- **Project Workspace داخلی** — File Explorer قابل‌نوشتن و Monaco Editor لوکال و Lazy-loaded با ویرایش چندتب، Find/Replace، Multi-cursor، Save/Create/Rename/Delete، هماهنگی با تغییرات خارجی فایل و دکمه‌ی **Show in Folder** برای نمایش فایل فعال در File Manager خود سیستم.
- **Project Search** — جست‌وجوی سریع متن در کل Project با Include/Exclude و بازکردن مستقیم نتیجه در Editor.
- **Terminal داخلی** — اجرای Command در Scope پروژه، تاریخچه‌ی خروجی، Stop و پایان Process tree.
- **Live Preview** — Preview لوکال مبتنی بر Capability در پنجره‌ی قابل‌جابجایی و تغییر اندازه؛ TL Studio فایل Previewable فعال را بین HTML، SVG و Image، PDF، Video، Audio، Markdown رندرشده و Plain Text رندرشده دنبال می‌کند. PDF مستقیماً با MIME و `Content-Disposition: inline` و Range support برای PDF Viewer خود مرورگر سرو می‌شود. پنجره‌ی Preview از هر 4 لبه و هر 4 گوشه قابل Resize است.
- **تنظیمات ظاهر و Editor** — حالت System، Dark و Light به‌همراه Editor theme و Font جداگانه برای UI، Code و Terminal.
- **Browser Source با Strict TypeScript و Module واقعی** — تمام Sourceهای Browser در `cmd/launcher/ui` با `strict: true` Type-check می‌شوند؛ `kernel.ts` هسته‌ی تایپ‌شده‌ی مشترک را Export می‌کند و ماژول‌ها آن را مستقیم Import می‌کنند. `browser.ts` گراف ES Module را مشخص می‌کند و esbuild یک Bundle اصلی به نام `browser.js` می‌سازد؛ دیگر وابستگی به `window.KLU` یا Build قدیمی JavaScriptهای جداگانه وجود ندارد.
- **معماری Local-first** — اجرای Loopback-only، رمز تصادفی Backend در هر اجرا، کنترل Origin و CSP محدودکننده.
- **بدون Cloud یا Telemetry اختصاصی TL Studio** — ترافیک Model براساس Provider و Runtime انتخاب‌شده‌ی کاربر انجام می‌شود و از زیرساخت TL Studio عبور نمی‌کند.

## معماری

```text
Browser workspace
    │ فقط localhost
    ▼
TL Studio launcher (Go)
    │
    ├─ TL Studio provider/model registry
    ├─ TL Studio tool registry
    ├─ TL Studio semantic session read model
    ├─ TL Studio semantic live event projection
    ├─ TL Studio permission policy engine
    ├─ project files / search / terminal / preview
    │
    └─ runtime adapter محلی و احراز‌شده
            ▼
        Local agent runtime
            ├─ agents / sessions / tool execution
            ├─ permission enforcement / questions / live events
            └─ provider execution / model inference
```

TL Studio مالک لایه‌ی محصول است: Workspace، رابط کاربری، Launcher محلی، تعریف Provider و Model، semantic metadata مربوط به Toolها، semantic read model مربوط به Session، semantic projection مربوط به Live Eventها، Permission Policy در Scope هر Project، تجربه‌ی Project و Session، Recovery و Release packaging.

تعریف Custom Providerها و API Keyهای آن‌ها اکنون تحت مالکیت TL Studio هستند. تعریف Provider داخل Registry خود TL Studio می‌ماند و Credential در Vault جداگانه نگه‌داری می‌شود؛ Launcher در زمان لازم آن را برای اجرای مدل به Runtime فعال sync می‌کند.

Runtime به‌عنوان یک لایه‌ی زیرساختی جدا پشت این مرز قرار می‌گیرد.

Project انتخاب‌شده روی سیستم کاربر باقی می‌ماند و TL Studio ترافیک Model را از زیرساخت خودش عبور نمی‌دهد.

## مرز Runtime

مرورگر و رابط محصول TL Studio به قراردادهای خود TL Studio وابسته‌اند، نه به API اختصاصی یک Engine. عملیات اجرایی Agent همچنان پشت `/runtime/*` قرار دارد، اما readهای Session فعلی از Routeهای semantic متعلق به Launcher در `/local/sessions*` و Live Eventهای Browser از SSE معنایی `/local/events` عبور می‌کنند. تعریف Provider/Model، semantic metadata مربوط به Toolها، Session read model، Permission Policy، فایل‌های Project، Search، Terminal، Preview و بخش‌های اصلی Workspace در مالکیت TL Studio هستند. Permission promptها از Routeهای `/local/permissions*` خود Launcher عبور می‌کنند تا Ruleهای Remembered در اختیار TL Studio باشند.

نسخه‌ی Stable فعلی، **Kilo Code 7.6.2** را به‌عنوان Agent Engine شخص ثالث و تست‌شده Bundle می‌کند. این Engine یک جزئیات پیاده‌سازی پشت Runtime Adapter است و هویت عمومی محصول به آن وابسته نیست. جزئیات سازگاری Engine در [`docs/KILO_API_CONTRACT.md`](./docs/KILO_API_CONTRACT.md) و Attribution لازم در [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md) نگهداری می‌شود.

CI همین Engine پین‌شده را از طریق مرز عمومی Runtime خود TL Studio برای Project routing، APIهای Agent/Provider/Session، Async Prompt، Live events، Permission، Provider configuration، اجرای Tool و Write واقعی روی فایل تست می‌کند.

## نسخه‌های قابل دانلود

| سیستم‌عامل | معماری |
| --- | --- |
| Windows | x64 |
| Linux | x64، ARM64 |
| macOS | Intel x64، Apple Silicon ARM64 |

## ساختار فایل Release

```text
tl-studio/
├─ tl-studio[.exe]
├─ bin/
│  └─ kilo[.exe]
├─ LICENSE
├─ THIRD_PARTY_NOTICES.md
└─ third_party/
   ├─ KILO_LICENSE.txt
   ├─ MONACO_LICENSE.txt
   └─ MONACO_THIRD_PARTY_NOTICES.txt
```

## اجرا از سورس

نیازمندی‌های Development:

- Go 1.23 یا جدیدتر
- Runtime binary سازگار در `PATH`، کنار Launcher یا مشخص‌شده به‌صورت دستی

```bash
go run ./cmd/launcher
```

بازکردن یک Project مشخص:

```bash
go run ./cmd/launcher --project /path/to/project
```

استفاده از Runtime binary مشخص:

```bash
go run ./cmd/launcher --runtime-bin /path/to/runtime
```

برای جلوگیری از بازشدن خودکار مرورگر از `--no-browser` استفاده کنید.

## قانون زیرساخت صفر

TL Studio طوری طراحی شده که نگهدارنده برای اجرای پروژه نیازی به پرداخت هزینه‌ی VPS، Hosting، Database، API Gateway، Model inference یا Telemetry backend نداشته باشد. سورس، Issueها، CI، Releaseها، فایل‌های دانلودی و Launcher سبک npm از زیرساخت GitHub/npm توزیع می‌شوند.

هزینه‌ی احتمالی استفاده از Model مستقیماً بین کاربر و Provider انتخاب‌شده‌ی اوست.

## مدل امنیتی

Launcher:

1. رابط را فقط روی Loopback اجرا می‌کند؛
2. Runtime لوکال را با رمز تصادفی در هر اجرا بالا می‌آورد؛
3. رمز Backend را سمت Server نگه می‌دارد؛
4. Project انتخاب‌شده را فقط به‌صورت محلی Route می‌کند؛
5. درخواست‌های Cross-origin را رد می‌کند؛
6. و رابط را با Content Security Policy محدودکننده سرو می‌کند.

Runtime در صورت داشتن Permission می‌تواند فایل‌ها را بخواند، بنویسد و Command اجرا کند. TL Studio را فقط روی سیستم و Projectهایی اجرا کنید که به آن‌ها اعتماد دارید.

## وضعیت پروژه

TL Studio خط پایدار Production را روی `main` و توسعه‌ی آزمایشی را روی `dev` نگه می‌دارد. نسخه‌ی Stable فقط بعد از عبور از CI خودکار و تست دستی روی یک سیستم واقعی Windows ارتقا داده می‌شود. مسیر اصلی که پیش از Promotion بررسی می‌شود شامل این زنجیره است:

```text
TL Studio UI
→ local agent runtime
→ selected model
→ tool call
→ permission
→ local file write
→ final assistant response
```

Alphaها فقط به‌صورت Preview Build خصوصی روی `dev` می‌مانند و روی npm یا GitHub Releases منتشر نمی‌شوند.

## لایسنس و Attribution

کد Launcher و UI پروژه‌ی TL Studio تحت لایسنس MIT منتشر شده است. Runtime فعلی Kilo Code نیز MIT است و به‌عنوان یک پروژه‌ی مستقل Upstream باقی می‌ماند. Releaseهایی که آن را Bundle می‌کنند، License Notice مربوط به آن را نیز همراه خود دارند؛ برای جزئیات [`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md) را ببینید.

TL Studio یک پروژه‌ی مستقل است و محصول رسمی Runtime Upstream خود نیست.

این پروژه با هویت **TunnelLab** توسعه داده می‌شود.
