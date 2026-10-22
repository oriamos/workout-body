// The built-in UI served by the engine binary logs in with a session cookie
// (POST /login). When the API answers 401 there, the session is missing or
// expired: go to the login page. The dev/preview proxy and the mock have no
// login page, so nothing happens there.
export const SERVED_BY_ENGINE = import.meta.env.PROD && import.meta.env.VITE_DATA_SOURCE !== 'mock';

let redirecting = false;
export function onUnauthorized(): void {
  if (!SERVED_BY_ENGINE || redirecting) return;
  redirecting = true;
  window.location.assign('/login');
}
