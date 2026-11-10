/// <reference types="astro/client" />
declare namespace App {
  interface Locals {
    /** Кто вошёл (из cookie сессии через GET /api/me); null — гость. */
    me: import('./lib/auth').Me | null;
  }
}
