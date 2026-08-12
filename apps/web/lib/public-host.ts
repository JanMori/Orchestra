const OFFICIAL_MARKETING_HOSTS = new Set(["orchestra.local", "localhost", "127.0.0.1"]);

export function isOfficialMarketingHost(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase().replace(/\.$/, "");
  return OFFICIAL_MARKETING_HOSTS.has(normalized);
}
