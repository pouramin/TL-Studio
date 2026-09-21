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
  const head = panel?.querySelector<HTMLElement>(".preview-head");
  if (!panel || !head) return;

  const KEY = "tl-studio.preview-window";
  const MIN_WIDTH = 340;
  const MIN_HEIGHT = 300;
  const clamp = (v: any, min: any, max: any) => Math.min(Math.max(v, min), max);
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
    const width = clamp(Number(state?.width) || Math.min(900, vp.width * 0.58), MIN_WIDTH, Math.max(MIN_WIDTH, vp.width - 32));
    const height = clamp(Number(state?.height) || Math.min(680, vp.height * 0.74), MIN_HEIGHT, Math.max(MIN_HEIGHT, vp.height - 32));
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

  const resizeDirections = ["n", "s", "e", "w", "ne", "nw", "se", "sw"];
  const resizeHandles = resizeDirections.map((direction) => {
    const handle = document.createElement("div");
    handle.className = `preview-resize-handle preview-resize-${direction}`;
    handle.dataset.direction = direction;
    handle.title = `Drag to resize preview from ${direction.toUpperCase()}`;
    handle.setAttribute("aria-hidden", "true");
    panel.appendChild(handle);
    return handle;
  });

  apply();

  let edgeResize: { id: number; direction: string; startX: number; startY: number; left: number; top: number; right: number; bottom: number } | null = null;
  const startResize = (event: any) => {
    if (event.button !== 0) return;
    const handle = event.currentTarget;
    const direction = String(handle?.dataset?.direction || "");
    if (!direction) return;
    const rect = panel.getBoundingClientRect();
    edgeResize = {
      id: event.pointerId,
      direction,
      startX: event.clientX,
      startY: event.clientY,
      left: rect.left,
      top: rect.top,
      right: rect.right,
      bottom: rect.bottom,
    };
    handle.setPointerCapture?.(event.pointerId);
    panel.classList.add("preview-resizing");
    event.preventDefault();
    event.stopPropagation();
  };

  const moveResize = (event: any) => {
    if (!edgeResize || edgeResize.id !== event.pointerId) return;
    const vp = viewport();
    const dx = event.clientX - edgeResize.startX;
    const dy = event.clientY - edgeResize.startY;
    let left = edgeResize.left;
    let right = edgeResize.right;
    let top = edgeResize.top;
    let bottom = edgeResize.bottom;
    const direction = edgeResize.direction;

    if (direction.includes("w")) {
      left = clamp(edgeResize.left + dx, 8, edgeResize.right - MIN_WIDTH);
    }
    if (direction.includes("e")) {
      right = clamp(edgeResize.right + dx, edgeResize.left + MIN_WIDTH, vp.width - 8);
    }
    if (direction.includes("n")) {
      top = clamp(edgeResize.top + dy, 8, edgeResize.bottom - MIN_HEIGHT);
    }
    if (direction.includes("s")) {
      bottom = clamp(edgeResize.bottom + dy, edgeResize.top + MIN_HEIGHT, vp.height - 8);
    }

    panel.style.left = `${left}px`;
    panel.style.top = `${top}px`;
    panel.style.width = `${Math.max(MIN_WIDTH, right - left)}px`;
    panel.style.height = `${Math.max(MIN_HEIGHT, bottom - top)}px`;
  };

  const endEdgeResize = (event: any) => {
    if (!edgeResize || edgeResize.id !== event.pointerId) return;
    edgeResize = null;
    panel.classList.remove("preview-resizing");
    write();
  };

  for (const handle of resizeHandles) {
    handle.addEventListener("pointerdown", startResize);
    handle.addEventListener("pointermove", moveResize);
    handle.addEventListener("pointerup", endEdgeResize);
    handle.addEventListener("pointercancel", endEdgeResize);
  }

  let drag: { id: number; dx: number; dy: number } | null = null;
  head.addEventListener("pointerdown", (event: PointerEvent) => {
    if (event.button !== 0 || (event.target as Element | null)?.closest("button, a, input, select")) return;
    const r = panel.getBoundingClientRect();
    drag = { id: event.pointerId, dx: event.clientX - r.left, dy: event.clientY - r.top };
    head.setPointerCapture?.(event.pointerId);
    panel.classList.add("preview-dragging");
    event.preventDefault();
  });

  head.addEventListener("pointermove", (event: PointerEvent) => {
    if (!drag || drag.id !== event.pointerId) return;
    const r = panel.getBoundingClientRect();
    const vp = viewport();
    const left = clamp(event.clientX - drag.dx, 8, Math.max(8, vp.width - r.width - 8));
    const top = clamp(event.clientY - drag.dy, 8, Math.max(8, vp.height - r.height - 8));
    panel.style.left = `${left}px`;
    panel.style.top = `${top}px`;
  });

  const endDrag = (event: any) => {
    if (!drag || drag.id !== event.pointerId) return;
    drag = null;
    panel.classList.remove("preview-dragging");
    write();
  };
  head.addEventListener("pointerup", endDrag);
  head.addEventListener("pointercancel", endDrag);

  let resizeTimer: number | null = null;
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
