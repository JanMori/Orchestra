import { beforeEach, describe, expect, it, vi } from "vitest";

const existingDocs = vi.hoisted(() => new Set<string>());

vi.mock("node:fs", () => ({
  existsSync: vi.fn((path: string) => {
    const normalized = path.replaceAll("\\", "/");
    return [...existingDocs].some((suffix) => normalized.endsWith(suffix));
  }),
}));

const pages = new Map<string, { url: string }>([
  ["en:", { url: "/" }],
  ["zh:", { url: "/zh" }],
  ["ko:", { url: "/ko" }],
  ["ja:", { url: "/ja" }],
  ["en:agents", { url: "/agents" }],
  ["zh:agents", { url: "/zh/agents" }],
  ["ko:agents", { url: "/ko/agents" }],
  ["ja:agents", { url: "/ja/agents" }],
]);

vi.mock("@/lib/source", () => ({
  source: {
    getPage: vi.fn((slugs: string[], lang: string) => {
      return pages.get(`${lang}:${slugs.join("/")}`) ?? null;
    }),
  },
}));

beforeEach(() => {
  existingDocs.clear();
  existingDocs.add("index.mdx");
  existingDocs.add("index.zh.mdx");
  existingDocs.add("agents.mdx");
  existingDocs.add("agents.zh.mdx");
});

describe("docsAlternates", () => {
  it("omits Korean hreflang when no Korean MDX file exists for the page", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "http://localhost:5001/docs/agents",
      languages: {
        en: "http://localhost:5001/docs/agents",
        zh: "http://localhost:5001/docs/zh/agents",
        "x-default": "http://localhost:5001/docs/agents",
      },
    });
  });

  it("omits Korean hreflang even when source.getPage returns a page for Korean", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"]).languages).not.toHaveProperty("ko");
  });

  it("includes Korean hreflang when a real *.ko.mdx page exists", async () => {
    existingDocs.add("agents.ko.mdx");
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "http://localhost:5001/docs/agents",
      languages: {
        en: "http://localhost:5001/docs/agents",
        zh: "http://localhost:5001/docs/zh/agents",
        ko: "http://localhost:5001/docs/ko/agents",
        "x-default": "http://localhost:5001/docs/agents",
      },
    });
  });

  it("includes Japanese hreflang when a real *.ja.mdx page exists", async () => {
    existingDocs.add("agents.ja.mdx");
    const { docsAlternates } = await import("./site");

    expect(docsAlternates(["agents"])).toEqual({
      canonical: "http://localhost:5001/docs/agents",
      languages: {
        en: "http://localhost:5001/docs/agents",
        zh: "http://localhost:5001/docs/zh/agents",
        ja: "http://localhost:5001/docs/ja/agents",
        "x-default": "http://localhost:5001/docs/agents",
      },
    });
  });

  it("keeps the locale root alternates limited to real localized MDX pages", async () => {
    const { docsAlternates } = await import("./site");

    expect(docsAlternates([])).toEqual({
      canonical: "http://localhost:5001/docs",
      languages: {
        en: "http://localhost:5001/docs",
        zh: "http://localhost:5001/docs/zh",
        "x-default": "http://localhost:5001/docs",
      },
    });
  });
});
