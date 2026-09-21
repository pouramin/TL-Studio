import type { Metadata } from 'next';
import { notFound } from 'next/navigation';
import { DocsBody, DocsDescription, DocsPage, DocsTitle } from 'fumadocs-ui/layouts/docs/page';
import { getMDXComponents } from '@/components/mdx';
import { source } from '@/lib/source';

export default async function Page(props: { params: Promise<{ slug?: string[] }> }) {
  const { slug } = await props.params;
  const page = source.getPage(slug, 'fa');
  if (!page) notFound();
  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription>{page.data.description}</DocsDescription>
      <DocsBody><MDX components={getMDXComponents()} /></DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return source.getPages('fa').map((page) => ({ slug: page.slugs }));
}

export async function generateMetadata(props: { params: Promise<{ slug?: string[] }> }): Promise<Metadata> {
  const { slug } = await props.params;
  const page = source.getPage(slug, 'fa');
  if (!page) notFound();
  return { title: page.data.title, description: page.data.description };
}
