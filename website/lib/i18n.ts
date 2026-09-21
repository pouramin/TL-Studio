import { defineI18n } from 'fumadocs-core/i18n';

export const i18n = defineI18n({
  defaultLanguage: 'en',
  languages: ['en', 'fa'],
  parser: 'dir',
  hideLocale: 'default-locale',
});

export type Locale = (typeof i18n.languages)[number];

export function localeDirection(locale: string): 'rtl' | 'ltr' {
  return locale === 'fa' ? 'rtl' : 'ltr';
}
