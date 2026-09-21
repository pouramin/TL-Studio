function TelegramIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="size-5 fill-current">
      <path d="M21.8 4.2a1.6 1.6 0 0 0-1.7-.2L3.3 10.5c-1.1.4-1.1 1.1-.2 1.4l4.3 1.4 1.7 5.1c.2.7.1 1 .8 1 .5 0 .8-.2 1-.4l2.1-2 4.4 3.2c.8.5 1.4.2 1.6-.8l2.8-13.4c.2-.8.1-1.4 0-1.8ZM9 13l8.4-5.3c.4-.2.8-.1.5.2l-6.9 6.2-.3 3.4L9 13Z" />
    </svg>
  );
}

function YouTubeIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className="size-5 fill-current">
      <path d="M23.5 6.2a3 3 0 0 0-2.1-2.1C19.5 3.6 12 3.6 12 3.6s-7.5 0-9.4.5A3 3 0 0 0 .5 6.2 31.4 31.4 0 0 0 0 12a31.4 31.4 0 0 0 .5 5.8 3 3 0 0 0 2.1 2.1c1.9.5 9.4.5 9.4.5s7.5 0 9.4-.5a3 3 0 0 0 2.1-2.1A31.4 31.4 0 0 0 24 12a31.4 31.4 0 0 0-.5-5.8ZM9.6 15.6V8.4L15.8 12l-6.2 3.6Z" />
    </svg>
  );
}

const socialLinkClass =
  'flex items-center rounded-lg p-1.5 text-fd-muted-foreground transition-colors hover:bg-fd-accent hover:text-fd-accent-foreground';

export function SocialLinks() {
  return (
    <div className="flex items-center gap-0.5">
      <a
        href="https://t.me/TunneLab"
        target="_blank"
        rel="noreferrer"
        aria-label="TunnelLab on Telegram"
        title="TunnelLab on Telegram"
        className={socialLinkClass}
      >
        <TelegramIcon />
      </a>
      <a
        href="https://www.youtube.com/@TunnelLab"
        target="_blank"
        rel="noreferrer"
        aria-label="TunnelLab on YouTube"
        title="TunnelLab on YouTube"
        className={socialLinkClass}
      >
        <YouTubeIcon />
      </a>
    </div>
  );
}
