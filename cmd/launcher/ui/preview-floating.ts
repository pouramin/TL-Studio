(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__previewFloatingInstalled) return;
  K.__previewFloatingInstalled = true;

  if (!document.querySelector('link[href="/preview-floating.css"]')) {
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = "/preview-floating.css";
    document.head.appendChild(css);
  }

  const panel = document.getElementById("previewPanel");
  const head = panel?.querySelector(".preview-head");
  if (!panel || !head) return;

  const KEY = "tl-studio.preview-window";
  const clamp = (v, min, max) => Math.min(Math.max(v, min), max);
  const viewport = () => ({ width: window.innerWidth, height: window.innerHeight });

  const read = () => {
    try { return JSON.parse(localStorage.getItem(KEY) || "null"); }
    catch { return null; }
  };
  const write = () => {
    try {
      const r = panel.getBoundingClientRect();
      localStorage.setItem(KEY, JSON.stringify({ left: r.left, top: r.top, width: r.width, height: r.height }));
    } catch {}
  };

  const apply = (state = read()) => {
    const vp = viewport();
    const width = clamp(Number(state?.width) || Math.min(900, vp.width * 0.58), 420, Math.max(420, vp.width - 32));
    const height = clamp(Number(state?.height) || Math.min(680, vp.height * 0.74), 300, Math.max(300, vp.height - 32));
    const left = clamp(Number(state?.left) || Math.max(16, vp.width - width - 24), 8, Math.max(8, vp.width - width - 8));
    const top = clamp(Number(state?.top) || 96, 8, Math.max(8, vp.height - height - 8));
    Object.assign(panel.style, {
      position: "fixed",
      left: `${left}px`,
      top: `${top}px`,
      right: "auto",
      bottom: "auto",
      width: `${width}px`,
      height: `${height}px`,
    });
  };

  panel.classList.add("preview-floating-window");
  head.classList.add("preview-drag-handle");
  apply();

  let drag = null;
  head.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || event.target.closest("button, a, input, select")) return;
    const r = panel.getBoundingClientRect();
    drag = { id: event.pointerId, dx: event.clientX - r.left, dy: event.clientY - r.top };
    head.setPointerCapture?.(event.pointerId);
    panel.classList.add("preview-dragging");
    event.preventDefault();
  });

  head.addEventListener("pointermove", (event) => {
    if (!drag || drag.id !== event.pointerId) return;
    const r = panel.getBoundingClientRect();
    const vp = viewport();
    const left = clamp(event.clientX - drag.dx, 8, Math.max(8, vp.width - r.width - 8));
    const top = clamp(event.clientY - drag.dy, 8, Math.max(8, vp.height - r.height - 8));
    panel.style.left = `${left}px`;
    panel.style.top = `${top}px`;
  });

  const endDrag = (event) => {
    if (!drag || drag.id !== event.pointerId) return;
    drag = null;
    panel.classList.remove("preview-dragging");
    write();
  };
  head.addEventListener("pointerup", endDrag);
  head.addEventListener("pointercancel", endDrag);

  let resizeTimer = null;
  if ("ResizeObserver" in window) {
    new ResizeObserver(() => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(write, 100);
    }).observe(panel);
  }

  window.addEventListener("resize", () => {
    const r = panel.getBoundingClientRect();
    apply({ left: r.left, top: r.top, width: r.width, height: r.height });
    write();
  });

  K.previewWindow = Object.freeze({
    reset: () => {
      try { localStorage.removeItem(KEY); } catch {}
      apply(null);
      write();
    },
  });
})();
