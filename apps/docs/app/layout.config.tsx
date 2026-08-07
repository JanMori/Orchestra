import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { ArrowUpRight } from "lucide-react";

// Docs-local stateless Multica mark — matches @orchestra/ui's MulticaIcon
// visually (same 8-pointed-asterisk clip-path), but without useState/
// useEffect so it's safe to render from Server Components such as
// layout.config.tsx / layout.tsx. Keep in sync with
// packages/ui/components/common/multica-icon.tsx if the mark changes.
const ORCHESTRA_CLIP = `polygon(
  45% 62.1%, 45% 100%, 55% 100%, 55% 62.1%,
  81.8% 88.9%, 88.9% 81.8%, 62.1% 55%, 100% 55%,
  100% 45%, 62.1% 45%, 88.9% 18.2%, 81.8% 11.1%,
  55% 37.9%, 55% 0%, 45% 0%, 45% 37.9%,
  18.2% 11.1%, 11.1% 18.2%, 37.9% 45%, 0% 45%,
  0% 55%, 37.9% 55%, 11.1% 81.8%, 18.2% 88.9%
)`;

function OrchestraMark() {
  return (
    <span className="inline-block size-[1.2em]" aria-hidden="true">
      <svg
        viewBox="240 257 1098 1098"
        className="size-full"
        xmlns="http://www.w3.org/2000/svg"
      >
        <g id="shapeci78WHxu7z">
          <g id="g1">
            <clipPath id="clipPath1">
              <path d="M 240.57019 257.21553 L 1338.244629 257.21553 L 1338.244629 1354.889969 L 240.57019 1354.889969 Z" />
            </clipPath>
            <g id="g2" clipPath="url(#clipPath1)">
              <g id="Y9oRq9ViRT">
                <g id="g3">
                  <g id="g4">
                    <g id="g5">
                      <path fill="#00b1f2" d="M 546.428528 498.770386 C 546.428528 518.951782 528.58728 535.312012 506.57901 535.312012 C 484.570679 535.312012 466.729431 518.951782 466.729431 498.770386 C 466.729431 478.588989 484.570679 462.228638 506.57901 462.228638 C 528.58728 462.228638 546.428528 478.588989 546.428528 498.770386 Z" />
                      <path fill="#00b1f2" d="M 506.57901 537.684814 C 483.290283 537.684814 464.141785 520.125854 464.141785 498.770386 C 464.141785 477.414795 483.290283 459.855835 506.57901 459.855835 C 529.867676 459.855835 549.016174 477.414795 549.016174 498.770386 C 549.016174 520.125854 529.867676 537.684814 506.57901 537.684814 Z M 506.57901 464.60144 C 485.87793 464.60144 469.317047 479.78772 469.317047 498.770386 C 469.317047 517.753052 485.87793 532.939209 506.57901 532.939209 C 527.28009 532.939209 543.840942 517.753052 543.840942 498.770386 C 543.840942 479.78772 527.28009 464.60144 506.57901 464.60144 Z" />
                    </g>
                  </g>
                  <g id="g27">
                    <path fill="currentColor" d="M 280.419739 373.959106 C 293.875458 373.959106 304.7435 383.925049 304.7435 396.263794 C 304.7435 408.602539 293.875458 418.568481 280.419739 418.568481 C 266.96405 418.568481 256.095978 408.602539 256.095978 396.263794 C 256.095978 383.925049 266.96405 373.959106 280.419739 373.959106 M 280.419739 359.722046 C 258.166107 359.722046 240.57019 375.857422 240.57019 396.263794 C 240.57019 416.670166 258.166107 432.80542 280.419739 432.80542 C 302.673401 432.80542 320.269318 416.670166 320.269318 396.263794 C 320.269318 375.857422 302.673401 359.722046 280.419739 359.722046 L 280.419739 359.722046 Z" />
                  </g>
                </g>
              </g>
            </g>
          </g>
        </g>
      </svg>
    </span>
  );
}

// GitHub mark — inlined SVG (lucide-react dropped the Github icon for brand
// trademark reasons). Path matches apps/web/features/landing/components/
// shared.tsx GitHubMark.
function GitHubMark() {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      className="size-[1em]"
      fill="currentColor"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8a8 8 0 0 0 5.47 7.59c.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2 .37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82A7.65 7.65 0 0 1 8 4.84c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

// External links shown at the top of the sidebar (and in the top nav on
// desktop). Leading icon = brand identity (GitHub mark / Orchestra mark);
// trailing ArrowUpRight = "opens externally" glyph, same pattern as
// `packages/views/layout/help-launcher.tsx` from PR #1560.
const externalLinkText = (label: string) => (
  <span className="inline-flex items-center gap-1">
    {label}
    <ArrowUpRight className="size-3 translate-y-px text-muted-foreground/60" />
  </span>
);

export const baseOptions: BaseLayoutProps = {
  nav: {
    title: (
      <span className="flex items-center gap-2 font-semibold text-base">
        <OrchestraMark /> Orchestra Docs
      </span>
    ),
  },
  links: [],
};
