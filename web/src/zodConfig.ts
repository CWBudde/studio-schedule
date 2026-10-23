import { z } from 'zod';

// The CSP forbids eval (ADR 0006). Without this, zod probes it with
// new Function() when a schema is built and the browser reports a CSP
// violation. main.tsx imports this module first, before any schema exists.
z.config({ jitless: true });
