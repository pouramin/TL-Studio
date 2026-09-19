import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import {
  DocsBody,
  DocsDescription,
  DocsPage,
  DocsTitle,
  MarkdownCopyButton,
  ViewOptionsPopover,
} from 'fumadocs-ui/layouts/docs/page';
import { getMDXComponents } from '@/components/mdx';
import { getPageMarkdownUrl, source } from '@/lib/source';

export default async function Page(props: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await props.params;
  const page = source.getPage(slug, 'en');
  if (!page) notFound();
  const MDX = page.data.body;
  const markdownUrl = getPageMarkdownUrl(page).url;
  const editUrl = `https://github.com/pouramin/TL-Studio/blob/docs-site/website/content/locales/${page.path}`;

  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription className="mb-0">{page.data.description}</DocsDescription>
      <div className="flex flex-row items-center gap-2 border-b pb-6">
        <MarkdownCopyButton markdownUrl={markdownUrl} />
        <ViewOptionsPopover markdownUrl={markdownUrl} githubUrl={editUrl} />
      </div>
      <DocsBody><MDX components={getMDXComponents()} /></DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.getPages('en').map((page) => ({ slug: page.slugs }));
}

export async function generateMetadata(props: { params: Promise<{ slug?: string[] }> }): Promise<Metadata> {
  const { slug } = await props.params;
  const page = source.getPage(slug, 'en');
  if (!page) notFound();
  return { title: page.data.title, description: page.data.description };
}
