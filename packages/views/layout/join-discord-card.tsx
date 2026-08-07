"use client";



/**
 * Dismissible "Join our Discord" entry sharing the sidebar footer strip with
 * the help launcher. Once dismissed it stays hidden for that user on this
 * browser — see {@link useDiscordCardDismissed}.
 *
 * Deliberately shaped as a sidebar row, not a bordered card: it matches
 * SidebarMenuButton metrics (h-8 / rounded-md / gap-2 / text-body) and
 * carries no resting border or fill. A dismissible promo is the
 * lowest-priority item in the sidebar, so it must not be drawn heavier than
 * the navigation above it.
 *
 * No external-link arrow, unlike the Help menu's outbound items: sharing one
 * 224px strip with the help trigger leaves 128px for the label, and the arrow
 * plus the dismiss button together overflow that in the widest locale (en,
 * 109px). The dismiss affordance wins because it is a user-facing capability
 * and the arrow is only a hint — the Discord mark already signals the
 * destination. `flex-1 min-w-0` makes the label truncate rather than push the
 * trigger off.
 *
 * That 128px is the budget at the DEFAULT 256px sidebar, and the sidebar is
 * user-resizable down to 200px — every 1px of drag takes 1px from the label.
 * So a title that merely fits at 256px still truncates for anyone who has
 * narrowed their sidebar; keep every locale's title comfortably under budget,
 * not just under it (MUL-5704).
 */
export function JoinDiscordCard() {
  return null;
}
