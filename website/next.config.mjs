import { createMDX } from 'fumadocs-mdx/next';

const withMDX = createMDX();
const isStaticExport = process.env.DEPLOY_TARGET === 'static';
const basePath = process.env.NEXT_PUBLIC_BASE_PATH || '';

/** @type {import('next').NextConfig} */
const config = {
  reactStrictMode: true,
  basePath,
  ...(isStaticExport
    ? {
        output: 'export',
        trailingSlash: true,
        images: { unoptimized: true },
      }
    : {}),
};

export default withMDX(config);
