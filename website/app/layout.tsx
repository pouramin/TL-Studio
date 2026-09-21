import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Vazirmatn } from 'next/font/google';
import { RootProvider } from 'fumadocs-ui/provider/next';
import SearchDialog from '@/components/search-dialog';
import { LocaleHtmlSync } from '@/components/locale-html-sync';
import { site } from '@/lib/site';
import './global.css';

const vazir = Vazirmatn({
  subsets: ['arabic'],
  display: 'swap',
  variable: '--font-vazir',
});

export const metadata: Metadata = {
  title: {
    default: `${site.name} — Documentation`,
    template: `%s — ${site.name}`,
  },
  description: site.description,
  applicationName: site.name,
  icons: {
    icon: site.markUrl,
  },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" dir="ltr" className={vazir.variable} suppressHydrationWarning>
      <body className="flex min-h-screen flex-col">
        <RootProvider search={{ SearchDialog }}>
          <LocaleHtmlSync />
          {children}
        </RootProvider>
      </body>
    </html>
  );
}
