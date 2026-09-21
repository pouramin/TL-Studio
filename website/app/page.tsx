import Link from 'next/link';
import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { ArrowRight, Download, FolderCode, LockKeyhole, ServerOff, SquareTerminal } from 'lucide-react';
import { baseOptions } from '@/lib/layout.shared';
import { site } from '@/lib/site';

const features = [
  {
    title: 'Local-first development',
    description: 'TL Studio runs on your computer, works directly with your local project, and does not require a TL Studio cloud backend or database.',
    icon: ServerOff,
  },
  {
    title: 'A browser IDE of its own',
    description: 'Use Monaco, Project Search, Terminal, Changes, and capability-driven Preview without depending on VS Code, JetBrains, Cursor, or another IDE.',
    icon: SquareTerminal,
  },
  {
    title: 'Native AI execution',
    description: 'Supported custom providers can run through TL Studio’s own model/tool/model Agent loop and core coding Tool Executor.',
    icon: FolderCode,
  },
  {
    title: 'Local provider ownership',
    description: 'Provider/model definitions, credentials, permission policy, sessions, and product-facing tool semantics are owned locally by TL Studio.',
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
  'Session persistence',
  'Credential vault',
];

export default function HomePage() {
  return (
    <HomeLayout {...baseOptions('en')}>
      <main className="relative overflow-hidden">
        <div className="hero-grid pointer-events-none absolute inset-0 -z-10 opacity-60" />
        <section className="mx-auto flex max-w-6xl flex-col items-center px-6 pb-20 pt-20 text-center md:pt-28">
          <div className="tl-brand-lockup mb-7">
            <img src={site.logoUrl} alt="TL Studio" />
          </div>
          <div className="mb-6 rounded-full border bg-fd-card/70 px-4 py-1.5 text-sm text-fd-muted-foreground">
            Stable v0.3.0 · Local-first · Open source
          </div>
          <h1 className="max-w-4xl text-balance text-5xl font-bold tracking-tight md:text-7xl">
            Your fast local development workspace with AI built in.
          </h1>
          <p className="mt-7 max-w-2xl text-balance text-lg leading-8 text-fd-muted-foreground md:text-xl">
            {site.description}
          </p>
          <div className="mt-9 flex flex-wrap items-center justify-center gap-3">
            <Link href="/docs" className="inline-flex items-center gap-2 rounded-lg bg-fd-primary px-5 py-3 font-medium text-fd-primary-foreground">
              Read the docs <ArrowRight className="size-4" />
            </Link>
            <a href={site.releasesUrl} className="inline-flex items-center gap-2 rounded-lg border bg-fd-card px-5 py-3 font-medium" target="_blank" rel="noreferrer">
              <Download className="size-4" /> Download releases
            </a>
          </div>
          <div className="mt-6 flex max-w-5xl flex-wrap justify-center gap-2">
            {capabilities.map((item) => (
              <span key={item} className="tl-capability-chip rounded-full px-3 py-1 text-sm text-fd-muted-foreground">
                {item}
              </span>
            ))}
          </div>

          <div className="mt-16 w-full max-w-5xl rounded-2xl border bg-fd-card/80 p-5 text-left shadow-sm md:p-8">
            <div className="mb-5 text-sm font-medium text-fd-muted-foreground">Stable v0.3 architecture</div>
            <div className="grid gap-3 md:grid-cols-4">
              {['Browser workspace', 'TL Studio local core', 'Native + compatibility execution', 'Models + tools'].map((label, index) => (
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
              <p className="mt-2 leading-7 text-fd-muted-foreground">{description}</p>
            </article>
          ))}
        </section>

        <section className="border-t bg-fd-card/20">
          <div className="mx-auto max-w-6xl px-6 py-14">
            <p className="text-sm font-medium text-fd-muted-foreground">Next private line · v0.4.0-alpha.1</p>
            <h2 className="mt-2 text-3xl font-semibold">Phase 2 is stable. Phase 3 has not started yet.</h2>
            <p className="mt-4 max-w-3xl leading-7 text-fd-muted-foreground">
              The dev branch has reopened from the v0.3.0 baseline for the next phase. No v0.4-only product capability is documented as stable until implementation and validation actually land.
            </p>
            <Link href="/docs/reference/development-preview" className="mt-5 inline-flex items-center gap-2 font-medium text-fd-primary">
              Development line details <ArrowRight className="size-4" />
            </Link>
          </div>
        </section>

        <section className="border-t bg-fd-card/35">
          <div className="mx-auto max-w-6xl px-6 py-16">
            <p className="text-sm font-medium text-fd-muted-foreground">Independent product · Compatibility engine behind the boundary</p>
            <h2 className="mt-2 text-3xl font-semibold">TL Studio now owns the core coding workflow for supported custom providers.</h2>
            <p className="mt-4 max-w-3xl leading-7 text-fd-muted-foreground">
              The workspace, editor, providers, credential vault, semantic sessions, tool model, permissions, live events, native Agent loop, core Tool Executor, preview system, security boundary, packaging, and releases are TL Studio product capabilities. A bundled third-party engine remains only for hosted and compatibility paths that are not native yet.
            </p>
          </div>
        </section>
      </main>
    </HomeLayout>
  );
}
