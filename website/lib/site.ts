export const publicBasePath = process.env.NEXT_PUBLIC_BASE_PATH || '';

export const site = {
  name: 'TL Studio',
  tagline: 'Build locally. Work with AI.',
  description:
    'A fast local development workspace with an editor, project tools, terminal, preview, providers, and an AI agent built in.',
  repoUrl: 'https://github.com/pouramin/TL-Studio',
  releasesUrl: 'https://github.com/pouramin/TL-Studio/releases',
  issuesUrl: 'https://github.com/pouramin/TL-Studio/issues',
  logoUrl: `${publicBasePath}/tl-studio-logo.svg`,
  markUrl: `${publicBasePath}/tl-studio-mark.svg`,
} as const;
