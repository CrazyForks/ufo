export const MAX_AUTH_PASSWORD_LEN = 128;
export const MAX_AUTH_NAME_LEN = 80;
export const MAX_AUTH_EMAIL_LEN = 254;

export function validAuthEmail(email: string): boolean {
  if (!email || email.length > MAX_AUTH_EMAIL_LEN) return false;
  const at = email.indexOf("@");
  if (at <= 0 || at === email.length - 1) return false;
  return email.slice(at + 1).includes(".");
}
