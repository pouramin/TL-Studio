import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { LanguageSwitcher } from '@/components/language-switcher';
import { SocialLinks } from '@/components/social-links';
import { site } from './site';
import { getSiteMessages } from './site-i18n';

export function baseOptions(lang: 'en' | 'fa' = 'en'): BaseLayoutProps {
  const prefix = lang === 'fa' ? '/fa' : '';
  const m = getSiteMessages(lang);

  return {
    nav: {
      title: (
        <span className="tl-docs-brand">
          <img src={site.markUrl} alt="" aria-hidden="true" />
          <span>TL Studio</span>
        </span>
      ),
      url: `${prefix}/`,
    },
    githubUrl: site.repoUrl,
    links: [
      {
        text: m.documentation,
        url: `${prefix}/docs`,
        active: 'nested-url',
      },
      {
        text: m.releases,
        url: site.releasesUrl,
        external: true,
      },
      {
        type: 'custom',
        secondary: true,
        children: <SocialLinks />,
      },
      {
        type: 'custom',
        secondary: true,
        children: <LanguageSwitcher />,
      },
    ],
  };
}
