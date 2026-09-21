import { source } from '@/lib/source';
import { llms } from 'fumadocs-core/source';

export const revalidate = false;

export async function GET() {
  return new Response(await llms(source).index(), {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
}
