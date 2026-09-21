'use client';

import { usePathname } from 'next/navigation';
import { useEffect } from 'react';

export function LocaleHtmlSync() {
  const pathname = usePathname() || '/';

  useEffect(() => {
    const basePath = process.env.NEXT_PUBLIC_BASE_PATH || '';
    const logical = basePath && pathname.startsWith(basePath) ? pathname.slice(basePath.length) || '/' : pathname;
    const isFa = logical === '/fa' || logical.startsWith('/fa/');
    document.documentElement.lang = isFa ? 'fa' : 'en';
    document.documentElement.dir = isFa ? 'rtl' : 'ltr';
  }, [pathname]);

  return null;
}
