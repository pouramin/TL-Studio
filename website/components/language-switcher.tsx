'use client';

import Link from 'next/link';
import { Languages } from 'lucide-react';
import { usePathname } from 'next/navigation';

export function LanguageSwitcher() {
  const pathname = usePathname() || '/';
  const basePath = process.env.NEXT_PUBLIC_BASE_PATH || '';
  const logical = basePath && pathname.startsWith(basePath) ? pathname.slice(basePath.length) || '/' : pathname;
  const isFa = logical === '/fa' || logical.startsWith('/fa/');
  const englishPath = isFa ? logical.slice(3) || '/' : logical;
  const persianPath = isFa ? logical : logical === '/' ? '/fa' : `/fa${logical}`;

  return (
    <details className="group relative [&>summary::-webkit-details-marker]:hidden">
      <summary
        aria-label="Change language"
        title="Change language"
        className="flex cursor-pointer list-none items-center rounded-lg p-1.5 text-fd-muted-foreground transition-colors hover:bg-fd-accent hover:text-fd-accent-foreground group-open:bg-fd-accent"
      >
        <Languages className="size-5" />
      </summary>
      <div className="absolute end-0 z-50 mt-1.5 flex min-w-36 flex-col gap-0.5 rounded-lg border bg-fd-popover p-1 text-fd-popover-foreground shadow-lg">
        <Link
          href={englishPath}
          className={`rounded-md px-3 py-2 text-sm ${!isFa ? 'bg-fd-primary/10 text-fd-primary' : 'hover:bg-fd-accent'}`}
        >
          English
        </Link>
        <Link
          href={persianPath}
          className={`rounded-md px-3 py-2 text-sm ${isFa ? 'bg-fd-primary/10 text-fd-primary' : 'hover:bg-fd-accent'}`}
        >
          فارسی
        </Link>
      </div>
    </details>
  );
}
