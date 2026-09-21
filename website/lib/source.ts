import { docs } from 'collections/server';
import { loader } from 'fumadocs-core/source';
import { lucideIconsPlugin } from 'fumadocs-core/source/lucide-icons';
import { i18n } from './i18n';

export const source = loader({
  i18n,
  baseUrl: '/docs',
  source: docs.toFumadocsSource(),
  plugins: [lucideIconsPlugin()],
});

export function getPageMarkdownUrl(page: (typeof source)['$inferPage']) {
  const basePath = process.env.NEXT_PUBLIC_BASE_PATH || '';
  const segments = [...page.slugs, 'content.md'];

  return {
    segments,
    url: `${basePath}/llms.mdx/docs/${segments.join('/')}`,
  };
}

export async function getLLMText(page: (typeof source)['$inferPage']) {
  const processed = await page.data.getText('processed');
  return `# ${page.data.title} (${page.url})\n\n${processed}`;
}
