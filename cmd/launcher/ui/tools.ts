(() => {
  "use strict";
  const K = window.KLU;

  const fallbackUnknown = Object.freeze({
    id: "runtime.unknown",
    runtimeIDs: [],
    name: "Runtime tool",
    description: "Tool not recognized by this TL Studio registry version. Runtime permission enforcement remains authoritative.",
    category: "runtime",
    permissionClass: "runtime",
    capabilities: Object.freeze({ read: false, write: false, execute: false, network: false }),
    presentation: "tool",
  });

  const cleanDescriptor = (value) => {
    if (!value || typeof value !== "object") return null;
    return {
      id: String(value.id || ""),
      runtimeIDs: Array.isArray(value.runtimeIDs) ? value.runtimeIDs.map(String).filter(Boolean) : [],
      name: String(value.name || ""),
      description: String(value.description || ""),
      category: String(value.category || "runtime"),
      permissionClass: String(value.permissionClass || "runtime"),
      capabilities: {
        read: value.capabilities?.read === true,
        write: value.capabilities?.write === true,
        execute: value.capabilities?.execute === true,
        network: value.capabilities?.network === true,
      },
      presentation: String(value.presentation || "tool"),
    };
  };

  K.state.toolRegistry = {
    version: 0,
    byRuntimeID: new Map(),
    unknown: fallbackUnknown,
  };

  K.loadToolRegistry = async () => {
    const payload = await K.api.tools.registry();
    const byRuntimeID = new Map();
    const descriptors = Array.isArray(payload?.tools) ? payload.tools.map(cleanDescriptor).filter(Boolean) : [];
    for (const descriptor of descriptors) {
      for (const runtimeID of descriptor.runtimeIDs) byRuntimeID.set(runtimeID, descriptor);
    }
    K.state.toolRegistry = {
      version: Number(payload?.version || 0),
      byRuntimeID,
      unknown: cleanDescriptor(payload?.unknown) || fallbackUnknown,
    };
    return K.state.toolRegistry;
  };

  K.toolDescriptor = (runtimeID) => {
    const key = String(runtimeID || "").trim();
    const known = K.state.toolRegistry?.byRuntimeID?.get(key);
    if (known) return { ...known, runtimeID: key, unknown: false };
    const fallback = K.state.toolRegistry?.unknown || fallbackUnknown;
    return {
      ...fallback,
      runtimeID: key,
      name: key ? `${fallback.name} (${key})` : fallback.name,
      unknown: true,
    };
  };

  K.__toolRegistryInstalled = true;
})();
