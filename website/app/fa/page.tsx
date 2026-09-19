import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { ArrowLeft, Download, FolderCode, LockKeyhole, ServerOff, SquareTerminal } from 'lucide-react';
import { baseOptions } from '@/lib/layout.shared';
import { site } from '@/lib/site';

const features = [
  {
    title: 'کاملاً Local',
    description: 'TL Studio روی کامپیوتر خودتان اجرا می‌شود و مستقیم با Project Local کار می‌کند؛ بدون Cloud Backend یا Database متعلق به TL Studio.',
    icon: ServerOff,
  },
  {
    title: 'Browser IDE مستقل',
    description: 'Fileها را Edit کنید، داخل Project جست‌وجو کنید، Command اجرا کنید، Changes را ببینید و Live Preview بگیرید؛ بدون وابستگی به VS Code، JetBrains یا Cursor.',
    icon: SquareTerminal,
  },
  {
    title: 'Project-aware Agent Workflow',
    description: 'Session، File، Attachment، Changes، Permission و Context مربوط به Agent در Scope همان Project بازشده باقی می‌ماند.',
    icon: FolderCode,
  },
  {
    title: 'Provider و Model Control',
    description: 'تعریف Custom Provider و Model داخل TL Studio مدیریت می‌شود و Credentialها داخل Browser Storage ذخیره نمی‌شوند.',
    icon: LockKeyhole,
  },
];

const capabilities = [
  'Multi-tab Editor',
  'Project Search',
  'Terminal',
  'Live Preview',
  'File Attachments',
  'Custom Providers',
  'Local Security',
];

export default function PersianHomePage() {
  return (
    <HomeLayout {...baseOptions('fa')}>
      <main dir="rtl" lang="fa" className="relative overflow-hidden text-right">
        <div className="hero-grid pointer-events-none absolute inset-0 -z-10 opacity-60" />
        <section className="mx-auto flex max-w-6xl flex-col items-center px-6 pb-20 pt-20 text-center md:pt-28">
          <div className="tl-brand-lockup mb-7" dir="ltr">
            <img src={site.logoUrl} alt="TL Studio — Code. Reason. Act." />
          </div>
          <div className="mb-6 rounded-full border bg-fd-card/70 px-4 py-1.5 text-sm text-fd-muted-foreground">
            Stable v0.2.1 · Local-first · Open Source
          </div>
          <h1 className="max-w-4xl text-balance text-5xl font-bold tracking-tight md:text-7xl">
            محیط توسعه‌ی سریع و Local با AI داخلی.
          </h1>
          <p className="mt-7 max-w-2xl text-balance text-lg leading-8 text-fd-muted-foreground md:text-xl">
            TL Studio یک Browser IDE مستقل و Local است که Editor، Search، Terminal، Live Preview، Providerها و Agent را داخل یک محیط سریع کنار هم می‌آورد.
          </p>
          <div className="mt-9 flex flex-wrap items-center justify-center gap-3">
            <Link href="/fa/docs" className="inline-flex items-center gap-2 rounded-lg bg-fd-primary px-5 py-3 font-medium text-fd-primary-foreground">
              مستندات <ArrowLeft className="size-4" />
            </Link>
            <a href={site.releasesUrl} className="inline-flex items-center gap-2 rounded-lg border bg-fd-card px-5 py-3 font-medium" target="_blank" rel="noreferrer">
              <Download className="size-4" /> Releases
            </a>
          </div>
          <div className="mt-6 flex max-w-4xl flex-wrap justify-center gap-2" dir="ltr">
            {capabilities.map((item) => (
              <span key={item} className="tl-capability-chip rounded-full px-3 py-1 text-sm text-fd-muted-foreground">
                {item}
              </span>
            ))}
          </div>

          <div className="mt-16 w-full max-w-4xl rounded-2xl border bg-fd-card/80 p-5 shadow-sm md:p-8">
            <div className="mb-5 text-sm font-medium text-fd-muted-foreground">TL Studio چطور کار می‌کند؟</div>
            <div className="grid gap-3 md:grid-cols-4" dir="ltr">
              {['Browser IDE', 'TL Studio Local Core', 'Agent Engine Adapter', 'Models + Tools'].map((label, index) => (
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

        <section className="border-t bg-fd-card/35">
          <div className="mx-auto max-w-6xl px-6 py-16">
            <p className="text-sm font-medium text-fd-muted-foreground">Independent Product · Replaceable Engine</p>
            <h2 className="mt-2 text-3xl font-semibold">TL Studio همون محصولیه که کاربر می‌بینه و استفاده می‌کنه.</h2>
            <p className="mt-4 max-w-3xl leading-8 text-fd-muted-foreground">
              Editor، Project Files، Search، Terminal، Live Preview، Provider/Model Registry، Recovery، Local Security، Packaging و Releaseها متعلق به TL Studio هستند. Agent Execution پشت یک Runtime Contract عمومی و Local قرار دارد.
            </p>
          </div>
        </section>
      </main>
    </HomeLayout>
  );
}
