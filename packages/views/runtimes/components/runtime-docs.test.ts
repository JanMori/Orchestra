import { describe, expect, it } from "vitest";
import {
  customRuntimeDocsHref,
  daemonRuntimesDocsHref,
} from "./runtime-docs";

describe("runtime docs links", () => {
  it.each([
    ["en", "http://localhost:5001/docs/daemon-runtimes"],
    ["zh-Hans", "http://localhost:5001/docs/zh/daemon-runtimes"],
    ["ja", "http://localhost:5001/docs/ja/daemon-runtimes"],
    ["ko", "http://localhost:5001/docs/ko/daemon-runtimes"],
  ])("localizes the daemon guide for %s", (language, expected) => {
    expect(daemonRuntimesDocsHref(language)).toBe(expected);
  });

  it("adds the localized custom runtime section", () => {
    expect(customRuntimeDocsHref("zh-Hans")).toBe(
      `http://localhost:5001/docs/zh/daemon-runtimes#${encodeURIComponent("自定义运行时配置")}`,
    );
  });
});
