// Compile-time guard: the Browser runtime adapter must satisfy the shared
// TLStudioRuntimeContract declared in global.d.ts. This module imports the
// module-owned kernel directly and intentionally emits no runtime behavior.
import { K } from "./kernel";

const runtimeContractCheck: TLStudioRuntimeContract = K.api;
void runtimeContractCheck;
