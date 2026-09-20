(() => {
  "use strict";
  const K = window.KLU;
  if (!K || K.__editorEnhancementsInstalled) return;
  K.__editorEnhancementsInstalled = true;

  const css = document.createElement("link");
  css.rel = "stylesheet";
  css.href = "/editor-enhancements.css";
  document.head.appendChild(css);

  const esc = (s) => String(s || "").replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");
  const lang = (path) => {
    const ext = String(path || "").toLowerCase().split(".").pop();
    if (["html","htm","xml","svg","vue","svelte"].includes(ext)) return "markup";
    if (["css","scss","sass","less"].includes(ext)) return "css";
    if (["js","jsx","mjs","cjs"].includes(ext)) return "js";
    if (["ts","tsx"].includes(ext)) return "ts";
    if (ext === "py") return "py";
    if (ext === "go") return "go";
    if (["json","jsonl"].includes(ext)) return "json";
    return "generic";
  };

  const words = {
    js: "as async await break case catch class const continue default delete do else export extends false finally for from function get if import in instanceof let new null of return set static super switch this throw true try typeof undefined var void while yield".split(" "),
    ts: "abstract any as async await bigint boolean break case catch class const constructor continue declare default delete do else enum export extends false finally for from function get if implements import in interface keyof let namespace never new null number object of private protected public readonly return set static string super switch symbol this throw true try type typeof undefined unknown var void while yield".split(" "),
    py: "and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield".split(" "),
    go: "break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var".split(" "),
    generic: "class const let var function return if else for while switch case break continue import export from new true false null public private protected static async await try catch finally throw".split(" "),
  };

  const highlight = (source, language) => {
    let text = esc(source);
    const stash = [];
    const protect = (html, cls) => {
      const id = stash.length;
      stash.push(`<span class="tok-${cls}">${html}</span>`);
      return `@@TL${id}@@`;
    };

    if (language === "markup") {
      text = text.replace(/(&lt;!--[\s\S]*?--&gt;)/g, (m) => protect(m, "comment"));
      text = text.replace(/(&lt;\/?)([A-Za-z][\w:-]*)/g, '$1<span class="tok-tag">$2</span>');
      text = text.replace(/\s([:\w-]+)(=)(\"[^\"]*\"|'[^']*')/g, ' <span class="tok-attr">$1</span>$2<span class="tok-string">$3</span>');
    } else if (language === "css") {
      text = text.replace(/(\/\*[\s\S]*?\*\/)/g, (m) => protect(m, "comment"));
      text = text.replace(/([.#]?[A-Za-z_][\w-]*)(\s*\{)/g, '<span class="tok-selector">$1</span>$2');
      text = text.replace(/([\w-]+)(\s*:)/g, '<span class="tok-property">$1</span>$2');
      text = text.replace(/(#[0-9a-fA-F]{3,8}\b|\b\d+(?:\.\d+)?(?:px|rem|em|%|vh|vw|s|ms)?\b)/g, '<span class="tok-number">$1</span>');
    } else {
      text = text.replace(/(\/\*[\s\S]*?\*\/|\/\/[^\n]*|#[^\n]*)/g, (m) => protect(m, "comment"));
      text = text.replace(/(&quot;(?:\\.|[^&])*?&quot;|'(?:\\.|[^'])*'|`(?:\\.|[^`])*`)/g, (m) => protect(m, "string"));
      text = text.replace(/\b(\d+(?:\.\d+)?)\b/g, '<span class="tok-number">$1</span>');
      const list = words[language] || words.generic;
      text = text.replace(new RegExp(`\\b(${list.join("|")})\\b`, "g"), '<span class="tok-keyword">$1</span>');
      if (language === "json") text = text.replace(/(&quot;[^&]*?&quot;)(\s*:)/g, '<span class="tok-property">$1</span>$2');
    }
    return text.replace(/@@TL(\d+)@@/g, (_, i) => stash[Number(i)] || "");
  };

  const install = () => {
    const editor = document.getElementById("fileEditor");
    const surface = document.getElementById("fileEditorSurface");
    if (!editor || !surface || surface.querySelector(".file-editor-highlight")) return false;
    const layer = document.createElement("pre");
    layer.className = "file-editor-highlight";
    layer.setAttribute("aria-hidden", "true");
    surface.insertBefore(layer, editor);

    const render = () => {
      const monacoReady = surface.classList.contains("monaco-ready");
      layer.hidden = monacoReady;
      if (monacoReady) {
        layer.textContent = "";
        return;
      }
      layer.innerHTML = `${highlight(editor.value, lang(K.state?.activeEditorPath))}\n`;
      layer.scrollTop = editor.scrollTop;
      layer.scrollLeft = editor.scrollLeft;
    };
    editor.addEventListener("input", render);
    editor.addEventListener("scroll", render);
    document.getElementById("fileTabs")?.addEventListener("click", () => setTimeout(render, 0));
    const target = document.getElementById("fileEditorTitle") || surface;
    new MutationObserver(render).observe(target, { childList: true, subtree: true, characterData: true });
    new MutationObserver(render).observe(surface, { attributes: true, attributeFilter: ["class"] });
    render();
    K.refreshEditorHighlight = render;
    return true;
  };

  if (!install()) {
    const timer = setInterval(() => { if (install()) clearInterval(timer); }, 120);
    setTimeout(() => clearInterval(timer), 10000);
  }
})();
