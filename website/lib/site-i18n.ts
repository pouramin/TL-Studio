export const messages = {
  en: {
    documentation: 'Documentation',
    releases: 'Releases',
    readDocs: 'Read the docs',
    download: 'Download releases',
    viewSource: 'View source on GitHub',
  },
  fa: {
    documentation: 'مستندات',
    releases: 'نسخه‌ها',
    readDocs: 'مطالعه مستندات',
    download: 'دریافت نسخه‌ها',
    viewSource: 'مشاهده سورس در GitHub',
  },
} as const;

export function getSiteMessages(lang: string) {
  return lang === 'fa' ? messages.fa : messages.en;
}
