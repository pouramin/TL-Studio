// Compile-time guard: the Browser runtime adapter must satisfy the shared
// TLStudioRuntimeContract declared in global.d.ts. This file intentionally emits
// no production JavaScript and is included only by tsconfig.check.json.
const runtimeContractCheck: TLStudioRuntimeContract = window.KLU.api;
void runtimeContractCheck;
