import type { ReactNode } from 'react';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { source } from '@/lib/source';
import { baseOptions } from '@/lib/layout.shared';

export default function Layout({ children }: { children: ReactNode }) {
  return (
    <div dir="rtl" lang="fa" className="w-full text-right">
      <DocsLayout tree={source.getPageTree('fa')} {...baseOptions('fa')}>
        {children}
      </DocsLayout>
    </div>
  );
}
