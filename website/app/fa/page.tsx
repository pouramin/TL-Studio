import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { ArrowLeft, Download, FolderCode, LockKeyhole, ServerOff, SquareTerminal } from 'lucide-react';
import { baseOptions } from '@/lib/layout.shared';
import { site } from '@/lib/site';

const features = [
  {
    title: 'توسعه کاملاً Local',
    description: 'TL Studio روی سیستم خودتان اجرا می‌شود و مستقیم با Project Local کار می‌کند؛ بدون Cloud Backend یا Database متعلق به TL Studio.',
    icon: ServerOff,
  },
  {
    title: 'Browser IDE مستقل',
    description: 'از Monaco، Project Search، Terminal، Changes و Preview استفاده کنید؛ بدون اینکه به VS Code، JetBrains یا Cursor وابسته باشید.',
    icon: SquareTerminal,
  },
  {
    title: 'اجرای Native Agent',
    description: 'Custom Providerهای پشتیبانی‌شده می‌توانند از Agent Loop و Core Tool Executor متعلق به خود TL Studio استفاده کنند.',
    icon: FolderCode,
  },
  {
    title: 'مالکیت Local روی Provider',
    description: 'تعریف Provider/Model، Credential، Permission Policy، Session و Tool Semantics در لایه محصول خود TL Studio مدیریت می‌شوند.',
    icon: LockKeyhole,
  },
];

const capabilities = [
  'Monaco Editor',
  'Native Agent',
  'Tool Executor',
  'Project Search',
  'Terminal',
  'Live Preview',
  'Session Persistence',
  'Credential Vault',
];

export default function PersianHomePage() {
  return (
    <HomeLayout {...baseOptions('fa')}>
      <main dir="rtl" lang="fa" className="relative overflow-hidden text-right">
        <div className="hero-grid pointer-events-none absolute inset-0 -z-10 opacity-60" />
        <section className="mx-auto flex max-w-6xl flex-col items-center px-6 pb-20 pt-20 text-center md:pt-28">
          <div className="tl-brand-lockup mb-7" dir="ltr">
            <img src={site.logoUrl} alt="TL Studio" />
          </div>
          <div className="mb-6 rounded-full border bg-fd-card/70 px-4 py-1.5 text-sm text-fd-muted-foreground">
            Stable v0.3.0 · Local-first · Open Source
          </div>
          <h1 className="max-w-4xl text-balance text-5xl font-bold tracking-tight md:text-7xl">
            محیط توسعه‌ی سریع و Local با AI داخلی.
          </h1>
          <p className="mt-7 max-w-2xl text-balance text-lg leading-8 text-fd-muted-foreground md:text-xl">
            TL Studio یک Browser IDE مستقل است که Editor، Search، Terminal، Preview، Providerها و Agent را داخل یک Workspace Local کنار هم می‌آورد.
          </p>
          <div className="mt-9 flex flex-wrap items-center justify-center gap-3">
            <Link href="/fa/docs" className="inline-flex items-center gap-2 rounded-lg bg-fd-primary px-5 py-3 font-medium text-fd-primary-foreground">
              مستندات <ArrowLeft className="size-4" />
            </Link>
            <a href={site.releasesUrl} className="inline-flex items-center gap-2 rounded-lg border bg-fd-card px-5 py-3 font-medium" target="_blank" rel="noreferrer">
              <Download className="size-4" /> Releases
            </a>
          </div>
          <div className="mt-6 flex max-w-5xl flex-wrap justify-center gap-2" dir="ltr">
            {capabilities.map((item) => (
              <span key={item} className="tl-capability-chip rounded-full px-3 py-1 text-sm text-fd-muted-foreground">
                {item}
              </span>
            ))}
          </div>

          <div className="mt-16 w-full max-w-5xl rounded-2xl border bg-fd-card/80 p-5 shadow-sm md:p-8">
            <div className="mb-5 text-sm font-medium text-fd-muted-foreground">معماری Stable v0.3</div>
            <div className="grid gap-3 md:grid-cols-4" dir="ltr">
              {['Browser Workspace', 'TL Studio Local Core', 'Native + Compatibility Execution', 'Models + Tools'].map((label, index) => (
                <div key={label} className="relative">
                  <div className="arch-line rounded-xl px-4 py-5 text-center font-medium">{label}</div>
                  {index < 3 ? <div className="absolute -right-3 top-1/2 hidden -translate-y-1/2 text-fd-muted-foreground md:block">→</div> : null}
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="mx-auto grid max-w-6xl gap-4 px-6 pb-24 md:grid-cols-2">
          {features.map(({ title, description, icon: Icon }) => (
            <article key={title} className="rounded-2xl border bg-fd-card p-6">
              <Icon className="mb-4 size-6" />
              <h2 className="text-lg font-semibold">{title}</h2>
              <p className="mt-2 leading-8 text-fd-muted-foreground">{description}</p>
            </article>
          ))}
        </section>

        <section className="border-t bg-fd-card/20">
          <div className="mx-auto max-w-6xl px-6 py-14">
            <p className="text-sm font-medium text-fd-muted-foreground">خط خصوصی بعدی · v0.4.0-alpha.1</p>
            <h2 className="mt-2 text-3xl font-semibold">فاز 2 وارد Stable شده و فاز 3 هنوز شروع نشده است.</h2>
            <p className="mt-4 max-w-3xl leading-8 text-fd-muted-foreground">
              Branch توسعه از Baseline نسخه v0.3.0 دوباره باز شده است. تا وقتی قابلیت جدیدی واقعاً پیاده‌سازی و Validate نشود، چیزی از v0.4 به‌عنوان Feature موجود معرفی نمی‌شود.
            </p>
            <Link href="/fa/docs/reference/development-preview" className="mt-5 inline-flex items-center gap-2 font-medium text-fd-primary">
              وضعیت خط Development <ArrowLeft className="size-4" />
            </Link>
          </div>
        </section>

        <section className="border-t bg-fd-card/35">
          <div className="mx-auto max-w-6xl px-6 py-16">
            <p className="text-sm font-medium text-fd-muted-foreground">Independent Product · Compatibility Engine پشت Boundary</p>
            <h2 className="mt-2 text-3xl font-semibold">هسته‌ی Workflow کدنویسی برای Custom Providerهای پشتیبانی‌شده حالا متعلق به TL Studio است.</h2>
            <p className="mt-4 max-w-3xl leading-8 text-fd-muted-foreground">
              Workspace، Editor، Providerها، Credential Vault، Sessionهای Semantic، Tool Model، Permissionها، Live Eventها، Native Agent Loop، Core Tool Executor، Preview، Security Boundary، Packaging و Releaseها همگی بخشی از خود TL Studio هستند. Engine شخص ثالث فقط برای مسیرهای Hosted و Compatibility که هنوز Native نشده‌اند باقی مانده است.
            </p>
          </div>
        </section>
      </main>
    </HomeLayout>
  );
}
