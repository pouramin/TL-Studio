TL Studio Claude Web Bridge

Purpose
-------
This Chrome extension is an inference-only transport for the Claude Web provider.
It uses the Claude session already signed in to the normal Chrome profile.

It does NOT own or execute the TL Studio Agent loop, Tools, Permissions, project
filesystem access, Terminal, Sessions, persistence, or model-to-tool orchestration.

Security boundary
-----------------
- No Claude cookie, sessionKey, or browser credential is read, decrypted, exported,
  serialized, or stored by TL Studio.
- Claude Web requests run in the main-world page context of claude.ai with
  credentials:"include", so Chrome keeps the session credential browser-owned.
- The extension accepts only two transport commands: probe and complete.
- Pairing is limited to localhost/127.0.0.1 and a per-pair random token.
- The extension has no TL Studio filesystem, Terminal, Permission, Session, or Tool
  endpoint access.

Review-build installation
-------------------------
This browser-session transport deliberately has a new extension identity so Chrome
cannot reuse the service worker from the older cookie-based review bridge.
If an older "TL Studio Claude Web Bridge" is still installed, disable or remove
it in chrome://extensions to avoid confusing the two entries.

Until the extension is published through the Chrome Web Store, a review build can
be tested manually:

1. Open chrome://extensions in the SAME normal Chrome profile where Claude is
   already signed in.
2. Enable Developer mode.
3. Choose Load unpacked.
4. Select this integrations/claude-web-extension folder.
5. Keep using TL Studio normally and choose Claude -> Web (Free/Pro).

The fixed review extension ID is:
hklkkfhbcohbfpojbcanhgmfanjhnfna

The Chrome Web Store production extension ID is:
cpellhbmfdhcgkblnmnppndmeiigmjcg

TL Studio probes the Store ID first and keeps the fixed review ID as a fallback.

Production distribution
-----------------------
Normal consumer Windows Chrome installs cannot silently install a self-hosted
extension. The production path is Chrome Web Store distribution (public or
unlisted), after which TL Studio can point users to the store install once and
reuse the normal Chrome session without a separate Claude login.

The extension executes authenticated Claude Web requests in the main-world context
of claude.ai so the server sees the normal Claude origin. It first reuses any Claude
tab already open in the user's normal Chrome profile. If none exists, it creates one
inactive pinned transport tab and reuses that same tab for all later model turns.
It does not create a fresh tab per prompt. If TL Studio created the transport tab,
Sign out closes it; user-owned Claude tabs are never closed by TL Studio.
