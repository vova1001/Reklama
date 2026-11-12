/// <reference types="astro/client" />
declare namespace App {
  interface Locals {
    /** Кто вошёл (из cookie сессии через GET /api/me); null — гость. */
    me: import('./lib/auth').Me | null;
    /** Сотрудник в /admin (GET /api/admin/me): роль, права, 2FA; null — вне админки. */
    admin: import('./lib/admin').AdminMe | null;
  }
}
